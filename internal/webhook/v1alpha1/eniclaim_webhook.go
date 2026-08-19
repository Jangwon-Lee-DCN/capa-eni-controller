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

package v1alpha1

import (
	"context"
	"fmt"
	"reflect"

	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	networkingv1alpha1 "github.com/ssu-dcn/capa-eni-controller/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var eniclaimlog = logf.Log.WithName("eniclaim-resource")

// SetupENIClaimWebhookWithManager registers the webhook for ENIClaim in the manager.
func SetupENIClaimWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &networkingv1alpha1.ENIClaim{}).
		WithValidator(&ENIClaimCustomValidator{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-networking-dcn-ssu-ac-kr-v1alpha1-eniclaim,mutating=false,failurePolicy=fail,sideEffects=None,groups=networking.dcn.ssu.ac.kr,resources=eniclaims,verbs=create;update,versions=v1alpha1,name=veniclaim-v1alpha1.eni.dcn.ssu.ac.kr,admissionReviewVersions=v1

// ENIClaimCustomValidator struct is responsible for validating the ENIClaim resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type ENIClaimCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type ENIClaim.
func (v *ENIClaimCustomValidator) ValidateCreate(_ context.Context, obj *networkingv1alpha1.ENIClaim) (admission.Warnings, error) {
	eniclaimlog.Info("Validation for ENIClaim upon creation", "name", obj.GetName())

	if obj.Name != obj.Spec.ENIID {
		return nil, fmt.Errorf("ENIClaim name must equal spec.eniID")
	}
	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type ENIClaim.
func (v *ENIClaimCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj *networkingv1alpha1.ENIClaim) (admission.Warnings, error) {
	eniclaimlog.Info("Validation for ENIClaim upon update", "name", newObj.GetName())

	if !reflect.DeepEqual(oldObj.Spec, newObj.Spec) {
		return nil, fmt.Errorf("ENIClaim spec is immutable")
	}
	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type ENIClaim.
func (v *ENIClaimCustomValidator) ValidateDelete(_ context.Context, obj *networkingv1alpha1.ENIClaim) (admission.Warnings, error) {
	eniclaimlog.Info("Validation for ENIClaim upon deletion", "name", obj.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
