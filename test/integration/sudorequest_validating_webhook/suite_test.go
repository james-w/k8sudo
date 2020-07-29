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

package sudorequest_validating_webhook

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	k8sudov1alpha1 "jetstack.io/k8sudo/api/v1alpha1"
	"jetstack.io/k8sudo/controllers"
	"jetstack.io/k8sudo/test/integration"
	// +kubebuilder:scaffold:imports
)

var k8sClient client.Client

const k8sUsername = "user"

var k8sRootClient client.Client
var k8sManager ctrl.Manager
var testEnv *envtest.Environment
var stopManager chan struct{}
var tempdir string

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"SudoRequest validating webhook integration Suite",
		[]Reporter{printer.NewlineReporter{}})
}

func EnableWebhook() {
	caBundle, err := ioutil.ReadFile(filepath.Join(k8sManager.GetWebhookServer().CertDir, "tls.crt"))
	Expect(err).ShouldNot(HaveOccurred())
	wh := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	wh.Name = "sudorequest-hook"
	ctx := context.Background()
	_, err = ctrl.CreateOrUpdate(ctx, k8sClient, wh, func() error {
		failPolicy := admissionregistrationv1.Fail
		urlStr := fmt.Sprintf("https://127.0.0.1:%d%s", k8sManager.GetWebhookServer().Port, controllers.SudoRequestValidateWebhookPath)
		sideEffect := admissionregistrationv1.SideEffectClassNone
		wh.Webhooks = []admissionregistrationv1.ValidatingWebhook{
			{
				Name:          "validate.k8sudo.jetstack.io",
				FailurePolicy: &failPolicy,
				ClientConfig: admissionregistrationv1.WebhookClientConfig{
					CABundle: caBundle,
					URL:      &urlStr,
				},
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
						},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{k8sudov1alpha1.GroupVersion.Group},
							APIVersions: []string{k8sudov1alpha1.GroupVersion.Version},
							Resources:   []string{k8sudov1alpha1.SudoRequestResourcePath},
						},
					},
				},
				SideEffects:             &sideEffect,
				AdmissionReviewVersions: []string{"v1beta1"},
			},
		}
		return nil
	})
	Expect(err).ShouldNot(HaveOccurred())
}

var _ = BeforeSuite(func(done Done) {
	logf.SetLogger(zap.LoggerTo(GinkgoWriter, true))

	tempdir, err := ioutil.TempDir("", "k8sudo-tests-")
	Expect(err).ToNot(HaveOccurred())

	password := "pass"

	testEnv = integration.StartTestEnv(tempdir, k8sUsername, password)
	scheme := scheme.Scheme
	k8sManager = integration.SetupManager(testEnv.Config, scheme, true, tempdir)

	err = k8sudov1alpha1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	err = (&controllers.SudoReqHandler{
		Client: k8sManager.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("SudoRequest"),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	stopManager = integration.StartManager(k8sManager)
	k8sClient = k8sManager.GetClient()
	Expect(k8sClient).ToNot(BeNil())

	d := &net.Dialer{Timeout: time.Second}
	Eventually(func() error {
		conn, err := tls.DialWithDialer(d, "tcp", fmt.Sprintf("127.0.0.1:%d", k8sManager.GetWebhookServer().Port), &tls.Config{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return err
		}
		conn.Close()
		return nil
	}).Should(Succeed())

	k8sManager.GetClient().Create(context.TODO(), &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sudorequests-admin",
		},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{k8sudov1alpha1.GroupVersion.Group},
			Resources: []string{"sudorequests"},
			Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
		}},
	})
	k8sManager.GetClient().Create(context.TODO(), &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "user-sudorequests-admin",
		},
		RoleRef: rbacv1.RoleRef{
			Name:     "sudorequests-admin",
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     "User",
				Name:     k8sUsername,
				APIGroup: "rbac.authorization.k8s.io",
			},
		},
	})

	EnableWebhook()

	cfg := rest.CopyConfig(testEnv.SecureConfig)
	cfg.Username = k8sUsername
	cfg.Password = password
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).ToNot(BeNil())
	k8sRootClient = k8sManager.GetClient()
	Expect(k8sRootClient).ToNot(BeNil())

	close(done)
}, 60)

var _ = AfterSuite(func() {
	integration.Shutdown(testEnv, stopManager)
	if tempdir != "" {
		err := os.RemoveAll(tempdir)
		Expect(err).ToNot(HaveOccurred())
	}
})
