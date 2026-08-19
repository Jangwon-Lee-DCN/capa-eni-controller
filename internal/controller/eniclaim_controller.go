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
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	infrastructurev1beta2 "sigs.k8s.io/cluster-api-provider-aws/v2/api/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	networkingv1alpha1 "github.com/ssu-dcn/capa-eni-controller/api/v1alpha1"
)

// ENIClaimReconciler reconciles a ENIClaim object
type ENIClaimReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=networking.dcn.ssu.ac.kr,resources=eniclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.dcn.ssu.ac.kr,resources=eniclaims/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=networking.dcn.ssu.ac.kr,resources=eniclaims/finalizers,verbs=update
// +kubebuilder:rbac:groups=networking.dcn.ssu.ac.kr,resources=enipools,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=awsmachines,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ENIClaim object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.24.1/pkg/reconcile
func (r *ENIClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	claim := &networkingv1alpha1.ENIClaim{}
	if err := r.Get(ctx, req.NamespacedName, claim); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	pool := &networkingv1alpha1.ENIPool{}
	if err := r.Get(ctx, client.ObjectKey{Name: claim.Spec.PoolRef.Name}, pool); err != nil {
		if apierrors.IsNotFound(err) && claim.DeletionTimestamp != nil {
			return ctrl.Result{}, r.removeFinalizer(ctx, claim)
		}
		return ctrl.Result{}, err
	}
	if claim.DeletionTimestamp != nil {
		log.Info("Released ENIClaim", "eni", claim.Spec.ENIID)
		return ctrl.Result{}, r.removeFinalizer(ctx, claim)
	}
	if !containsString(claim.Finalizers, networkingv1alpha1.ClaimFinalizer) {
		base := claim.DeepCopy()
		claim.Finalizers = append(claim.Finalizers, networkingv1alpha1.ClaimFinalizer)
		if err := r.Patch(ctx, claim, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}
	}
	machine := &infrastructurev1beta2.AWSMachine{}
	err := r.Get(ctx, client.ObjectKey{Namespace: claim.Spec.MachineRef.Namespace, Name: claim.Spec.MachineRef.Name}, machine)
	if apierrors.IsNotFound(err) {
		// A mutating admission webhook creates the claim immediately before the
		// AWSMachine is persisted. Keep a short grace period for that transaction.
		if time.Since(claim.CreationTimestamp.Time) < 30*time.Second {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		if deleteErr := r.Delete(ctx, claim); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
			return ctrl.Result{}, deleteErr
		}
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	phase := networkingv1alpha1.ENIClaimPhaseBound
	privateIP := configuredPrivateIP(pool, claim.Spec.ENIID)
	if privateIP == "" {
		phase = networkingv1alpha1.ENIClaimPhaseFailed
	}
	if claim.Status.Phase != phase || claim.Status.PrivateIP != privateIP {
		claim.Status.Phase = phase
		claim.Status.PrivateIP = privateIP
		apiMeta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{Type: "Ready", Status: boolCondition(phase == networkingv1alpha1.ENIClaimPhaseBound), Reason: string(phase), ObservedGeneration: claim.Generation})
		if err := r.Status().Update(ctx, claim); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func configuredPrivateIP(pool *networkingv1alpha1.ENIPool, eniID string) string {
	for _, configured := range pool.Spec.Interfaces {
		if configured.ID == eniID {
			return configured.PrivateIP
		}
	}
	return ""
}

func (r *ENIClaimReconciler) removeFinalizer(ctx context.Context, claim *networkingv1alpha1.ENIClaim) error {
	if !containsString(claim.Finalizers, networkingv1alpha1.ClaimFinalizer) {
		return nil
	}
	base := claim.DeepCopy()
	claim.Finalizers = removeString(claim.Finalizers, networkingv1alpha1.ClaimFinalizer)
	return r.Patch(ctx, claim, client.MergeFrom(base))
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func removeString(values []string, unwanted string) []string {
	result := values[:0]
	for _, value := range values {
		if value != unwanted {
			result = append(result, value)
		}
	}
	return result
}

func boolCondition(value bool) metav1.ConditionStatus {
	if value {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
}

// SetupWithManager sets up the controller with the Manager.
func (r *ENIClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1alpha1.ENIClaim{}).
		Named("eniclaim").
		Complete(r)
}
