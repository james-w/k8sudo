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
	"net/http"

	"k8s.io/api/admission/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	k8sudov1alpha1 "jetstack.io/k8sudo/api/v1alpha1"
)

// log is for logging in this package.
var sudorequestlog = logf.Log.WithName("sudorequest-resource")

// +kubebuilder:webhook:verbs=create;update,path=/validate-k8sudo-jetstack-io-v1alpha1-sudorequest,mutating=false,failurePolicy=fail,groups=k8sudo.jetstack.io,resources=sudorequests,versions=v1alpha1,name=vsudorequest.kb.io

const (
	SudoRequestValidateWebhookPath = "/validate-k8sudo-jetstack-io-v1alpha1-sudorequest"
)

func SetupWebhookWithManager(mgr ctrl.Manager) error {
	mgr.GetWebhookServer().Register(SudoRequestValidateWebhookPath, &webhook.Admission{Handler: &SudoReqHandler{Client: mgr.GetClient()}})
	return nil
}

type SudoReqHandler struct {
	Client  client.Client
	Decoder *admission.Decoder
}

func (h *SudoReqHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	sudoReq := &k8sudov1alpha1.SudoRequest{}
	err := h.Decoder.Decode(req, sudoReq)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	sudorequestlog.Info("Validating SudoRequest", "SudoRequest", sudoReq)
	if req.AdmissionRequest.Operation == v1beta1.Create || req.AdmissionRequest.Operation == v1beta1.Update {
		if sudoReq.Spec.User == "" {
			return admission.Denied("User must be set")
		}
		if sudoReq.Spec.Role == "" {
			return admission.Denied("Role must be set")
		}
	}
	return admission.Allowed("")
}

func (h *SudoReqHandler) InjectDecoder(d *admission.Decoder) error {
	h.Decoder = d
	return nil
}
