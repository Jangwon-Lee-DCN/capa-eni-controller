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

package v1beta2

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	infrastructurev1beta2 "sigs.k8s.io/cluster-api-provider-aws/v2/api/v1beta2"

	networkingv1alpha1 "github.com/ssu-dcn/capa-eni-controller/api/v1alpha1"
)

var _ = Describe("AWSMachine Webhook", func() {
	var (
		obj       *infrastructurev1beta2.AWSMachine
		defaulter AWSMachineCustomDefaulter
	)

	BeforeEach(func() {
		obj = &infrastructurev1beta2.AWSMachine{}
		defaulter = AWSMachineCustomDefaulter{}
		Expect(defaulter).NotTo(BeNil(), "Expected defaulter to be initialized")
		Expect(obj).NotTo(BeNil(), "Expected obj to be initialized")
	})

	Context("When creating AWSMachine under Defaulting Webhook", func() {
		It("pauses an opted-in machine", func() {
			obj.Annotations = map[string]string{networkingv1alpha1.AllocateFromPoolAnnotation: "true"}
			Expect(defaulter.Default(ctx, obj)).To(Succeed())
			Expect(obj.Annotations).To(HaveKey(networkingv1alpha1.CAPPausedAnnotation))
			Expect(obj.Annotations[networkingv1alpha1.AllocatorPausedAnnotation]).To(Equal("true"))
		})

		It("does not affect a machine without opt-in", func() {
			Expect(defaulter.Default(ctx, obj)).To(Succeed())
			Expect(obj.Annotations).NotTo(HaveKey(networkingv1alpha1.CAPPausedAnnotation))
		})

		It("preserves a user pause", func() {
			obj.Annotations = map[string]string{
				networkingv1alpha1.AllocateFromPoolAnnotation: "true",
				networkingv1alpha1.CAPPausedAnnotation:        "",
			}
			Expect(defaulter.Default(ctx, obj)).To(Succeed())
			Expect(obj.Annotations).NotTo(HaveKey(networkingv1alpha1.AllocatorPausedAnnotation))
		})
	})

})
