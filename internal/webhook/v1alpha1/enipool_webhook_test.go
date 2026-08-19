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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	networkingv1alpha1 "github.com/ssu-dcn/capa-eni-controller/api/v1alpha1"
)

var _ = Describe("ENIPool Webhook", func() {
	var (
		obj       *networkingv1alpha1.ENIPool
		validator ENIPoolCustomValidator
		defaulter ENIPoolCustomDefaulter
	)

	BeforeEach(func() {
		obj = &networkingv1alpha1.ENIPool{}
		validator = ENIPoolCustomValidator{}
		Expect(validator).NotTo(BeNil(), "Expected validator to be initialized")
		defaulter = ENIPoolCustomDefaulter{}
		Expect(defaulter).NotTo(BeNil(), "Expected defaulter to be initialized")
		Expect(obj).NotTo(BeNil(), "Expected obj to be initialized")
	})

	Context("When creating ENIPool under Defaulting Webhook", func() {
		It("defaults exhaustion policy to Dynamic", func() {
			Expect(defaulter.Default(ctx, obj)).To(Succeed())
			Expect(obj.Spec.ExhaustionPolicy).To(Equal(networkingv1alpha1.ExhaustionPolicyDynamic))
		})
	})

	Context("When creating or updating ENIPool under Validating Webhook", func() {
		It("rejects duplicate ENIs in one pool", func() {
			obj.Spec.Interfaces = []networkingv1alpha1.ENIReference{{ID: "eni-0123", PrivateIP: "10.0.0.10"}, {ID: "eni-0123", PrivateIP: "10.0.0.11"}}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(MatchError(ContainSubstring("listed more than once")))
		})
	})

})
