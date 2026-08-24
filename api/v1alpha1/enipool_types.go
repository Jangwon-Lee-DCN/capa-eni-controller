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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ENIPoolSpec defines the desired state of ENIPool
type ENIPoolSpec struct {
	// Region is the AWS region containing every ENI in this pool.
	// +kubebuilder:validation:MinLength=1
	Region string `json:"region"`

	// VPCID is the VPC containing every ENI in this pool.
	// +kubebuilder:validation:Pattern=`^vpc-[0-9a-f]+$`
	VPCID string `json:"vpcID"`

	// Interfaces is the operator-managed set of pre-created ENIs and their expected primary IPv4 addresses.
	// +kubebuilder:validation:MinItems=1
	Interfaces []ENIReference `json:"interfaces"`

	// ExhaustionPolicy controls behavior when no eligible ENI remains.
	// +kubebuilder:validation:Enum=Dynamic;Wait;Fail
	// +kubebuilder:default=Dynamic
	// +optional
	ExhaustionPolicy ExhaustionPolicy `json:"exhaustionPolicy,omitempty"`
}

// ENIReference identifies an ENI and pins its expected primary private IPv4 address.
type ENIReference struct {
	// Key is an optional operator-defined selector used to request this exact ENI.
	// Keys must be unique within a pool.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`
	// +optional
	Key string `json:"key,omitempty"`

	// ID is the AWS network interface ID.
	// +kubebuilder:validation:Pattern=`^eni-[0-9a-f]+$`
	ID string `json:"id"`

	// PrivateIP is the expected primary private IPv4 address of the ENI.
	// +kubebuilder:validation:Format=ipv4
	PrivateIP string `json:"privateIP"`
}

// ExhaustionPolicy controls allocation behavior when an ENIPool is exhausted.
type ExhaustionPolicy string

const (
	// ExhaustionPolicyDynamic lets CAPA allocate networking normally.
	ExhaustionPolicyDynamic ExhaustionPolicy = "Dynamic"
	// ExhaustionPolicyWait leaves the AWSMachine paused until an ENI is available.
	ExhaustionPolicyWait ExhaustionPolicy = "Wait"
	// ExhaustionPolicyFail records a terminal allocation failure.
	ExhaustionPolicyFail ExhaustionPolicy = "Fail"
)

// ENIState is the observed allocation state of an interface.
type ENIState string

const (
	ENIStateAvailable ENIState = "Available"
	ENIStateClaimed   ENIState = "Claimed"
	ENIStateInvalid   ENIState = "Invalid"
)

// ENIObservation contains AWS-discovered data. AZ and subnet are never user inputs.
type ENIObservation struct {
	Key              string                     `json:"key,omitempty"`
	ID               string                     `json:"id"`
	AvailabilityZone string                     `json:"availabilityZone,omitempty"`
	SubnetID         string                     `json:"subnetID,omitempty"`
	PrivateIP        string                     `json:"privateIP,omitempty"`
	State            ENIState                   `json:"state"`
	ClaimRef         *NamespacedObjectReference `json:"claimRef,omitempty"`
	Message          string                     `json:"message,omitempty"`
}

// NamespacedObjectReference identifies a Kubernetes object without coupling the API to a concrete kind.
type NamespacedObjectReference struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// ENIPoolStatus defines the observed state of ENIPool.
type ENIPoolStatus struct {
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	Interfaces []ENIObservation `json:"interfaces,omitempty"`

	// conditions represent the current state of the ENIPool resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Region",type=string,JSONPath=`.spec.region`
// +kubebuilder:printcolumn:name="VPC",type=string,JSONPath=`.spec.vpcID`
// +kubebuilder:printcolumn:name="Policy",type=string,JSONPath=`.spec.exhaustionPolicy`

// ENIPool is the Schema for the enipools API
type ENIPool struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ENIPool
	// +required
	Spec ENIPoolSpec `json:"spec"`

	// status defines the observed state of ENIPool
	// +optional
	Status ENIPoolStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ENIPoolList contains a list of ENIPool
type ENIPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ENIPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ENIPool{}, &ENIPoolList{})
		return nil
	})
}
