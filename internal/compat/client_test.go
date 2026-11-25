package compat

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	infrastructurev1beta2 "sigs.k8s.io/cluster-api-provider-aws/v2/api/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testNamespace = "lab"
const nameField = "name"

func TestServedVersionContracts(t *testing.T) {
	for _, versions := range [][2]string{{legacyVersion, legacyVersion}, {currentVersion, currentVersion}, {legacyVersion, currentVersion}, {currentVersion, legacyVersion}} {
		t.Run(versions[0]+"-capi-"+versions[1], func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := infrastructurev1beta2.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			if err := clusterv1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			infra := infraGroup
			capi := capiGroup
			mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: infra, Version: versions[0]}, {Group: capi, Version: versions[1]}})
			for _, kind := range []string{awsMachineKind, awsClusterKind} {
				mapper.Add(schema.GroupVersionKind{Group: infra, Version: versions[0], Kind: kind}, meta.RESTScopeNamespace)
			}
			for _, kind := range []string{machineKind, clusterKind} {
				mapper.Add(schema.GroupVersionKind{Group: capi, Version: versions[1], Kind: kind}, meta.RESTScopeNamespace)
			}
			obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": infra + "/" + versions[0], kindField: awsMachineKind, metadataField: map[string]any{nameField: "worker", "namespace": testNamespace, "annotations": map[string]any{"preserve": "yes"}}, specField: map[string]any{"instanceType": "c5.4xlarge", networkInterfacesField: []any{"eni-old"}}}}
			if versions[0] == legacyVersion {
				if err := unstructured.SetNestedField(obj.Object, "retain-me", specField, "legacyProviderSetting"); err != nil {
					t.Fatal(err)
				}
			}
			ref := map[string]any{kindField: awsClusterKind, nameField: testNamespace}
			if versions[1] == legacyVersion {
				ref["apiVersion"] = infra + "/" + versions[0]
			} else {
				ref["apiGroup"] = infra
			}
			cluster := &unstructured.Unstructured{Object: map[string]any{"apiVersion": capi + "/" + versions[1], kindField: clusterKind, metadataField: map[string]any{nameField: testNamespace, "namespace": testNamespace}, specField: map[string]any{"infrastructureRef": ref}}}
			base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(obj, cluster).Build()
			c, err := New(base, mapper)
			if err != nil {
				t.Fatal(err)
			}
			key := client.ObjectKey{Namespace: testNamespace, Name: "worker"}
			typed := &infrastructurev1beta2.AWSMachine{}
			if err = c.Get(context.Background(), key, typed); err != nil {
				t.Fatal(err)
			}
			if len(typed.Spec.NetworkInterfaces) != 1 || typed.Spec.NetworkInterfaces[0] != "eni-old" {
				t.Fatalf("lost ENI: %#v", typed.Spec)
			}
			before := typed.DeepCopy()
			typed.Spec.NetworkInterfaces = []string{"eni-primary", "eni-n2", "eni-n4"}
			if err = c.Patch(context.Background(), typed, client.MergeFrom(before)); err != nil {
				t.Fatal(err)
			}
			wire := c.Object(awsMachineKind)
			if err = base.Get(context.Background(), key, wire); err != nil {
				t.Fatal(err)
			}
			preserved, _, _ := unstructured.NestedString(wire.Object, specField, "instanceType")
			ids, _, _ := unstructured.NestedStringSlice(wire.Object, specField, networkInterfacesField)
			if preserved != "c5.4xlarge" || len(ids) != 3 || wire.GetAPIVersion() != infra+"/"+versions[0] {
				t.Fatalf("wire object corrupted: %#v", wire.Object)
			}
			if versions[0] == legacyVersion {
				v, _, _ := unstructured.NestedString(wire.Object, specField, "legacyProviderSetting")
				if v != "retain-me" {
					t.Fatal("legacy-only field lost")
				}
			}
			tc := &clusterv1.Cluster{}
			if err = c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: testNamespace}, tc); err != nil {
				t.Fatal(err)
			}
			if !tc.Spec.InfrastructureRef.IsDefined() || tc.Spec.InfrastructureRef.APIGroup != infra {
				t.Fatalf("legacy infrastructure reference lost: %#v", tc.Spec.InfrastructureRef)
			}
		})
	}
}

func TestUnsupportedProviderFailsDiscovery(t *testing.T) {
	mapper := meta.NewDefaultRESTMapper(nil)
	if _, err := New(fake.NewClientBuilder().Build(), mapper); err == nil {
		t.Fatal("missing providers must not silently select an API version")
	}
}
