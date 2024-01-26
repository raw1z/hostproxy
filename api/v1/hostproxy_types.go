/*
Copyright 2024.

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// HostproxySpec defines the desired state of Hostproxy
type HostproxySpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Port of the host which is proxied inside the cluster
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65536
	// +kubebuilder:validation:ExclusiveMaximum=false
	HostPort int32 `json:"hostPort,omitempty"`

	// Port of the service inside the cluster to which the host port is proxied
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65536
	// +kubebuilder:validation:ExclusiveMaximum=false
	ClusterPort int32 `json:"clusterPort,omitempty"`
}

// HostproxyStatus defines the observed state of Hostproxy
type HostproxyStatus struct {
	// Represents the observations of a Hostproxy's current state.
	// Hostproxy.status.conditions.type are: "Available", "Progressing", and "Degraded"
	// Hostproxy.status.conditions.status are one of True, False, Unknown.
	// Hostproxy.status.conditions.reason the value should be a CamelCase string and producers of specific
	// condition types may define expected values and meanings for this field, and whether the values
	// are considered a guaranteed API.
	// Hostproxy.status.conditions.Message is a human readable message indicating details about the transition.
	// For further information see: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Hostproxy is the Schema for the hostproxies API
type Hostproxy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HostproxySpec   `json:"spec,omitempty"`
	Status HostproxyStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// HostproxyList contains a list of Hostproxy
type HostproxyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Hostproxy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Hostproxy{}, &HostproxyList{})
}
