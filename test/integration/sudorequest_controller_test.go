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
	"context"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"

	k8sudov1alpha1 "jetstack.io/k8sudo/api/v1alpha1"
)

func lookupKey(sudoRequest *k8sudov1alpha1.SudoRequest) types.NamespacedName {
	return types.NamespacedName{Name: sudoRequest.GetObjectMeta().GetName()}
}

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

func createSudoRequest(ctx context.Context, sudoRequest *k8sudov1alpha1.SudoRequest, timeout, interval time.Duration) *k8sudov1alpha1.SudoRequest {
	Expect(k8sClient.Create(ctx, sudoRequest)).Should(Succeed())
	sudoRequestLookupKey := lookupKey(sudoRequest)
	createdSudoRequest := &k8sudov1alpha1.SudoRequest{}
	Eventually(func() bool {
		_, err := FetchSudoRequest(ctx, sudoRequestLookupKey)
		if err != nil {
			return false
		}
		return true
	}, timeout, interval).Should(BeTrue())
	Eventually(func() bool {
		err := k8sClient.Get(ctx, sudoRequestLookupKey, createdSudoRequest)
		if err != nil {
			return false
		}
		return createdSudoRequest.Status.Status != ""
	}, timeout, interval).Should(BeTrue())
	return createdSudoRequest
}

func FetchSudoRequest(ctx context.Context, key types.NamespacedName) (*k8sudov1alpha1.SudoRequest, error) {
	req := &k8sudov1alpha1.SudoRequest{}
	err := k8sClient.Get(ctx, key, req)
	return req, err
}

func GetStatus(ctx context.Context, key types.NamespacedName) func() (k8sudov1alpha1.SudoRequestStatusStatus, error) {
	return func() (k8sudov1alpha1.SudoRequestStatusStatus, error) {
		req, err := FetchSudoRequest(ctx, key)
		if err != nil {
			return "", err
		}
		return req.Status.Status, nil
	}
}

var _ = Describe("SudoRequest controller", func() {
	const (
		timeout  = 10 * time.Second
		interval = 250 * time.Millisecond
		duration = 2 * time.Second
	)

	Context("When updating SudoRequest status", func() {
		It("Should error if User is not set", func() {
			By("Creating a new SudoRequest")
			ctx := context.Background()
			req := initSudoRequest("no-user")
			req.Spec.Role = "role"
			createdSudoRequest := createSudoRequest(ctx, req, timeout, interval)
			By("Checking the status is Error")
			Consistently(GetStatus(ctx, lookupKey(createdSudoRequest)), duration, interval).Should(Equal(k8sudov1alpha1.SudoRequestStatusError))
			By("Checking the Reason explains the problem")
			Expect(createdSudoRequest.Status.Reason).To(Equal("User must be specified"))
		})
		It("Should error if Role is not set", func() {
			By("Creating a new SudoRequest")
			ctx := context.Background()
			req := initSudoRequest("no-role")
			req.Spec.User = "user"
			createdSudoRequest := createSudoRequest(ctx, req, timeout, interval)
			By("Checking the status is Error")
			Consistently(GetStatus(ctx, lookupKey(createdSudoRequest)), duration, interval).Should(Equal(k8sudov1alpha1.SudoRequestStatusError))
			By("Checking the Reason explains the problem")
			Expect(createdSudoRequest.Status.Reason).To(Equal("Target role must be specified"))
		})
		It("Should set Expired if after request expiration period", func() {
			By("Creating a new SudoRequest")
			ctx := context.Background()
			req := initSudoRequest("expired")
			req.Spec.User = "user"
			req.Spec.Role = "role"
			req.Spec.Expires = &metav1.Time{Time: time.Now()}
			createdSudoRequest := createSudoRequest(ctx, req, timeout, interval)
			By("Checking the status is Expired")
			Consistently(GetStatus(ctx, lookupKey(createdSudoRequest)), duration, interval).Should(Equal(k8sudov1alpha1.SudoRequestStatusExpired))
		})
		It("Should create the CRB if the request is valid", func() {
			By("Creating a new SudoRequest")
			ctx := context.Background()
			req := initSudoRequest("accepted")
			req.Spec.User = "user"
			req.Spec.Role = "role"
			req.Spec.Expires = &metav1.Time{Time: time.Now().Add(3 * time.Second)}
			createdSudoRequest := createSudoRequest(ctx, req, timeout, interval)
			By("Checking the status is Ready")
			Consistently(GetStatus(ctx, lookupKey(createdSudoRequest)), duration, interval).Should(Equal(k8sudov1alpha1.SudoRequestStatusReady))
			By("Checking the CRB is created")
			createdSudoRequest, err := FetchSudoRequest(ctx, lookupKey(createdSudoRequest))
			Expect(err).NotTo(HaveOccurred())
			name := createdSudoRequest.Status.ClusterRoleBinding
			Expect(name).NotTo(Equal(""))
			crb := &rbacv1.ClusterRoleBinding{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, crb)
				if err != nil {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())
			Expect(crb.Subjects).To(HaveLen(1))
			Expect(crb.Subjects[0].Name).To(Equal(req.Spec.User))
			Expect(crb.RoleRef.Name).To(Equal(req.Spec.Role))
			By("Sleep until expiration")
			time.Sleep(time.Until(req.Spec.Expires.Time))
			By("Checking the status is Expired")
			Eventually(GetStatus(ctx, lookupKey(createdSudoRequest)), timeout, interval).Should(Equal(k8sudov1alpha1.SudoRequestStatusExpired))
			By("Checking the CRB is deleted")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, crb)
				if err != nil {
					return false
				}
				return true
			}, timeout, interval).Should(BeFalse())
		})
	})
})
