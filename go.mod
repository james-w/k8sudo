module jetstack.io/k8sudo

go 1.13

require (
	github.com/go-logr/logr v0.1.0
	github.com/onsi/ginkgo v1.11.0
	github.com/onsi/gomega v1.8.1
	k8s.io/api v0.18.2
	k8s.io/apimachinery v0.18.5
	k8s.io/client-go v0.18.2
	sigs.k8s.io/controller-runtime v0.6.0
)

replace sigs.k8s.io/controller-runtime => github.com/everpeace/controller-runtime v0.6.1-0.20200606083138-7db3b83c1db6
