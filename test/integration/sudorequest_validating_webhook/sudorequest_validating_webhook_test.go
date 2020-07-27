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
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	k8sudov1alpha1 "jetstack.io/k8sudo/api/v1alpha1"
)

func initSudoRequest(name string) *k8sudov1alpha1.SudoRequest {
	req := &k8sudov1alpha1.SudoRequest{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "k8sudo.jetstack.io/v1alpha1",
			Kind:       "SudoReqeuest",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: k8sudov1alpha1.SudoRequestSpec{},
	}
	return req
}

var _ = Describe("SudoRequest validating webhook", func() {
	const (
		timeout  = 10 * time.Second
		interval = 250 * time.Millisecond
		duration = 2 * time.Second
	)
	Context("When creating SudoRequest", func() {
		It("Should error if User is not set", func() {
			By("Creating a new SudoRequest")
			ctx := context.Background()
			req := initSudoRequest("no-user")
			req.Spec.Role = "role"
			req.Spec.User = ""
			err := k8sClient.Create(ctx, req)
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("User must be set"))
		})

		It("Should error if Role is not set", func() {
			By("Creating a new SudoRequest")
			ctx := context.Background()
			req := initSudoRequest("no-role")
			req.Spec.Role = ""
			req.Spec.User = "user"
			err := k8sClient.Create(ctx, req)
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Role must be set"))
		})
	})
})
