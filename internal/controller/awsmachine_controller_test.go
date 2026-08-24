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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	infrastructurev1beta2 "sigs.k8s.io/cluster-api-provider-aws/v2/api/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	networkingv1alpha1 "github.com/ssu-dcn/capa-eni-controller/api/v1alpha1"
)

var _ = Describe("AWSMachine Controller", func() {
	Context("When allocating an ENI", func() {
		It("claims the ENI selected by the owner Machine annotation and unpauses the AWSMachine", func() {
			testScheme := runtime.NewScheme()
			Expect(networkingv1alpha1.AddToScheme(testScheme)).To(Succeed())
			Expect(clusterv1.AddToScheme(testScheme)).To(Succeed())
			Expect(infrastructurev1beta2.AddToScheme(testScheme)).To(Succeed())

			awsMachine := &infrastructurev1beta2.AWSMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "worker-abcde",
					Namespace: "workload",
					Annotations: map[string]string{
						networkingv1alpha1.AllocateFromPoolAnnotation: "true",
						networkingv1alpha1.AllocatorPausedAnnotation:  "true",
						networkingv1alpha1.CAPPausedAnnotation:        "",
					},
					OwnerReferences: []metav1.OwnerReference{{APIVersion: "cluster.x-k8s.io/v1beta2", Kind: "Machine", Name: "worker-abcde"}},
				},
			}
			machine := &clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name: "worker-abcde", Namespace: "workload",
					Annotations: map[string]string{networkingv1alpha1.InterfaceKeyAnnotation: "edge-worker-1"},
				},
				Spec: clusterv1.MachineSpec{ClusterName: "cluster-a"},
			}
			cluster := &clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-a", Namespace: "workload"},
				Spec: clusterv1.ClusterSpec{InfrastructureRef: clusterv1.ContractVersionedObjectReference{
					APIGroup: "infrastructure.cluster.x-k8s.io", Kind: "AWSCluster", Name: "cluster-a",
				}},
			}
			awsCluster := &infrastructurev1beta2.AWSCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-a", Namespace: "workload"},
				Spec:       infrastructurev1beta2.AWSClusterSpec{Region: "ap-northeast-2", NetworkSpec: infrastructurev1beta2.NetworkSpec{VPC: infrastructurev1beta2.VPCSpec{ID: "vpc-0123"}}},
			}
			pool := &networkingv1alpha1.ENIPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-a"},
				Spec: networkingv1alpha1.ENIPoolSpec{Region: "ap-northeast-2", VPCID: "vpc-0123", Interfaces: []networkingv1alpha1.ENIReference{
					{Key: "edge-worker-1", ID: "eni-0999", PrivateIP: "10.0.0.20"},
					{Key: "core-worker-1", ID: "eni-0123", PrivateIP: "10.0.0.10"},
				}, ExhaustionPolicy: networkingv1alpha1.ExhaustionPolicyDynamic},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(awsMachine, machine, cluster, awsCluster, pool).Build()
			reconciler := &AWSMachineReconciler{Client: fakeClient, Scheme: testScheme}

			_, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "workload", Name: "worker-abcde"}})
			Expect(err).NotTo(HaveOccurred())

			updated := &infrastructurev1beta2.AWSMachine{}
			Expect(fakeClient.Get(context.Background(), types.NamespacedName{Namespace: "workload", Name: "worker-abcde"}, updated)).To(Succeed())
			Expect(updated.Spec.NetworkInterfaces).To(Equal([]string{"eni-0999"}))
			Expect(updated.Annotations).NotTo(HaveKey(networkingv1alpha1.CAPPausedAnnotation))
			Expect(updated.Annotations[networkingv1alpha1.AllocationResultAnnotation]).To(Equal(networkingv1alpha1.AllocationResultAllocated))

			claim := &networkingv1alpha1.ENIClaim{}
			Expect(fakeClient.Get(context.Background(), types.NamespacedName{Name: "eni-0999"}, claim)).To(Succeed())
			Expect(claim.Spec.MachineRef.Name).To(Equal("worker-abcde"))
		})
	})
})
