package v1beta2

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestAdmissionWireVersions(t *testing.T) {
	for _, version := range []string{"v1beta1", "v1beta2"} {
		t.Run(version, func(t *testing.T) {
			raw := []byte(`{"apiVersion":"infrastructure.cluster.x-k8s.io/` + version + `","kind":"AWSMachine","metadata":{"name":"worker","namespace":"lab","annotations":{"eni.dcn.ssu.ac.kr/allocate-from-pool":"true"}},"spec":{"legacyProviderSetting":"preserve-this"}}`)
			req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Kind: metav1.GroupVersionKind{Group: infraGroup, Version: version, Kind: "AWSMachine"}, Object: runtime.RawExtension{Raw: raw}}}
			h := &AWSMachineAdmission{Defaulter: &AWSMachineCustomDefaulter{}}
			response := h.Handle(context.Background(), req)
			if !response.Allowed || len(response.Patches) == 0 {
				t.Fatalf("expected mutation: %#v", response)
			}
			for _, patch := range response.Patches {
				if !strings.HasPrefix(patch.Path, "/metadata/annotations/") {
					t.Fatalf("provider fields changed: %#v", patch)
				}
			}
			req.DryRun = new(true)
			response = h.Handle(context.Background(), req)
			if !response.Allowed {
				t.Fatalf("dry-run rejected: %#v", response)
			}
			req.Kind.Version = "v1alpha4"
			if h.Handle(context.Background(), req).Allowed {
				t.Fatal("unsupported version accepted")
			}
			req.Kind.Version = version
			req.Object.Raw = json.RawMessage("{")
			if h.Handle(context.Background(), req).Allowed {
				t.Fatal("malformed object accepted")
			}
		})
	}
}
