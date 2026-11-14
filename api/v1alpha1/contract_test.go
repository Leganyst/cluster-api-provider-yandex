package v1alpha1_test

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"

	infrav1 "github.com/yandex-cloud/cluster-api-provider-yandex/api/v1alpha1"
)

// TestContractVersionIsConsistent checks the rule of the Cluster API provider contract:
// the contract version declared in metadata.yaml (read by clusterctl) must be the same as
// the contract version in the cluster.x-k8s.io/<contract> label of the CRDs (read by the
// Cluster API controllers), and the label must point to an API version served by the CRDs.
func TestContractVersionIsConsistent(t *testing.T) {
	g := NewWithT(t)
	root := filepath.Join("..", "..")

	// metadata.yaml: the first release series is the current one.
	metadata := struct {
		ReleaseSeries []struct {
			Major    int    `json:"major"`
			Minor    int    `json:"minor"`
			Contract string `json:"contract"`
		} `json:"releaseSeries"`
	}{}
	raw, err := os.ReadFile(filepath.Join(root, "metadata.yaml"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(yaml.Unmarshal(raw, &metadata)).To(Succeed())
	g.Expect(metadata.ReleaseSeries).NotTo(BeEmpty())
	contract := metadata.ReleaseSeries[0].Contract
	g.Expect(contract).To(Equal("v1beta2"))

	// config/crd/kustomization.yaml: labels applied to every CRD of the provider.
	kustomization := struct {
		Labels []struct {
			Pairs map[string]string `json:"pairs"`
		} `json:"labels"`
	}{}
	raw, err = os.ReadFile(filepath.Join(root, "config", "crd", "kustomization.yaml"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(yaml.Unmarshal(raw, &kustomization)).To(Succeed())

	contractLabels := map[string]string{}
	for _, l := range kustomization.Labels {
		for k, v := range l.Pairs {
			contractLabels[k] = v
		}
	}
	g.Expect(contractLabels).To(Equal(map[string]string{
		"cluster.x-k8s.io/" + contract: infrav1.GroupVersion.Version,
	}))
}
