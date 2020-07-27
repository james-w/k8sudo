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
	"time"

	"github.com/go-logr/logr"
	authv1 "k8s.io/api/authorization/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	types "k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	k8sudov1alpha1 "jetstack.io/k8sudo/api/v1alpha1"
)

const (
	crbOwnerKey     = ".metadata.controller"
	defaultDuration = 10 * time.Minute
	maxDuration     = 1 * time.Hour
	sudoRequestKind = "SudoRequest"
)

var (
	apiGVStr = k8sudov1alpha1.GroupVersion.String()
)

// IgnoreAlreadyExists returns nil on AlreadyExists errors.
// All other values that are not AlreadyExists errors or nil are returned unmodified.
func IgnoreAlreadyExists(err error) error {
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

// SudoRequestReconciler creates the secret in response to a SudoRequest
type SudoRequestReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	Clock
}

type realClock struct{}

func (_ realClock) Now() time.Time { return time.Now() }

// clock knows how to get the current time.
// It can be used to fake out timing for testing.
type Clock interface {
	Now() time.Time
}

func expiryTime(start, requested time.Time, def, max time.Duration) time.Time {
	var duration time.Duration
	if requested == (time.Time{}) {
		duration = def
	} else {
		duration = requested.Sub(start)
	}
	if duration > max {
		duration = max
	}
	return start.Add(duration)
}

func expiryTimeForRequest(req *k8sudov1alpha1.SudoRequest) time.Time {
	requested := time.Time{}
	if req.Spec.Expires != nil {
		requested = req.Spec.Expires.Time
	}
	return expiryTime(req.GetCreationTimestamp().Time, requested, defaultDuration, maxDuration)
}

// +kubebuilder:rbac:groups=k8sudo.jetstack.io,resources=sudorequests,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=k8sudo.jetstack.io,resources=sudorequests/status,verbs=get;update;patch

func (r *SudoRequestReconciler) updateStatusFromChild(sudoReq *k8sudov1alpha1.SudoRequest, childCRB *rbacv1.ClusterRoleBinding) {
	if childCRB != nil {
		sudoReq.Status.Status = k8sudov1alpha1.SudoRequestStatusReady
		sudoReq.Status.Reason = ""
		// Is this the correct way to reference another object?
		sudoReq.Status.ClusterRoleBinding = childCRB.GetName()
	}

	sudoReq.Status.Expires = &metav1.Time{Time: expiryTimeForRequest(sudoReq)}

	if r.Now().After(sudoReq.Status.Expires.Time) {
		sudoReq.Status.Status = k8sudov1alpha1.SudoRequestStatusExpired
		sudoReq.Status.Reason = ""
	}

	if sudoReq.Status.Status == k8sudov1alpha1.SudoRequestStatusExpired ||
		sudoReq.Status.Status == k8sudov1alpha1.SudoRequestStatusReady {
		return
	}

	if sudoReq.Spec.User == "" {
		sudoReq.Status.Status = k8sudov1alpha1.SudoRequestStatusError
		sudoReq.Status.Reason = "User must be specified"
		return
	}

	if sudoReq.Spec.Role == "" {
		sudoReq.Status.Status = k8sudov1alpha1.SudoRequestStatusError
		sudoReq.Status.Reason = "Target role must be specified"
		return
	}
}

func (r *SudoRequestReconciler) updateStatusFromAccessReview(sudoReq *k8sudov1alpha1.SudoRequest, sar *authv1.SubjectAccessReview) {
	if !sar.Status.Allowed || sar.Status.Denied {
		sudoReq.Status.Status = k8sudov1alpha1.SudoRequestStatusDenied
		sudoReq.Status.Reason = fmt.Sprintf("Failed to authorize: %s", sar.Status.Reason)
		return
	}

	sudoReq.Status.Status = k8sudov1alpha1.SudoRequestStatusPending
	sudoReq.Status.Reason = ""
}

func (r *SudoRequestReconciler) findChildCRB(ctx context.Context, sudoReq *k8sudov1alpha1.SudoRequest, log logr.Logger) (*rbacv1.ClusterRoleBinding, error) {
	childCRB := &rbacv1.ClusterRoleBinding{}
	if err := r.Get(ctx, types.NamespacedName{Name: crbName(sudoReq)}, childCRB); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		log.Error(err, "unable to get child ClusterRoleBinding")
		return nil, err
	}
	return childCRB, nil
}

func (r *SudoRequestReconciler) checkAccess(ctx context.Context, sudoReq *k8sudov1alpha1.SudoRequest, log logr.Logger) (*authv1.SubjectAccessReview, error) {
	sar := &authv1.SubjectAccessReview{
		Spec: authv1.SubjectAccessReviewSpec{
			User: sudoReq.Spec.User,
			ResourceAttributes: &authv1.ResourceAttributes{
				Namespace: "",
				Verb:      "sudo",
				Group:     "rbac.authorization.k8s.io",
				Version:   "v1",
				Resource:  "clusterroles",
				Name:      sudoReq.Spec.Role,
			},
		},
	}
	err := r.Create(ctx, sar)
	if err != nil {
		log.Error(err, "unable to create SubjectAccessReview")
		return sar, nil
	}
	return sar, nil
}

func (r *SudoRequestReconciler) updateStatus(ctx context.Context, sudoReq *k8sudov1alpha1.SudoRequest, log logr.Logger) error {

	childCRB, err := r.findChildCRB(ctx, sudoReq, log)
	if err != nil {
		return err
	}
	r.updateStatusFromChild(sudoReq, childCRB)

	if sudoReq.Status.Status != "" {
		return nil
	}

	sar, err := r.checkAccess(ctx, sudoReq, log)
	if err != nil {
		return err
	}
	r.updateStatusFromAccessReview(sudoReq, sar)

	return nil
}

func crbName(sudoReq *k8sudov1alpha1.SudoRequest) string {
	return fmt.Sprintf("sudo-%s-%s-%s-%s", sudoReq.Spec.User, sudoReq.Spec.Role, sudoReq.Name, sudoReq.CreationTimestamp.Format("2006.01.02.15.04.05"))
}

func (r *SudoRequestReconciler) createClusterRoleBinding(sudoReq *k8sudov1alpha1.SudoRequest, log logr.Logger) (*rbacv1.ClusterRoleBinding, error) {
	name := crbName(sudoReq)
	crb := &rbacv1.ClusterRoleBinding{
		Subjects: []rbacv1.Subject{
			{
				Kind:      "User",
				APIGroup:  "rbac.authorization.k8s.io",
				Name:      sudoReq.Spec.User,
				Namespace: "",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     sudoReq.Spec.Role,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	if err := ctrl.SetControllerReference(sudoReq, crb, r.Scheme); err != nil {
		log.Error(err, "Error setting ownerReference")
		return nil, err
	}
	return crb, nil
}

func (r *SudoRequestReconciler) OnReady(sudoReq *k8sudov1alpha1.SudoRequest) (ctrl.Result, error) {
	return ctrl.Result{RequeueAfter: sudoReq.Status.Expires.Sub(r.Now())}, nil
}

func (r *SudoRequestReconciler) OnPending(ctx context.Context, sudoReq *k8sudov1alpha1.SudoRequest, log logr.Logger) (ctrl.Result, error) {
	crb, err := r.createClusterRoleBinding(sudoReq, log)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err = r.Create(ctx, crb); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Cache is updating, so we haven't realised this is
			// our child yet, requeue so that we see the child
			return ctrl.Result{Requeue: true}, nil
		}
		log.Error(err, "unable to create ClusterRoleBinding")
		return ctrl.Result{}, err
	}

	// Requeue to update the Status to include the reference to the created CRB
	// We wait one second to increase the chance that the CRB will be visible
	// on the API server
	return ctrl.Result{RequeueAfter: time.Second}, nil
}

func (r *SudoRequestReconciler) OnExpired(ctx context.Context, sudoReq *k8sudov1alpha1.SudoRequest, log logr.Logger) (ctrl.Result, error) {
	if sudoReq.Status.ClusterRoleBinding != "" {
		var crb rbacv1.ClusterRoleBinding
		if err := r.Get(ctx, types.NamespacedName{Name: sudoReq.Status.ClusterRoleBinding}, &crb); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		if err := r.Delete(ctx, &crb); err != nil {
			log.Error(err, "failed to delete ClusterRoleBinding")
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
	}
	return ctrl.Result{}, nil
}

func (r *SudoRequestReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("sudorequest", req.NamespacedName)

	var sudoReq k8sudov1alpha1.SudoRequest
	if err := r.Get(ctx, req.NamespacedName, &sudoReq); err != nil {
		log.Error(err, "unable to fetch SudoRequest")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	err := r.updateStatus(ctx, &sudoReq, log)
	if err != nil {
		log.Error(err, "unable to validate request")
		return ctrl.Result{}, err
	}

	if err := r.Status().Update(ctx, &sudoReq); err != nil {
		log.Error(err, "unable to update SudoRequest status")
		return ctrl.Result{}, err
	}

	if sudoReq.Status.Status == k8sudov1alpha1.SudoRequestStatusPending {
		return r.OnPending(ctx, &sudoReq, log)
	}

	if sudoReq.Status.Status == k8sudov1alpha1.SudoRequestStatusReady {
		return r.OnReady(&sudoReq)
	}

	if sudoReq.Status.Status == k8sudov1alpha1.SudoRequestStatusExpired {
		return r.OnExpired(ctx, &sudoReq, log)
	}

	return ctrl.Result{}, nil
}

func (r *SudoRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Clock == nil {
		r.Clock = realClock{}
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&k8sudov1alpha1.SudoRequest{}).
		Complete(r)
}
