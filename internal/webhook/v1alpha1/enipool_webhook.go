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
	"net/netip"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	networkingv1alpha1 "github.com/ssu-dcn/capa-eni-controller/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var enipoollog = logf.Log.WithName("enipool-resource")

// SetupENIPoolWebhookWithManager registers the webhook for ENIPool in the manager.
func SetupENIPoolWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &networkingv1alpha1.ENIPool{}).
		WithValidator(&ENIPoolCustomValidator{Client: mgr.GetClient()}).
		WithDefaulter(&ENIPoolCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-networking-dcn-ssu-ac-kr-v1alpha1-enipool,mutating=true,failurePolicy=fail,sideEffects=None,groups=networking.dcn.ssu.ac.kr,resources=enipools,verbs=create;update,versions=v1alpha1,name=menipool-v1alpha1.eni.dcn.ssu.ac.kr,admissionReviewVersions=v1

// ENIPoolCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind ENIPool when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type ENIPoolCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind ENIPool.
func (d *ENIPoolCustomDefaulter) Default(_ context.Context, obj *networkingv1alpha1.ENIPool) error {
	enipoollog.Info("Defaulting for ENIPool", "name", obj.GetName())

	if obj.Spec.ExhaustionPolicy == "" {
		obj.Spec.ExhaustionPolicy = networkingv1alpha1.ExhaustionPolicyDynamic
	}
	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-networking-dcn-ssu-ac-kr-v1alpha1-enipool,mutating=false,failurePolicy=fail,sideEffects=None,groups=networking.dcn.ssu.ac.kr,resources=enipools,verbs=create;update,versions=v1alpha1,name=venipool-v1alpha1.eni.dcn.ssu.ac.kr,admissionReviewVersions=v1

// ENIPoolCustomValidator struct is responsible for validating the ENIPool resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type ENIPoolCustomValidator struct {
	Client client.Client
}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type ENIPool.
func (v *ENIPoolCustomValidator) ValidateCreate(ctx context.Context, obj *networkingv1alpha1.ENIPool) (admission.Warnings, error) {
	enipoollog.Info("Validation for ENIPool upon creation", "name", obj.GetName())

	return nil, v.validate(ctx, obj)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type ENIPool.
func (v *ENIPoolCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj *networkingv1alpha1.ENIPool) (admission.Warnings, error) {
	enipoollog.Info("Validation for ENIPool upon update", "name", newObj.GetName())

	return nil, v.validate(ctx, newObj)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type ENIPool.
func (v *ENIPoolCustomValidator) ValidateDelete(_ context.Context, obj *networkingv1alpha1.ENIPool) (admission.Warnings, error) {
	enipoollog.Info("Validation for ENIPool upon deletion", "name", obj.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}

func (v *ENIPoolCustomValidator) validate(ctx context.Context, obj *networkingv1alpha1.ENIPool) error {
	wanted := map[string]struct{}{}
	wantedIPs := map[netip.Addr]struct{}{}
	wantedKeys := map[string]struct{}{}
	for _, configured := range obj.Spec.Interfaces {
		if _, duplicate := wanted[configured.ID]; duplicate {
			return fmt.Errorf("ENI %s is listed more than once", configured.ID)
		}
		wanted[configured.ID] = struct{}{}
		ip, err := netip.ParseAddr(configured.PrivateIP)
		if err != nil || !ip.Is4() {
			return fmt.Errorf("ENI %s privateIP must be a valid IPv4 address", configured.ID)
		}
		if _, duplicate := wantedIPs[ip]; duplicate {
			return fmt.Errorf("private IP %s is listed more than once", configured.PrivateIP)
		}
		wantedIPs[ip] = struct{}{}
		if configured.Key != "" {
			if _, duplicate := wantedKeys[configured.Key]; duplicate {
				return fmt.Errorf("interface key %q is listed more than once", configured.Key)
			}
			wantedKeys[configured.Key] = struct{}{}
		}
	}
	if v.Client == nil {
		return nil
	}
	list := &networkingv1alpha1.ENIPoolList{}
	if err := v.Client.List(ctx, list); err != nil {
		return fmt.Errorf("list ENIPools: %w", err)
	}
	for i := range list.Items {
		other := &list.Items[i]
		if other.Name == obj.Name {
			continue
		}
		if other.Spec.Region == obj.Spec.Region && other.Spec.VPCID == obj.Spec.VPCID {
			return fmt.Errorf("ENIPool %q already owns region %s and VPC %s", other.Name, obj.Spec.Region, obj.Spec.VPCID)
		}
		for _, configured := range other.Spec.Interfaces {
			if _, duplicate := wanted[configured.ID]; duplicate {
				return fmt.Errorf("ENI %s is already registered in ENIPool %q", configured.ID, other.Name)
			}
		}
	}
	return nil
}
