package v1beta2

import (
	"context"
	"encoding/json"
	"net/http"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	infrastructurev1beta2 "sigs.k8s.io/cluster-api-provider-aws/v2/api/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// AWSMachineAdmission preserves provider-specific fields across supported wire versions.
type AWSMachineAdmission struct{ Defaulter *AWSMachineCustomDefaulter }

func (h *AWSMachineAdmission) Handle(ctx context.Context, req admission.Request) admission.Response {
	if req.Kind.Group != infraGroup || req.Kind.Kind != "AWSMachine" || (req.Kind.Version != "v1beta1" && req.Kind.Version != "v1beta2") {
		return admission.Denied("unsupported AWSMachine API version")
	}
	obj := &infrastructurev1beta2.AWSMachine{}
	if err := json.Unmarshal(req.Object.Raw, obj); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	ctx = admission.NewContextWithRequest(ctx, req)
	if err := h.Defaulter.Default(ctx, obj); err != nil {
		return admission.Denied(err.Error())
	}
	wire := &unstructured.Unstructured{}
	if err := json.Unmarshal(req.Object.Raw, &wire.Object); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	if len(obj.Annotations) > 0 {
		wire.SetAnnotations(obj.Annotations)
	}
	if len(obj.Spec.NetworkInterfaces) > 0 {
		if err := unstructured.SetNestedStringSlice(wire.Object, obj.Spec.NetworkInterfaces, "spec", "networkInterfaces"); err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
	}
	data, err := json.Marshal(wire.Object)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, data)
}
