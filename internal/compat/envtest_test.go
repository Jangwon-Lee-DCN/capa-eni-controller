package compat

import (
	"context"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	infrastructurev1beta2 "sigs.k8s.io/cluster-api-provider-aws/v2/api/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// Exercise actual API discovery and PATCH against a server offering no v1beta2.
func TestLegacyOnlyAPIServer(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("run make test with envtest assets")
	}
	env := &envtest.Environment{}
	kinds := map[string][2]string{awsMachineKind: {infraGroup, "awsmachines"}, awsClusterKind: {infraGroup, "awsclusters"}, machineKind: {capiGroup, "machines"}, clusterKind: {capiGroup, "clusters"}}
	for kind, info := range kinds {
		env.CRDs = append(env.CRDs, &extv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: info[1] + "." + info[0]}, Spec: extv1.CustomResourceDefinitionSpec{Group: info[0], Names: extv1.CustomResourceDefinitionNames{Kind: kind, ListKind: kind + "List", Plural: info[1]}, Scope: extv1.NamespaceScoped, Versions: []extv1.CustomResourceDefinitionVersion{{Name: legacyVersion, Served: true, Storage: true, Schema: &extv1.CustomResourceValidation{OpenAPIV3Schema: &extv1.JSONSchemaProps{Type: "object", Properties: map[string]extv1.JSONSchemaProps{specField: {Type: "object", XPreserveUnknownFields: new(true)}}}}}}}})
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := env.Stop(); err != nil {
			t.Error(err)
		}
	})
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, infrastructurev1beta2.AddToScheme, clusterv1.AddToScheme} {
		if err = add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	httpClient, err := rest.HTTPClientFor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	mapper, err := apiutil.NewDynamicRESTMapper(cfg, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	base, err := client.New(cfg, client.Options{Scheme: scheme, Mapper: mapper})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = base.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}); err != nil {
		t.Fatal(err)
	}
	c, err := New(base, mapper)
	if err != nil {
		t.Fatal(err)
	}
	for kind, gvk := range c.Versions {
		if gvk.Version != legacyVersion {
			t.Fatalf("%s selected unavailable version %s", kind, gvk.Version)
		}
	}
	wire := c.Object(awsMachineKind)
	wire.SetName("legacy-worker")
	wire.SetNamespace(testNamespace)
	wire.Object[specField] = map[string]any{networkInterfacesField: []any{"eni-primary"}, "oldProviderOption": "preserved"}
	if err = base.Create(ctx, wire); err != nil {
		t.Fatal(err)
	}
	typed := &infrastructurev1beta2.AWSMachine{}
	key := client.ObjectKeyFromObject(wire)
	if err = c.Get(ctx, key, typed); err != nil {
		t.Fatal(err)
	}
	before := typed.DeepCopy()
	typed.Annotations = map[string]string{"allocator-test": "updated"}
	if err = c.Patch(ctx, typed, client.MergeFrom(before)); err != nil {
		t.Fatal(err)
	}
	got := c.Object(awsMachineKind)
	if err = base.Get(ctx, key, got); err != nil {
		t.Fatal(err)
	}
	value, _, err := unstructured.NestedString(got.Object, specField, "oldProviderOption")
	if err != nil || value != "preserved" || got.GetAnnotations()["allocator-test"] != "updated" {
		t.Fatalf("legacy server patch lost fields: %#v %v", got.Object, err)
	}
}
