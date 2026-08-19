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

var _ = Describe("ENIClaim Webhook", func() {
	var (
		obj       *networkingv1alpha1.ENIClaim
		validator ENIClaimCustomValidator
	)

	BeforeEach(func() {
		obj = &networkingv1alpha1.ENIClaim{}
		validator = ENIClaimCustomValidator{}
		Expect(validator).NotTo(BeNil(), "Expected validator to be initialized")
		Expect(obj).NotTo(BeNil(), "Expected obj to be initialized")
	})

	Context("When creating or updating ENIClaim under Validating Webhook", func() {
		It("requires the claim name to equal the ENI ID", func() {
			obj.Name = "wrong-name"
			obj.Spec.ENIID = "eni-0123"
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(MatchError("ENIClaim name must equal spec.eniID"))
		})
	})

})
