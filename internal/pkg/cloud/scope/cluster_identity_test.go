package scope_test

import (
	"context"
	"errors"
	"testing"

	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrav1 "github.com/yandex-cloud/cluster-api-provider-yandex/api/v1alpha1"
	"github.com/yandex-cloud/cluster-api-provider-yandex/internal/pkg/client/mock_client"
	"github.com/yandex-cloud/cluster-api-provider-yandex/internal/pkg/cloud/scope"
)

// TestNewClusterScope_IdentityIsNotReplacedByDefaultKey checks that a cluster which references a
// YandexIdentity is never served with the default service account key: if the client can not be
// built from the identity, the scope creation fails instead of silently using other credentials.
func TestNewClusterScope_IdentityIsNotReplacedByDefaultKey(t *testing.T) {
	g := NewWithT(t)

	scheme := runtime.NewScheme()
	g.Expect(infrav1.AddToScheme(scheme)).To(Succeed())

	identity := &infrav1.YandexIdentity{
		ObjectMeta: metav1.ObjectMeta{Namespace: "capy-system", Name: "identity"},
		Spec:       infrav1.YandexIdentitySpec{SecretName: "secret", KeyName: "key"},
	}
	yandexCluster := &infrav1.YandexCluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "cluster"},
		Spec: infrav1.YandexClusterSpec{
			IdentityRef: &infrav1.IdentityReference{Namespace: "capy-system", Name: "identity"},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(identity, yandexCluster).Build()

	ctrl := gomock.NewController(t)
	getter := mock_client.NewMockYandexClientGetter(ctrl)
	// GetDefault is not expected: the strict mock fails the test if it is called.
	getter.EXPECT().
		GetFromSecret(gomock.Any(), gomock.Any(), "secret", "capy-system", "key").
		Return(nil, errors.New("secret is broken"))

	clusterScope, err := scope.NewClusterScope(context.Background(), scope.ClusterScopeParams{
		Client:             k8sClient,
		Cluster:            &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "cluster"}},
		YandexCluster:      yandexCluster,
		YandexClientGetter: getter,
	})
	g.Expect(err).To(MatchError(ContainSubstring("secret is broken")))
	g.Expect(err).To(MatchError(ContainSubstring("capy-system/identity")))
	g.Expect(clusterScope).To(BeNil())
}

// TestIdentityScope_RemoveSecretFinalizerWithoutSecret checks that an identity whose secret does not
// exist can be deleted: there is no finalizer to remove from a missing secret.
func TestIdentityScope_RemoveSecretFinalizerWithoutSecret(t *testing.T) {
	g := NewWithT(t)

	scheme := runtime.NewScheme()
	g.Expect(infrav1.AddToScheme(scheme)).To(Succeed())
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())

	identity := &infrav1.YandexIdentity{
		ObjectMeta: metav1.ObjectMeta{Namespace: "capy-system", Name: "identity"},
		Spec:       infrav1.YandexIdentitySpec{SecretName: "missing-secret", KeyName: "key"},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(identity).Build()

	identityScope, err := scope.NewIdentityScope(scope.IdentityScopeParams{
		Client:             k8sClient,
		YandexClientGetter: mock_client.NewMockYandexClientGetter(gomock.NewController(t)),
		YandexIdentity:     identity,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(identityScope.RemoveSecretFinalizer(context.Background())).To(Succeed())
}
