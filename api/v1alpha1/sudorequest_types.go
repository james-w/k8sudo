/*
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
)

const (
	SudoRequestResourcePath = "sudorequests"
)

// SudoRequestSpec defines the desired state of SudoRequest
type SudoRequestSpec struct {
	// The user to grant permissions to
	User string `json:"user,omitempty"`

	// The Role to give the user access to
	Role string `json:"role,omitempty"`

	// A description of why the escalation is needed
	Reason string `json:"reason,omitempty"`

	// When the request should expire and access should be revoked
	Expires *metav1.Time `json:"expires,omitempty"`
}

type SudoRequestStatusStatus string

const (
	SudoRequestStatusPending SudoRequestStatusStatus = "Pending"
	SudoRequestStatusDenied  SudoRequestStatusStatus = "Denied"
	SudoRequestStatusError   SudoRequestStatusStatus = "Error"
	SudoRequestStatusReady   SudoRequestStatusStatus = "Ready"
	SudoRequestStatusExpired SudoRequestStatusStatus = "Expired"
)

// SudoRequestStatus defines the observed state of SudoRequest
type SudoRequestStatus struct {
	// The status of the request
	Status SudoRequestStatusStatus `json:"status,omitempty"`

	// The reason for the status if known
	Reason string `json:"reason,omitempty"`

	// The secret holding the credentials if the request has been granted
	ClusterRoleBinding string `json:"clusterRoleBinding,omitempty"`

	// When the escalation will expire
	// This applies regardless of what expiration time (if any) is set
	// in the spec.
	Expires *metav1.Time `json:"expires,omitempty"`
}

// +kubebuilder:resource:path=sudorequests,scope=Cluster
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// SudoRequest is the Schema for the sudorequests API
type SudoRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SudoRequestSpec   `json:"spec,omitempty"`
	Status SudoRequestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SudoRequestList contains a list of SudoRequest
type SudoRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SudoRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SudoRequest{}, &SudoRequestList{})
}
