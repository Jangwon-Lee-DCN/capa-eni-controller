/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/events"
	infrastructurev1beta2 "sigs.k8s.io/cluster-api-provider-aws/v2/api/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	networkingv1alpha1 "github.com/ssu-dcn/capa-eni-controller/api/v1alpha1"
	controllermetrics "github.com/ssu-dcn/capa-eni-controller/internal/metrics"
)

// AWSMachineReconciler reconciles a AWSMachine object
type AWSMachineReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=awsmachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=awsmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=awsmachines/finalizers,verbs=update
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=awsclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.dcn.ssu.ac.kr,resources=enipools,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.dcn.ssu.ac.kr,resources=eniclaims,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AWSMachine object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.24.1/pkg/reconcile
func (r *AWSMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	machine := &infrastructurev1beta2.AWSMachine{}
	if err := r.Get(ctx, req.NamespacedName, machine); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if machine.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}
	if machine.Annotations[networkingv1alpha1.AllocateFromPoolAnnotation] != "true" ||
		machine.Annotations[networkingv1alpha1.AllocatorPausedAnnotation] != "true" ||
		len(machine.Spec.NetworkInterfaces) > 0 {
		return ctrl.Result{}, nil
	}
	region, vpcID, err := r.resolveClusterNetwork(ctx, machine)
	if err != nil {
		log.Info("Waiting for AWSCluster network", "reason", err.Error())
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	pool, err := r.findPool(ctx, region, vpcID)
	if err != nil {
		return ctrl.Result{}, err
	}
	if pool == nil {
		return r.dynamicFallback(ctx, machine, "NoMatchingPool")
	}
	claim, err := r.claimInterface(ctx, machine, pool)
	if err != nil {
		return ctrl.Result{}, err
	}
	if claim == nil {
		switch pool.Spec.ExhaustionPolicy {
		case networkingv1alpha1.ExhaustionPolicyWait:
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		case networkingv1alpha1.ExhaustionPolicyFail:
			return r.markFailed(ctx, machine, "PoolExhausted")
		default:
			return r.dynamicFallback(ctx, machine, "PoolExhausted")
		}
	}
	base := machine.DeepCopy()
	machine.Spec.NetworkInterfaces = []string{claim.Spec.ENIID}
	removeAllocatorPause(machine)
	setAnnotation(machine, networkingv1alpha1.AllocationResultAnnotation, networkingv1alpha1.AllocationResultAllocated)
	setAnnotation(machine, networkingv1alpha1.AllocationReasonAnnotation, "")
	if err := r.Patch(ctx, machine, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	log.Info("Allocated ENI to AWSMachine", "eni", claim.Spec.ENIID, "pool", pool.Name)
	if r.Recorder != nil {
		r.Recorder.Eventf(machine, nil, "Normal", "ENIAllocated", "AllocateENI", "Allocated ENI %s from pool %s", claim.Spec.ENIID, pool.Name)
	}
	return ctrl.Result{}, nil
}

func (r *AWSMachineReconciler) resolveClusterNetwork(ctx context.Context, awsMachine *infrastructurev1beta2.AWSMachine) (string, string, error) {
	var ownerName string
	for _, owner := range awsMachine.OwnerReferences {
		ownerGroupVersion, err := schema.ParseGroupVersion(owner.APIVersion)
		if err == nil && owner.Kind == "Machine" && ownerGroupVersion.Group == clusterv1.GroupVersion.Group {
			ownerName = owner.Name
			break
		}
	}
	if ownerName == "" {
		return "", "", fmt.Errorf("AWSMachine has no CAPI Machine owner")
	}
	machine := &clusterv1.Machine{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: awsMachine.Namespace, Name: ownerName}, machine); err != nil {
		return "", "", fmt.Errorf("get owner Machine: %w", err)
	}
	cluster := &clusterv1.Cluster{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: awsMachine.Namespace, Name: machine.Spec.ClusterName}, cluster); err != nil {
		return "", "", fmt.Errorf("get Cluster: %w", err)
	}
	if !cluster.Spec.InfrastructureRef.IsDefined() || cluster.Spec.InfrastructureRef.Kind != "AWSCluster" {
		return "", "", fmt.Errorf("Cluster does not reference an AWSCluster")
	}
	awsCluster := &infrastructurev1beta2.AWSCluster{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: awsMachine.Namespace, Name: cluster.Spec.InfrastructureRef.Name}, awsCluster); err != nil {
		return "", "", fmt.Errorf("get AWSCluster: %w", err)
	}
	if awsCluster.Spec.Region == "" || awsCluster.Spec.NetworkSpec.VPC.ID == "" {
		return "", "", fmt.Errorf("AWSCluster region or VPC ID is not ready")
	}
	return awsCluster.Spec.Region, awsCluster.Spec.NetworkSpec.VPC.ID, nil
}

func (r *AWSMachineReconciler) findPool(ctx context.Context, region, vpcID string) (*networkingv1alpha1.ENIPool, error) {
	list := &networkingv1alpha1.ENIPoolList{}
	if err := r.List(ctx, list); err != nil {
		return nil, err
	}
	var match *networkingv1alpha1.ENIPool
	for i := range list.Items {
		pool := &list.Items[i]
		if pool.Spec.Region == region && pool.Spec.VPCID == vpcID {
			if match != nil {
				return nil, fmt.Errorf("multiple ENIPools match region %s and VPC %s", region, vpcID)
			}
			match = pool.DeepCopy()
		}
	}
	return match, nil
}

func (r *AWSMachineReconciler) claimInterface(ctx context.Context, machine *infrastructurev1beta2.AWSMachine, pool *networkingv1alpha1.ENIPool) (*networkingv1alpha1.ENIClaim, error) {
	claims := &networkingv1alpha1.ENIClaimList{}
	if err := r.List(ctx, claims); err != nil {
		return nil, err
	}
	for i := range claims.Items {
		claim := &claims.Items[i]
		if claim.Spec.MachineRef.Namespace == machine.Namespace && claim.Spec.MachineRef.Name == machine.Name {
			return claim.DeepCopy(), nil
		}
	}
	configured := append([]networkingv1alpha1.ENIReference(nil), pool.Spec.Interfaces...)
	sort.Slice(configured, func(i, j int) bool {
		left, leftErr := netip.ParseAddr(configured[i].PrivateIP)
		right, rightErr := netip.ParseAddr(configured[j].PrivateIP)
		if leftErr != nil || rightErr != nil {
			return configured[i].PrivateIP < configured[j].PrivateIP
		}
		return left.Compare(right) < 0
	})
	for _, requested := range configured {
		claim := &networkingv1alpha1.ENIClaim{
			ObjectMeta: metav1.ObjectMeta{Name: requested.ID, Finalizers: []string{networkingv1alpha1.ClaimFinalizer}},
			Spec: networkingv1alpha1.ENIClaimSpec{
				MachineRef: networkingv1alpha1.NamespacedObjectReference{Namespace: machine.Namespace, Name: machine.Name},
				PoolRef:    networkingv1alpha1.ClusterObjectReference{Name: pool.Name},
				ENIID:      requested.ID,
			},
		}
		if err := r.Create(ctx, claim); err != nil {
			if apierrors.IsAlreadyExists(err) {
				continue
			}
			return nil, err
		}
		return claim, nil
	}
	return nil, nil
}

func (r *AWSMachineReconciler) dynamicFallback(ctx context.Context, machine *infrastructurev1beta2.AWSMachine, reason string) (ctrl.Result, error) {
	base := machine.DeepCopy()
	removeAllocatorPause(machine)
	setAnnotation(machine, networkingv1alpha1.AllocationResultAnnotation, networkingv1alpha1.AllocationResultDynamicFallback)
	setAnnotation(machine, networkingv1alpha1.AllocationReasonAnnotation, reason)
	controllermetrics.DynamicFallbacks.WithLabelValues(reason).Inc()
	if r.Recorder != nil {
		r.Recorder.Eventf(machine, nil, "Warning", "DynamicFallback", "ReleaseToCAPA", "No ENI was allocated: %s", reason)
	}
	return ctrl.Result{}, r.Patch(ctx, machine, client.MergeFrom(base))
}

func (r *AWSMachineReconciler) markFailed(ctx context.Context, machine *infrastructurev1beta2.AWSMachine, reason string) (ctrl.Result, error) {
	base := machine.DeepCopy()
	setAnnotation(machine, networkingv1alpha1.AllocationResultAnnotation, networkingv1alpha1.AllocationResultFailed)
	setAnnotation(machine, networkingv1alpha1.AllocationReasonAnnotation, reason)
	return ctrl.Result{}, r.Patch(ctx, machine, client.MergeFrom(base))
}

func removeAllocatorPause(machine *infrastructurev1beta2.AWSMachine) {
	if machine.Annotations == nil || machine.Annotations[networkingv1alpha1.AllocatorPausedAnnotation] != "true" {
		return
	}
	delete(machine.Annotations, networkingv1alpha1.AllocatorPausedAnnotation)
	delete(machine.Annotations, networkingv1alpha1.CAPPausedAnnotation)
}

func setAnnotation(machine *infrastructurev1beta2.AWSMachine, key, value string) {
	if machine.Annotations == nil {
		machine.Annotations = map[string]string{}
	}
	if value == "" {
		delete(machine.Annotations, key)
		return
	}
	machine.Annotations[key] = value
}

// SetupWithManager sets up the controller with the Manager.
func (r *AWSMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1beta2.AWSMachine{}).
		Named("awsmachine").
		Complete(r)
}
