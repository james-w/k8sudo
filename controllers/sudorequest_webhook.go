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

	"github.com/go-logr/logr"
	"k8s.io/api/admission/v1beta1"
	authv1 "k8s.io/api/authentication/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	k8sudov1alpha1 "jetstack.io/k8sudo/api/v1alpha1"
)

// +kubebuilder:webhook:verbs=create;update,path=/validate-k8sudo-jetstack-io-v1alpha1-sudorequest,mutating=false,failurePolicy=fail,groups=k8sudo.jetstack.io,resources=sudorequests,versions=v1alpha1,name=vsudorequest.kb.io

const (
	SudoRequestValidateWebhookPath = "/validate-k8sudo-jetstack-io-v1alpha1-sudorequest"
)

func (h *SudoReqHandler) SetupWithManager(mgr ctrl.Manager) error {
	mgr.GetWebhookServer().Register(SudoRequestValidateWebhookPath, &webhook.Admission{Handler: h})
	return nil
}

type SudoReqHandler struct {
	Client  client.Client
	Decoder *admission.Decoder
	Log     logr.Logger
}

func (h *SudoReqHandler) ValidateAccess(spec k8sudov1alpha1.SudoRequestSpec, userInfo authv1.UserInfo, log logr.Logger) admission.Response {
	if spec.User != userInfo.Username {
		return admission.Denied(fmt.Sprintf("%s cannot create a SudoRequest for %s", userInfo.Username, spec.User))
	}
	return admission.Allowed("")
}

func Validate(spec k8sudov1alpha1.SudoRequestSpec, log logr.Logger) admission.Response {
	if spec.User == "" {
		return admission.Denied("User must be set")
	}
	if spec.Role == "" {
		return admission.Denied("Role must be set")
	}
	return admission.Allowed("")
}

func (h *SudoReqHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	sudoReq := &k8sudov1alpha1.SudoRequest{}
	err := h.Decoder.Decode(req, sudoReq)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	log := h.Log.WithValues("sudorequest", sudoReq.GetObjectMeta().GetName())
	log.Info("Validating SudoRequest")
	if req.AdmissionRequest.Operation == v1beta1.Create ||
		req.AdmissionRequest.Operation == v1beta1.Update {
		resp := Validate(sudoReq.Spec, log)
		if !resp.Allowed {
			return resp
		}
		return h.ValidateAccess(sudoReq.Spec, req.UserInfo, log)
	}
	return admission.Allowed("")
}

func (h *SudoReqHandler) InjectDecoder(d *admission.Decoder) error {
	h.Decoder = d
	return nil
}
