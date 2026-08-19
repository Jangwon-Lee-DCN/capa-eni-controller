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

	"k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	networkingv1alpha1 "github.com/ssu-dcn/capa-eni-controller/api/v1alpha1"
	controllermetrics "github.com/ssu-dcn/capa-eni-controller/internal/metrics"
)

// ENIPoolReconciler reconciles a ENIPool object
type ENIPoolReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=networking.dcn.ssu.ac.kr,resources=enipools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.dcn.ssu.ac.kr,resources=enipools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=networking.dcn.ssu.ac.kr,resources=enipools/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ENIPool object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.24.1/pkg/reconcile
func (r *ENIPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	pool := &networkingv1alpha1.ENIPool{}
	if err := r.Get(ctx, req.NamespacedName, pool); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	claims := &networkingv1alpha1.ENIClaimList{}
	if err := r.List(ctx, claims); err != nil {
		return ctrl.Result{}, err
	}
	claimByENI := map[string]*networkingv1alpha1.ENIClaim{}
	for i := range claims.Items {
		claimByENI[claims.Items[i].Spec.ENIID] = &claims.Items[i]
	}
	observed := make([]networkingv1alpha1.ENIObservation, 0, len(pool.Spec.Interfaces))
	available := 0
	for _, configured := range pool.Spec.Interfaces {
		item := networkingv1alpha1.ENIObservation{ID: configured.ID, PrivateIP: configured.PrivateIP}
		if claim := claimByENI[configured.ID]; claim != nil {
			item.State = networkingv1alpha1.ENIStateClaimed
			item.ClaimRef = &claim.Spec.MachineRef
		} else {
			item.State = networkingv1alpha1.ENIStateAvailable
			available++
		}
		observed = append(observed, item)
	}
	pool.Status.ObservedGeneration = pool.Generation
	pool.Status.Interfaces = observed
	controllermetrics.PoolAvailableInterfaces.WithLabelValues(pool.Name, pool.Spec.Region, pool.Spec.VPCID).Set(float64(available))
	setCondition(&pool.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "DeclaredInventoryReady", Message: "ENI inventory reflects the declared pool and active claims", ObservedGeneration: pool.Generation})
	if err := r.Status().Update(ctx, pool); err != nil && !errors.IsConflict(err) {
		return ctrl.Result{}, err
	}
	log.Info("Updated ENIPool inventory", "interfaces", len(observed))
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func setCondition(conditions *[]metav1.Condition, condition metav1.Condition) {
	apiMeta.SetStatusCondition(conditions, condition)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ENIPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1alpha1.ENIPool{}).
		Named("enipool").
		Complete(r)
}
