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

package controllers

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	testinglogr "github.com/go-logr/logr/testing"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	authv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	k8sudov1alpha1 "jetstack.io/k8sudo/api/v1alpha1"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name     string
		spec     k8sudov1alpha1.SudoRequestSpec
		expected admission.Response
	}{
		{
			name: "no user",
			spec: k8sudov1alpha1.SudoRequestSpec{
				Role: "role",
			},
			expected: admission.Denied("User must be set"),
		},
		{
			name: "no role",
			spec: k8sudov1alpha1.SudoRequestSpec{
				User: "user",
			},
			expected: admission.Denied("Role must be set"),
		},
		{
			name: "valid",
			spec: k8sudov1alpha1.SudoRequestSpec{
				User: "user",
				Role: "role",
			},
			expected: admission.Allowed(""),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log := testinglogr.TestLogger{T: t}
			resp := Validate(test.spec, log)
			if got, want := resp, test.expected; !reflect.DeepEqual(got, want) {
				t.Errorf("unexpected response: (got != want) %v != %v", got, want)
			}
		})
	}
}

func TestValidateAccess(t *testing.T) {
	user1 := "user1"
	user2 := "user2"
	tests := []struct {
		name     string
		spec     k8sudov1alpha1.SudoRequestSpec
		username string
		expected admission.Response
	}{
		{
			name: "same username",
			spec: k8sudov1alpha1.SudoRequestSpec{
				User: user1,
			},
			username: user1,
			expected: admission.Allowed(""),
		},
		{
			name: "different username",
			spec: k8sudov1alpha1.SudoRequestSpec{
				User: user2,
			},
			username: user1,
			expected: admission.Denied(fmt.Sprintf("%s cannot create a SudoRequest for %s", user1, user2)),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log := testinglogr.TestLogger{T: t}
			h := &SudoReqHandler{
				Log: log,
			}
			resp := h.ValidateAccess(test.spec, authv1.UserInfo{Username: test.username}, log)
			if got, want := resp, test.expected; !reflect.DeepEqual(got, want) {
				t.Errorf("wrong response: (got != want) %+v != %+v", got, want)
			}
		})
	}
}

func TestHandle(t *testing.T) {
	tests := []struct {
		name      string
		req       string
		username  string
		operation admissionv1beta1.Operation
		expected  admission.Response
	}{
		{
			name:      "allowed",
			operation: admissionv1beta1.Create,
			username:  "user",
			req:       "{\"spec\": {\"user\": \"user\", \"role\": \"role\"}}",
			expected:  admission.Allowed(""),
		},
		{
			name:      "invalid spec",
			operation: admissionv1beta1.Create,
			// No user value
			req:      "{\"spec\": {\"user\": \"\", \"role\": \"role\"}}",
			expected: admission.Denied("User must be set"),
		},
		{
			name:      "disallowed",
			operation: admissionv1beta1.Create,
			username:  "user1",
			req:       "{\"spec\": {\"user\": \"user2\", \"role\": \"role\"}}",
			expected:  admission.Denied("user1 cannot create a SudoRequest for user2"),
		},
		{
			name:     "malformed",
			req:      "",
			expected: admission.Errored(http.StatusBadRequest, fmt.Errorf("there is no content to decode")),
		},
		{
			name:      "delete",
			operation: admissionv1beta1.Delete,
			req:       "{}",
			expected:  admission.Allowed(""),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log := testinglogr.TestLogger{T: t}
			h := &SudoReqHandler{
				Log: log,
			}
			decoder, err := admission.NewDecoder(scheme.Scheme)
			if err != nil {
				t.Fatalf("error creating decoder: %s", err)
			}
			h.InjectDecoder(decoder)
			ctx := context.Background()
			req := admissionv1beta1.AdmissionRequest{
				Operation: test.operation,
				Object: runtime.RawExtension{
					Raw: []byte(test.req),
				},
				UserInfo: authv1.UserInfo{
					Username: test.username,
				},
			}
			resp := h.Handle(ctx, admission.Request{AdmissionRequest: req})
			if got, want := resp, test.expected; !reflect.DeepEqual(got, want) {
				t.Errorf("unexpected response: (got != want) %v != %v", got, want)
			}
		})
	}
}
