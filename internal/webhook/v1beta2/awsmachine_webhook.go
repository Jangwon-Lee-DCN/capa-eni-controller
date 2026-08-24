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

package v1beta2

import (
	"context"
	"fmt"
	"net/netip"
	"sort"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	infrastructurev1beta2 "sigs.k8s.io/cluster-api-provider-aws/v2/api/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	networkingv1alpha1 "github.com/ssu-dcn/capa-eni-controller/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var awsmachinelog = logf.Log.WithName("awsmachine-resource")

// SetupAWSMachineWebhookWithManager registers the webhook for AWSMachine in the manager.
func SetupAWSMachineWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &infrastructurev1beta2.AWSMachine{}).
		WithDefaulter(&AWSMachineCustomDefaulter{Client: mgr.GetClient()}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-infrastructure-cluster-x-k8s-io-v1beta2-awsmachine,mutating=true,failurePolicy=fail,sideEffects=NoneOnDryRun,groups=infrastructure.cluster.x-k8s.io,resources=awsmachines,verbs=create,versions=v1beta2,name=mawsmachine-v1beta2.eni.dcn.ssu.ac.kr,admissionReviewVersions=v1

// AWSMachineCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind AWSMachine when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type AWSMachineCustomDefaulter struct {
	Client client.Client
}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind AWSMachine.
func (d *AWSMachineCustomDefaulter) Default(ctx context.Context, obj *infrastructurev1beta2.AWSMachine) error {
	annotations := obj.GetAnnotations()
	if annotations[networkingv1alpha1.AllocateFromPoolAnnotation] != "true" || len(obj.Spec.NetworkInterfaces) > 0 {
		return nil
	}
	if _, explicitlyPaused := annotations[networkingv1alpha1.CAPPausedAnnotation]; explicitlyPaused {
		return nil
	}
	if d.Client == nil { // Retained for isolated unit tests.
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[networkingv1alpha1.CAPPausedAnnotation] = ""
		annotations[networkingv1alpha1.AllocatorPausedAnnotation] = "true"
		obj.SetAnnotations(annotations)
		return nil
	}
	if request, err := admission.RequestFromContext(ctx); err == nil && request.DryRun != nil && *request.DryRun {
		return nil
	}
	region, vpcID, err := d.resolveClusterNetwork(ctx, obj)
	if err != nil {
		return err
	}
	pool, err := d.findPool(ctx, region, vpcID)
	if err != nil || pool == nil {
		return err
	}
	interfaceKey, err := d.resolveInterfaceKey(ctx, obj)
	if err != nil {
		return err
	}
	claim, err := d.claimInterface(ctx, obj, pool, interfaceKey)
	if err != nil {
		return err
	}
	if claim == nil { // Dynamic exhaustion policy leaves CAPA networking untouched.
		return nil
	}
	obj.Spec.NetworkInterfaces = []string{claim.Spec.ENIID}
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[networkingv1alpha1.AllocationResultAnnotation] = networkingv1alpha1.AllocationResultAllocated
	obj.SetAnnotations(annotations)
	awsmachinelog.Info("Allocated ENI during AWSMachine admission", "name", obj.GetName(), "eni", claim.Spec.ENIID)
	return nil
}

func (d *AWSMachineCustomDefaulter) resolveClusterNetwork(ctx context.Context, obj *infrastructurev1beta2.AWSMachine) (string, string, error) {
	clusterName := obj.Labels[clusterv1.ClusterNameLabel]
	if clusterName == "" {
		return "", "", fmt.Errorf("AWSMachine has no %s label", clusterv1.ClusterNameLabel)
	}
	cluster := &clusterv1.Cluster{}
	if err := d.Client.Get(ctx, client.ObjectKey{Namespace: obj.Namespace, Name: clusterName}, cluster); err != nil {
		return "", "", fmt.Errorf("get Cluster: %w", err)
	}
	if !cluster.Spec.InfrastructureRef.IsDefined() || cluster.Spec.InfrastructureRef.Kind != "AWSCluster" {
		return "", "", fmt.Errorf("Cluster does not reference an AWSCluster")
	}
	awsCluster := &infrastructurev1beta2.AWSCluster{}
	if err := d.Client.Get(ctx, client.ObjectKey{Namespace: obj.Namespace, Name: cluster.Spec.InfrastructureRef.Name}, awsCluster); err != nil {
		return "", "", fmt.Errorf("get AWSCluster: %w", err)
	}
	return awsCluster.Spec.Region, awsCluster.Spec.NetworkSpec.VPC.ID, nil
}

func (d *AWSMachineCustomDefaulter) findPool(ctx context.Context, region, vpcID string) (*networkingv1alpha1.ENIPool, error) {
	list := &networkingv1alpha1.ENIPoolList{}
	if err := d.Client.List(ctx, list); err != nil {
		return nil, err
	}
	var match *networkingv1alpha1.ENIPool
	for i := range list.Items {
		if list.Items[i].Spec.Region == region && list.Items[i].Spec.VPCID == vpcID {
			if match != nil {
				return nil, fmt.Errorf("multiple ENIPools match region %s and VPC %s", region, vpcID)
			}
			match = list.Items[i].DeepCopy()
		}
	}
	return match, nil
}

func (d *AWSMachineCustomDefaulter) resolveInterfaceKey(ctx context.Context, obj *infrastructurev1beta2.AWSMachine) (string, error) {
	if key := obj.Annotations[networkingv1alpha1.InterfaceKeyAnnotation]; key != "" {
		return key, nil
	}
	for _, owner := range obj.OwnerReferences {
		ownerGroupVersion, err := schema.ParseGroupVersion(owner.APIVersion)
		if err != nil || owner.Kind != "Machine" || ownerGroupVersion.Group != clusterv1.GroupVersion.Group {
			continue
		}
		machine := &clusterv1.Machine{}
		if err := d.Client.Get(ctx, client.ObjectKey{Namespace: obj.Namespace, Name: owner.Name}, machine); err != nil {
			return "", fmt.Errorf("get owner Machine for interface key: %w", err)
		}
		return machine.Annotations[networkingv1alpha1.InterfaceKeyAnnotation], nil
	}
	return "", nil
}

func (d *AWSMachineCustomDefaulter) claimInterface(ctx context.Context, obj *infrastructurev1beta2.AWSMachine, pool *networkingv1alpha1.ENIPool, interfaceKey string) (*networkingv1alpha1.ENIClaim, error) {
	configured := append([]networkingv1alpha1.ENIReference(nil), pool.Spec.Interfaces...)
	if interfaceKey != "" {
		configured = configured[:0]
		for _, candidate := range pool.Spec.Interfaces {
			if candidate.Key == interfaceKey {
				configured = append(configured, candidate)
			}
		}
		if len(configured) == 0 {
			return nil, fmt.Errorf("ENIPool %q has no interface with key %q", pool.Name, interfaceKey)
		}
	}
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
				MachineRef: networkingv1alpha1.NamespacedObjectReference{Namespace: obj.Namespace, Name: obj.Name},
				PoolRef:    networkingv1alpha1.ClusterObjectReference{Name: pool.Name}, ENIID: requested.ID,
			},
		}
		if err := d.Client.Create(ctx, claim); err != nil {
			if apierrors.IsAlreadyExists(err) {
				continue
			}
			return nil, err
		}
		return claim, nil
	}
	return nil, nil
}
