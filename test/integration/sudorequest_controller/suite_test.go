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

package sudorequest_controller

import (
	"io/ioutil"
	"os"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
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
var k8sManager ctrl.Manager
var testEnv *envtest.Environment
var stopManager chan struct{}
var tempdir string

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"SudoRequest Controller integration Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func(done Done) {
	logf.SetLogger(zap.LoggerTo(GinkgoWriter, true))

	tempdir, err := ioutil.TempDir("", "k8sudo-tests-")
	Expect(err).ToNot(HaveOccurred())

	username := "user"
	password := "pass"

	testEnv := integration.StartTestEnv(tempdir, username, password)
	scheme := scheme.Scheme
	k8sManager := integration.SetupManager(testEnv.Config, scheme, false, tempdir)

	err = k8sudov1alpha1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	err = (&controllers.SudoRequestReconciler{
		Client: k8sManager.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("SudoRequest"),
		Scheme: scheme,
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	stopManager = integration.StartManager(k8sManager)

	k8sClient = k8sManager.GetClient()
	Expect(k8sClient).ToNot(BeNil())

	close(done)
}, 60)

var _ = AfterSuite(func() {
	integration.Shutdown(testEnv, stopManager)
	err := os.RemoveAll(tempdir)
	Expect(err).ToNot(HaveOccurred())
})
