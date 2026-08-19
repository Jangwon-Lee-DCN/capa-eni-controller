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

// ENIClaimSpec defines the desired state of ENIClaim
type ENIClaimSpec struct {
	MachineRef NamespacedObjectReference `json:"machineRef"`

	PoolRef ClusterObjectReference `json:"poolRef"`

	// ENIID is the interface reserved by this claim. Its value is immutable.
	// +kubebuilder:validation:Pattern=`^eni-[0-9a-f]+$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="eniID is immutable"
	ENIID string `json:"eniID"`
}

type ClusterObjectReference struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

type ENIClaimPhase string

const (
	ENIClaimPhasePending   ENIClaimPhase = "Pending"
	ENIClaimPhaseBound     ENIClaimPhase = "Bound"
	ENIClaimPhaseReleasing ENIClaimPhase = "Releasing"
	ENIClaimPhaseFailed    ENIClaimPhase = "Failed"
)

// ENIClaimStatus defines the observed state of ENIClaim.
type ENIClaimStatus struct {
	Phase     ENIClaimPhase `json:"phase,omitempty"`
	PrivateIP string        `json:"privateIP,omitempty"`

	// conditions represent the current state of the ENIClaim resource.
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
// +kubebuilder:printcolumn:name="ENI",type=string,JSONPath=`.spec.eniID`
// +kubebuilder:printcolumn:name="Machine",type=string,JSONPath=`.spec.machineRef.name`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`

// ENIClaim is the Schema for the eniclaims API
type ENIClaim struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ENIClaim
	// +required
	Spec ENIClaimSpec `json:"spec"`

	// status defines the observed state of ENIClaim
	// +optional
	Status ENIClaimStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ENIClaimList contains a list of ENIClaim
type ENIClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ENIClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ENIClaim{}, &ENIClaimList{})
		return nil
	})
}
