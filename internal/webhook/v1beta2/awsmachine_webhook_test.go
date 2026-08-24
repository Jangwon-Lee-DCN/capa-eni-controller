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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	infrastructurev1beta2 "sigs.k8s.io/cluster-api-provider-aws/v2/api/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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
		It("claims the interface selected by key", func() {
			testScheme := runtime.NewScheme()
			Expect(networkingv1alpha1.AddToScheme(testScheme)).To(Succeed())
			Expect(clusterv1.AddToScheme(testScheme)).To(Succeed())
			Expect(infrastructurev1beta2.AddToScheme(testScheme)).To(Succeed())

			obj = &infrastructurev1beta2.AWSMachine{ObjectMeta: metav1.ObjectMeta{
				Name: "worker-abcde", Namespace: "workload",
				Labels: map[string]string{clusterv1.ClusterNameLabel: "cluster-a"},
				Annotations: map[string]string{
					networkingv1alpha1.AllocateFromPoolAnnotation: "true",
					networkingv1alpha1.InterfaceKeyAnnotation:     "edge-worker-1",
				},
			}}
			cluster := &clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-a", Namespace: "workload"},
				Spec: clusterv1.ClusterSpec{InfrastructureRef: clusterv1.ContractVersionedObjectReference{
					APIGroup: "infrastructure.cluster.x-k8s.io", Kind: "AWSCluster", Name: "cluster-a",
				}},
			}
			awsCluster := &infrastructurev1beta2.AWSCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-a", Namespace: "workload"},
				Spec: infrastructurev1beta2.AWSClusterSpec{
					Region:      "ap-northeast-2",
					NetworkSpec: infrastructurev1beta2.NetworkSpec{VPC: infrastructurev1beta2.VPCSpec{ID: "vpc-0123"}},
				},
			}
			pool := &networkingv1alpha1.ENIPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-a"},
				Spec: networkingv1alpha1.ENIPoolSpec{
					Region: "ap-northeast-2", VPCID: "vpc-0123",
					Interfaces: []networkingv1alpha1.ENIReference{
						{Key: "core-worker-1", ID: "eni-0123", PrivateIP: "10.0.0.10"},
						{Key: "edge-worker-1", ID: "eni-0999", PrivateIP: "10.0.0.20"},
					},
				},
			}
			defaulter.Client = fake.NewClientBuilder().WithScheme(testScheme).WithObjects(cluster, awsCluster, pool).Build()

			Expect(defaulter.Default(ctx, obj)).To(Succeed())
			Expect(obj.Spec.NetworkInterfaces).To(Equal([]string{"eni-0999"}))
		})

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
