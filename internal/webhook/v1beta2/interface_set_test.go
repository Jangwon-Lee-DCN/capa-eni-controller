package v1beta2

import (
	"context"
	"reflect"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	networkingv1alpha1 "github.com/ssu-dcn/capa-eni-controller/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	infrastructurev1beta2 "sigs.k8s.io/cluster-api-provider-aws/v2/api/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func bundleFixture(t *testing.T, n int) (*AWSMachineCustomDefaulter, *infrastructurev1beta2.AWSMachine, *networkingv1alpha1.ENIPool) {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, f := range []func(*runtime.Scheme) error{networkingv1alpha1.AddToScheme, clusterv1.AddToScheme, infrastructurev1beta2.AddToScheme} {
		if err := f(scheme); err != nil {
			t.Fatal(err)
		}
	}
	pool := &networkingv1alpha1.ENIPool{ObjectMeta: metav1.ObjectMeta{Name: "fixed"}, Spec: networkingv1alpha1.ENIPoolSpec{Region: fixtureRegion, VPCID: fixtureVPC, ExhaustionPolicy: networkingv1alpha1.ExhaustionPolicyFail}}
	keys := []string{fixturePrimaryKey, "n2", "n4", "n3", "n6"}
	ids := []string{fixturePrimaryENI, "eni-002", fixtureN4ENI, "eni-004", "eni-005"}
	for i := range n {
		pool.Spec.Interfaces = append(pool.Spec.Interfaces, networkingv1alpha1.ENIReference{Key: keys[i], ID: ids[i], PrivateIP: "172.31.81.10"})
	}
	cluster := &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: fixtureBundleCluster, Namespace: fixtureDefaultNamespace}, Spec: clusterv1.ClusterSpec{InfrastructureRef: clusterv1.ContractVersionedObjectReference{APIGroup: infraGroup, Kind: awsClusterKind, Name: fixtureBundleCluster}}}
	aws := &infrastructurev1beta2.AWSCluster{ObjectMeta: metav1.ObjectMeta{Name: fixtureBundleCluster, Namespace: fixtureDefaultNamespace}, Spec: infrastructurev1beta2.AWSClusterSpec{Region: pool.Spec.Region, NetworkSpec: infrastructurev1beta2.NetworkSpec{VPC: infrastructurev1beta2.VPCSpec{ID: pool.Spec.VPCID}}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, cluster, aws).Build()
	obj := &infrastructurev1beta2.AWSMachine{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: fixtureDefaultNamespace, Labels: map[string]string{clusterv1.ClusterNameLabel: fixtureBundleCluster}, Annotations: map[string]string{networkingv1alpha1.AllocateFromPoolAnnotation: allocationEnabled, networkingv1alpha1.InterfaceKeyAnnotation: fixturePrimaryKey}}}
	return &AWSMachineCustomDefaulter{Client: c}, obj, pool
}
func claimCount(t *testing.T, d *AWSMachineCustomDefaulter) int {
	t.Helper()
	claims := &networkingv1alpha1.ENIClaimList{}
	if e := d.Client.List(context.Background(), claims); e != nil {
		t.Fatal(e)
	}
	return len(claims.Items)
}
func TestInterfaceSetOrderedAndIdempotent(t *testing.T) {
	for _, n := range []int{3, 5} {
		d, obj, _ := bundleFixture(t, n)
		extra := fixtureAdditionalKeys
		if n == 5 {
			extra += ",n3,n6"
		}
		obj.Annotations[networkingv1alpha1.AdditionalInterfaceKeysAnnotation] = extra
		if e := d.Default(context.Background(), obj); e != nil {
			t.Fatal(e)
		}
		want := []string{fixturePrimaryENI, "eni-002", fixtureN4ENI, "eni-004", "eni-005"}[:n]
		if !reflect.DeepEqual(obj.Spec.NetworkInterfaces, want) {
			t.Fatalf("wrong order: %v", obj.Spec.NetworkInterfaces)
		}
		obj.Spec.NetworkInterfaces = nil
		if e := d.Default(context.Background(), obj); e != nil {
			t.Fatalf("retry: %v", e)
		}
		if got := claimCount(t, d); got != n {
			t.Fatalf("got %d claims, wanted %d", got, n)
		}
	}
}

func TestPrimaryOnlyCAPAStillReservesCompleteSet(t *testing.T) {
	d, obj, _ := bundleFixture(t, 5)
	obj.Annotations[networkingv1alpha1.AdditionalInterfaceKeysAnnotation] = fixtureAdditionalKeys + ",n3,n6"
	obj.Annotations[networkingv1alpha1.AttachAdditionalAnnotation] = allocationEnabled
	if err := d.Default(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(obj.Spec.NetworkInterfaces, []string{fixturePrimaryENI}) || claimCount(t, d) != 5 {
		t.Fatal("primary-only CAPA path did not retain all reservations")
	}
	if obj.Annotations[networkingv1alpha1.InterfaceSetAnnotation] != fixturePrimaryENI+",eni-002,"+fixtureN4ENI+",eni-004,eni-005" {
		t.Fatal("attachment order lost")
	}
}
func TestInterfaceSetRejectsMalformedKeysBeforeReserving(t *testing.T) {
	for _, keys := range []string{"n2,missing", "n2,n2", "n2,,n4", "primary,n4"} {
		d, obj, _ := bundleFixture(t, 3)
		obj.Annotations[networkingv1alpha1.AdditionalInterfaceKeysAnnotation] = keys
		if e := d.Default(context.Background(), obj); e == nil {
			t.Fatalf("accepted malformed bundle %s", keys)
		}
		if claimCount(t, d) != 0 || len(obj.Spec.NetworkInterfaces) != 0 {
			t.Fatal("partial reservation on malformed request")
		}
	}
}
func TestInterfaceSetRollsBackNewClaimsOnBusySecondary(t *testing.T) {
	d, obj, pool := bundleFixture(t, 3)
	pool.Spec.ExhaustionPolicy = networkingv1alpha1.ExhaustionPolicyDynamic
	if e := d.Client.Update(context.Background(), pool); e != nil {
		t.Fatal(e)
	}
	busy := &networkingv1alpha1.ENIClaim{ObjectMeta: metav1.ObjectMeta{Name: fixtureN4ENI}, Spec: networkingv1alpha1.ENIClaimSpec{ENIID: fixtureN4ENI, MachineRef: networkingv1alpha1.NamespacedObjectReference{Namespace: fixtureDefaultNamespace, Name: "another-worker"}, PoolRef: networkingv1alpha1.ClusterObjectReference{Name: pool.Name}}}
	if e := d.Client.Create(context.Background(), busy); e != nil {
		t.Fatal(e)
	}
	obj.Annotations[networkingv1alpha1.AdditionalInterfaceKeysAnnotation] = fixtureAdditionalKeys
	if e := d.Default(context.Background(), obj); e == nil {
		t.Fatal("busy bundle fell back to dynamic allocation")
	}
	if claimCount(t, d) != 1 || len(obj.Spec.NetworkInterfaces) != 0 {
		t.Fatal("partial claims were retained")
	}
	other := &networkingv1alpha1.ENIClaim{}
	if e := d.Client.Get(context.Background(), client.ObjectKey{Name: fixtureN4ENI}, other); e != nil || other.Spec.MachineRef.Name != "another-worker" {
		t.Fatal("another owner's claim changed")
	}
}
func TestInterfaceSetRequiresPrimaryAndRejectsDuplicateIDs(t *testing.T) {
	d, obj, pool := bundleFixture(t, 3)
	obj.Annotations[networkingv1alpha1.InterfaceKeyAnnotation] = ""
	obj.Annotations[networkingv1alpha1.AdditionalInterfaceKeysAnnotation] = fixtureAdditionalKeys
	if e := d.Default(context.Background(), obj); e == nil {
		t.Fatal("accepted implicit primary")
	}
	obj.Annotations[networkingv1alpha1.InterfaceKeyAnnotation] = fixturePrimaryKey
	pool.Spec.Interfaces[1].ID = pool.Spec.Interfaces[0].ID
	if e := d.Client.Update(context.Background(), pool); e != nil {
		t.Fatal(e)
	}
	if e := d.Default(context.Background(), obj); e == nil {
		t.Fatal("accepted same ENI twice")
	}
	if claimCount(t, d) != 0 {
		t.Fatal("invalid bundle left claims")
	}
}
func TestSingleInterfaceFailPolicyDoesNotFallBack(t *testing.T) {
	d, obj, pool := bundleFixture(t, 3)
	claim := &networkingv1alpha1.ENIClaim{ObjectMeta: metav1.ObjectMeta{Name: fixturePrimaryENI}, Spec: networkingv1alpha1.ENIClaimSpec{ENIID: fixturePrimaryENI, PoolRef: networkingv1alpha1.ClusterObjectReference{Name: pool.Name}}}
	if e := d.Client.Create(context.Background(), claim); e != nil {
		t.Fatal(e)
	}
	if e := d.Default(context.Background(), obj); e == nil {
		t.Fatal("fixed primary fell back to dynamic")
	}
}

func TestInterfaceSetDoesNotShareClaimsAcrossAdmissionRequests(t *testing.T) {
	d, obj, _ := bundleFixture(t, 3)
	obj.Annotations[networkingv1alpha1.AdditionalInterfaceKeysAnnotation] = fixtureAdditionalKeys
	first := admission.NewContextWithRequest(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{UID: "first-admission"}})
	second := admission.NewContextWithRequest(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{UID: "second-admission"}})
	if e := d.Default(first, obj); e != nil {
		t.Fatal(e)
	}
	obj.Spec.NetworkInterfaces = nil
	if e := d.Default(second, obj); e == nil {
		t.Fatal("concurrent admission borrowed another request's claims")
	}
	if claimCount(t, d) != 3 {
		t.Fatal("another admission's reservations were removed")
	}
	if e := d.Default(first, obj); e != nil {
		t.Fatalf("same admission was not idempotent: %v", e)
	}
}
