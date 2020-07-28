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

package integration

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io/ioutil"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func StartTestEnv() (*envtest.Environment, *rest.Config) {
	By("bootstrapping test environment")
	apiServerFlags := envtest.DefaultKubeAPIServerFlags[0 : len(envtest.DefaultKubeAPIServerFlags)-1]
	apiServerFlags = append(apiServerFlags, "--enable-admission-plugins=ValidatingAdmissionWebhook")
	apiServerFlags = append(apiServerFlags, "--authorization-mode=RBAC")
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		KubeAPIServerFlags:    apiServerFlags,
	}

	cfg, err := testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	// +kubebuilder:scaffold:scheme

	return testEnv, cfg
}

func GenerateCerts(dir string) error {
	priv, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		return err
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(time.Hour)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Acme Co"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	certOut, err := os.Create(filepath.Join(dir, "tls.crt"))
	if err != nil {
		return err
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return err
	}
	if err := certOut.Close(); err != nil {
		return err
	}

	keyOut, err := os.OpenFile(filepath.Join(dir, "tls.key"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}); err != nil {
		return err
	}
	if err := keyOut.Close(); err != nil {
		return err
	}
	return nil
}

func SetupManager(cfg *rest.Config, withCerts bool) (ctrl.Manager, func() error) {
	By("setting up the manager")
	options := ctrl.Options{
		Scheme:                 scheme.Scheme,
		MetricsBindAddress:     "0",
		HealthProbeBindAddress: "0",
		Port:                   10289,
	}
	tempDir := ""
	if withCerts {
		// Declaring err so that we don't shadow the
		// tempDir variable so that the cleanup func works
		var err error
		tempDir, err = ioutil.TempDir("", "k8sudo-certs-")
		Expect(err).ToNot(HaveOccurred())
		err = GenerateCerts(tempDir)
		Expect(err).ToNot(HaveOccurred())
		options.CertDir = tempDir
	}
	k8sManager, err := ctrl.NewManager(cfg, options)
	Expect(err).ToNot(HaveOccurred())

	return k8sManager, func() error {
		if tempDir != "" {
			return os.RemoveAll(tempDir)
		}
		return nil
	}
}

func StartManager(mgr ctrl.Manager) chan struct{} {
	By("starting the manager")
	stopManager := make(chan struct{})

	go func() {
		defer GinkgoRecover()
		err := mgr.Start(stopManager)
		Expect(err).ToNot(HaveOccurred())
	}()

	return stopManager
}

func Shutdown(testEnv *envtest.Environment, stopManager chan struct{}) {
	By("tearing down the test environment")
	if stopManager != nil {
		close(stopManager)
	}
	if testEnv != nil {
		err := testEnv.Stop()
		Expect(err).ToNot(HaveOccurred())
	}
}
