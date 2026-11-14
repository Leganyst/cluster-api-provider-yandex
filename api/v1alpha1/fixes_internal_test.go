package v1alpha1

import (
	"context"
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// Test_isFQDN checks that the host validation is purely syntactic:
// the result must not depend on whether the name can be resolved.
func Test_isFQDN(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{host: "ya.ru", want: true},
		// Valid name which is not resolvable from the machine running the test.
		{host: "api.capy-test.cluster.internal", want: true},
		{host: "k8s-api.example.com", want: true},
		// A single label is not a fully qualified name.
		{host: "yaru", want: false},
		{host: "yaru.", want: false},
		{host: "", want: false},
		// Malformed IPv4: the last label must not be all-numeric.
		{host: "1000.10.10.10", want: false},
		{host: "Upper.Example.com", want: false},
		{host: "bad_name.example.com", want: false},
		{host: "2a02:6b8:a::a", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			NewWithT(t).Expect(isFQDN(tt.host)).To(Equal(tt.want))
		})
	}
}

// Test_yandexClusterAdmitter_Handle_identityChanged checks that changing the identity of a cluster
// replaces the identity label instead of leaving the label of the previous identity behind.
func Test_yandexClusterAdmitter_Handle_identityChanged(t *testing.T) {
	g := NewWithT(t)

	scheme := runtime.NewScheme()
	g.Expect(AddToScheme(scheme)).To(Succeed())
	m := &yandexClusterAdmitter{decoder: admission.NewDecoder(scheme)}

	raw := func(c *YandexCluster) runtime.RawExtension {
		b, err := json.Marshal(c)
		g.Expect(err).NotTo(HaveOccurred())
		return runtime.RawExtension{Raw: b}
	}

	oldCluster := &YandexCluster{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
			"yandexidentity/old-namespace": "old-name",
			// A label whose key equals the name of the old identity must survive: the previous
			// implementation deleted the label with the key equal to the old label value.
			"old-name": "keep-me",
		}},
		Spec: YandexClusterSpec{IdentityRef: &IdentityReference{Name: "old-name", Namespace: "old-namespace"}},
	}
	newCluster := oldCluster.DeepCopy()
	newCluster.Spec.IdentityRef = &IdentityReference{Name: "new-name", Namespace: "new-namespace"}

	got := m.Handle(context.Background(), admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			Object:    raw(newCluster),
			OldObject: raw(oldCluster),
		},
	})
	g.Expect(got.Allowed).To(BeTrue())

	patches := map[string]string{}
	for _, p := range got.Patches {
		patches[p.Path] = p.Operation
	}
	// "/" inside a label key is escaped as "~1" in a JSON pointer.
	g.Expect(patches).To(Equal(map[string]string{
		"/metadata/labels/yandexidentity~1new-namespace": "add",
		"/metadata/labels/yandexidentity~1old-namespace": "remove",
	}))
}
