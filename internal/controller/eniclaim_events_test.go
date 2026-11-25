package controller

import (
	"context"
	"testing"

	networkingv1alpha1 "github.com/ssu-dcn/capa-eni-controller/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestMachineEventEnqueuesOnlyItsClaims(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := networkingv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	const workerName = "event-worker"
	claims := make([]client.Object, 0, 3)
	for _, id := range []string{"eni-first", "eni-second"} {
		claims = append(claims, &networkingv1alpha1.ENIClaim{ObjectMeta: metav1.ObjectMeta{Name: id}, Spec: networkingv1alpha1.ENIClaimSpec{MachineRef: networkingv1alpha1.NamespacedObjectReference{Namespace: "event-lab", Name: workerName}}})
	}
	claims = append(claims, &networkingv1alpha1.ENIClaim{ObjectMeta: metav1.ObjectMeta{Name: "eni-other"}, Spec: networkingv1alpha1.ENIClaimSpec{MachineRef: networkingv1alpha1.NamespacedObjectReference{Namespace: "other-lab", Name: workerName}}})
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(claims...).WithIndex(&networkingv1alpha1.ENIClaim{}, machineRefIndex, func(o client.Object) []string {
		x := o.(*networkingv1alpha1.ENIClaim)
		return []string{x.Spec.MachineRef.Namespace + "/" + x.Spec.MachineRef.Name}
	}).Build()
	r := &ENIClaimReconciler{Client: c}
	requests := r.claimsForMachine(context.Background(), &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: workerName, Namespace: "event-lab"}})
	if len(requests) != 2 {
		t.Fatalf("machine event mapped to %d claims", len(requests))
	}
	for _, request := range requests {
		if request.Name == "eni-other" {
			t.Fatal("unrelated namespace claim enqueued")
		}
	}
}
