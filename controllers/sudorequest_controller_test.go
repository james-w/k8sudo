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
	"reflect"
	"testing"
	"time"

	testinglogr "github.com/go-logr/logr/testing"
	authv1 "k8s.io/api/authorization/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	k8sudov1alpha1 "jetstack.io/k8sudo/api/v1alpha1"
)

func TestIgnoreAlreadyExists(t *testing.T) {
	badRequest := apierrors.NewBadRequest("test")
	tests := []struct {
		name     string
		input    error
		expected error
	}{
		{
			name:     "nil -> nil",
			input:    nil,
			expected: nil,
		},
		{
			name:     "err -> nil",
			input:    badRequest,
			expected: badRequest,
		},
		{
			name:     "already exists -> nil",
			input:    apierrors.NewAlreadyExists(schema.GroupResource{}, "bar"),
			expected: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, want := IgnoreAlreadyExists(test.input), test.expected; got != want {
				t.Errorf("IgnoreAlreadyExists gave wrong result, got: %v != want: %v", got, want)
			}
		})
	}
}

func TestExpiryTime(t *testing.T) {
	now := time.Now()
	never := time.Time{}
	inOneHour := now.Add(time.Hour)
	oneHourAgo := now.Add(-1 * time.Hour)

	tests := []struct {
		name      string
		start     time.Time
		requested time.Time
		def       time.Duration
		max       time.Duration
		expected  time.Time
	}{
		{
			name:      "no requested",
			start:     now,
			requested: never,
			def:       1 * time.Minute,
			max:       1 * time.Hour,
			expected:  now.Add(1 * time.Minute),
		},
		{
			name:      "def > max",
			start:     now,
			requested: never,
			def:       1 * time.Hour,
			max:       1 * time.Minute,
			expected:  now.Add(1 * time.Minute),
		},
		{
			name:      "uses requested",
			start:     now,
			requested: inOneHour,
			def:       1 * time.Minute,
			max:       2 * time.Hour,
			expected:  inOneHour,
		},
		{
			name:      "requested > max",
			start:     now,
			requested: inOneHour,
			def:       1 * time.Minute,
			max:       2 * time.Minute,
			expected:  now.Add(2 * time.Minute),
		},
		{
			name:      "requested < start",
			start:     now,
			requested: oneHourAgo,
			def:       1 * time.Minute,
			max:       2 * time.Minute,
			expected:  oneHourAgo,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, want := expiryTime(test.start, test.requested, test.def, test.max), test.expected; got != want {
				t.Errorf("got wrong expiry time: (got != want) %s != %s", got, want)
			}
		})
	}
}

func TestExpiryTimeForRequest(t *testing.T) {
	now := time.Now()
	never := time.Time{}
	inOneHour := now.Add(time.Hour)

	tests := []struct {
		name              string
		creationTimestamp time.Time
		requested         time.Time
		expected          time.Time
	}{
		{
			name:              "no requested",
			creationTimestamp: now,
			requested:         never,
			expected:          now.Add(defaultDuration),
		},
		{
			name:              "uses requested",
			creationTimestamp: now,
			requested:         inOneHour,
			expected:          inOneHour,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := &k8sudov1alpha1.SudoRequest{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Time{Time: test.creationTimestamp},
				},
				Spec: k8sudov1alpha1.SudoRequestSpec{},
			}
			if test.requested != (time.Time{}) {
				req.Spec.Expires = &metav1.Time{Time: test.requested}
			}
			if got, want := expiryTimeForRequest(req), test.expected; got != want {
				t.Errorf("got wrong expiry time: (got != want) %s != %s", got, want)
			}
		})
	}
}

type FakeClock struct {
	CurrentTime time.Time
}

func (c FakeClock) Now() time.Time { return c.CurrentTime }

func TestUpdateStatusFromChild(t *testing.T) {
	crbName := "sudo-crb"
	creationTimestamp := time.Now()

	childCRB := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: crbName,
		},
	}

	tests := []struct {
		name            string
		childCRB        *rbacv1.ClusterRoleBinding
		user            string
		role            string
		expectedStatus  k8sudov1alpha1.SudoRequestStatusStatus
		expectedReason  string
		expectedCRBName string
		expectedExpires time.Time
		currentTime     time.Time
	}{
		{
			name:            "with child",
			childCRB:        childCRB,
			expectedStatus:  k8sudov1alpha1.SudoRequestStatusReady,
			expectedReason:  "",
			expectedCRBName: crbName,
			expectedExpires: creationTimestamp.Add(defaultDuration),
			currentTime:     creationTimestamp,
		},
		{
			name:            "with child expired",
			childCRB:        childCRB,
			expectedStatus:  k8sudov1alpha1.SudoRequestStatusExpired,
			expectedReason:  "",
			expectedCRBName: crbName,
			expectedExpires: creationTimestamp.Add(defaultDuration),
			currentTime:     creationTimestamp.Add(defaultDuration).Add(time.Second),
		},
		{
			name:            "without child expired",
			childCRB:        nil,
			expectedStatus:  k8sudov1alpha1.SudoRequestStatusExpired,
			expectedReason:  "",
			expectedCRBName: "",
			expectedExpires: creationTimestamp.Add(defaultDuration),
			currentTime:     creationTimestamp.Add(defaultDuration).Add(time.Second),
		},
		{
			name:     "without child valid",
			childCRB: nil,
			user:     "user",
			role:     "role",
			// It doesn't set the status as pending as it hasn't confirmed
			// that the user is allowed to assume that role, so it leaves
			// it empty
			expectedStatus:  "",
			expectedReason:  "",
			expectedCRBName: "",
			expectedExpires: creationTimestamp.Add(defaultDuration),
			currentTime:     creationTimestamp,
		},
		{
			name:            "without child missing user",
			childCRB:        nil,
			user:            "",
			role:            "",
			expectedStatus:  k8sudov1alpha1.SudoRequestStatusError,
			expectedReason:  "User must be specified",
			expectedCRBName: "",
			expectedExpires: creationTimestamp.Add(defaultDuration),
			currentTime:     creationTimestamp,
		},
		{
			name:            "without child missing role",
			childCRB:        nil,
			user:            "user",
			role:            "",
			expectedStatus:  k8sudov1alpha1.SudoRequestStatusError,
			expectedReason:  "Target role must be specified",
			expectedCRBName: "",
			expectedExpires: creationTimestamp.Add(defaultDuration),
			currentTime:     creationTimestamp,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := &k8sudov1alpha1.SudoRequest{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Time{Time: creationTimestamp},
				},
				Spec: k8sudov1alpha1.SudoRequestSpec{
					User: test.user,
					Role: test.role,
				},
			}
			clock := FakeClock{
				CurrentTime: test.currentTime,
			}
			r := &SudoRequestReconciler{
				Clock: clock,
			}
			r.updateStatusFromChild(req, test.childCRB)
			if got, want := req.Status.Status, test.expectedStatus; got != want {
				t.Errorf("wrong status: (got != want) %s != %s", got, want)
			}
			if got, want := req.Status.Reason, test.expectedReason; got != want {
				t.Errorf("wrong reason: (got != want) %s != %s", got, want)
			}
			if got, want := req.Status.ClusterRoleBinding, test.expectedCRBName; got != want {
				t.Errorf("wrong ClusterRoleBinding name: (got != want) %s != %s", got, want)
			}
			if got, want := req.Status.Expires.Time, test.expectedExpires; got != want {
				t.Errorf("wrong expires: (got != want) %v != %v", got, want)
			}
		})
	}
}

func TestUpdateStatusFromAccessReview(t *testing.T) {
	tests := []struct {
		name            string
		sar             *authv1.SubjectAccessReview
		expectedStatus  k8sudov1alpha1.SudoRequestStatusStatus
		expectedReason  string
		expectedCRBName string
	}{
		{
			name: "not allowed",
			sar: &authv1.SubjectAccessReview{
				Status: authv1.SubjectAccessReviewStatus{
					Allowed: false,
					Denied:  false,
					Reason:  "not allowed",
				},
			},
			expectedStatus:  k8sudov1alpha1.SudoRequestStatusDenied,
			expectedReason:  "Failed to authorize: not allowed",
			expectedCRBName: "",
		},
		{
			name: "denied",
			sar: &authv1.SubjectAccessReview{
				Status: authv1.SubjectAccessReviewStatus{
					Allowed: true,
					Denied:  true,
					Reason:  "denied",
				},
			},
			expectedStatus:  k8sudov1alpha1.SudoRequestStatusDenied,
			expectedReason:  "Failed to authorize: denied",
			expectedCRBName: "",
		},
		{
			name: "allowed",
			sar: &authv1.SubjectAccessReview{
				Status: authv1.SubjectAccessReviewStatus{
					Allowed: true,
					Denied:  false,
					Reason:  "allowed",
				},
			},
			expectedStatus:  k8sudov1alpha1.SudoRequestStatusPending,
			expectedReason:  "",
			expectedCRBName: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := &k8sudov1alpha1.SudoRequest{}
			clock := FakeClock{}
			r := &SudoRequestReconciler{
				Clock: clock,
			}
			r.updateStatusFromAccessReview(req, test.sar)
			if got, want := req.Status.Status, test.expectedStatus; got != want {
				t.Errorf("wrong status: (got != want) %s != %s", got, want)
			}
			if got, want := req.Status.Reason, test.expectedReason; got != want {
				t.Errorf("wrong reason: (got != want) %s != %s", got, want)
			}
			if got, want := req.Status.ClusterRoleBinding, test.expectedCRBName; got != want {
				t.Errorf("wrong ClusterRoleBinding name: (got != want) %s != %s", got, want)
			}
			var noTime *metav1.Time
			if got, want := req.Status.Expires, noTime; got != want {
				t.Errorf("wrong expires: (got != want) %v != %v", got, want)
			}
		})
	}
}

func TestCreateClusterRoleBinding(t *testing.T) {
	user := "user"
	role := "role"
	reqName := "sudo-req"
	creationTimestamp := time.Now()

	req := &k8sudov1alpha1.SudoRequest{
		Spec: k8sudov1alpha1.SudoRequestSpec{
			User: user,
			Role: role,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              reqName,
			CreationTimestamp: metav1.Time{Time: creationTimestamp},
		},
	}

	clock := FakeClock{}
	scheme := runtime.NewScheme()
	k8sudov1alpha1.AddToScheme(scheme)
	r := &SudoRequestReconciler{
		Clock:  clock,
		Scheme: scheme,
	}
	log := testinglogr.TestLogger{T: t}
	crb, err := r.createClusterRoleBinding(req, log)
	if err != nil {
		t.Fatalf("error creating CRB: %v", err)
	}
	if got, want := crb.Subjects, ([]rbacv1.Subject{{Kind: "User", APIGroup: "rbac.authorization.k8s.io", Name: user}}); !reflect.DeepEqual(got, want) {
		t.Errorf("wrong Subject: (got != want) %#v != %#v", got, want)
	}
	if got, want := crb.RoleRef, (rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: role}); !reflect.DeepEqual(got, want) {
		t.Errorf("wrong RoleRef: (got != want) %#v != %#v", got, want)
	}

	expectedName := fmt.Sprintf("sudo-%s-%s-%s-%s", user, role, reqName, creationTimestamp.Format("2006.01.02.15.04.05"))
	if got, want := crb.ObjectMeta.Name, expectedName; got != want {
		t.Errorf("wrong Name: (got != want) %s != %s", got, want)
	}
}

func TestFindChildCRB(t *testing.T) {
	req := k8sudov1alpha1.SudoRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "sudo-req",
			CreationTimestamp: metav1.Time{Time: time.Now()},
		},
		Spec: k8sudov1alpha1.SudoRequestSpec{
			User: "user",
			Role: "role",
		},
	}

	validCRB := rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: crbName(&req),
		},
	}

	invalidCRB := rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: crbName(&req) + "-wrong",
		},
	}

	tests := []struct {
		name          string
		crbs          []rbacv1.ClusterRoleBinding
		expectError   bool
		expectedChild *rbacv1.ClusterRoleBinding
	}{
		{
			name:          "child exists",
			crbs:          []rbacv1.ClusterRoleBinding{validCRB},
			expectError:   false,
			expectedChild: &validCRB,
		},
		{
			name:          "child does not exist",
			crbs:          []rbacv1.ClusterRoleBinding{invalidCRB},
			expectError:   false,
			expectedChild: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := FakeClock{}
			scheme := runtime.NewScheme()
			k8sudov1alpha1.AddToScheme(scheme)
			rbacv1.AddToScheme(scheme)
			client := fake.NewFakeClientWithScheme(scheme, &rbacv1.ClusterRoleBindingList{Items: test.crbs})
			r := &SudoRequestReconciler{
				Clock:  clock,
				Scheme: scheme,
				Client: client,
			}
			log := testinglogr.TestLogger{T: t}
			ctx := context.Background()
			child, err := r.findChildCRB(ctx, &req, log)
			if got, want := (err != nil), test.expectError; got != want {
				t.Errorf("unexpected error state: (got != want) %t != %t: %v", got, want, err)
			}
			if got, want := child, test.expectedChild; !reflect.DeepEqual(got, want) {
				t.Errorf("wrong child: (got != want) %+v != %+v", got, want)
			}
		})
	}
}
