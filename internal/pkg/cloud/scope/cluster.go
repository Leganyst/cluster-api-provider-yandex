package scope

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"

	"github.com/pkg/errors"
	infrav1 "github.com/yandex-cloud/cluster-api-provider-yandex/api/v1alpha1"
	yandex "github.com/yandex-cloud/cluster-api-provider-yandex/internal/pkg/client"

	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var nonAlphanumericRegex = regexp.MustCompile(`[^a-z0-9-]+`)

const (
	maxNameLength = 64
)

// ClusterScopeParams defines the input parameters used to create a new Scope.
type ClusterScopeParams struct {
	Client             client.Client
	Cluster            *clusterv1.Cluster
	YandexClientGetter yandex.YandexClientGetter
	YandexCluster      *infrav1.YandexCluster
}

// NewClusterScope creates a new Scope from the supplied parameters.
// This is meant to be called for each reconcile iteration.
func NewClusterScope(ctx context.Context, params ClusterScopeParams) (*ClusterScope, error) {
	if params.Client == nil {
		return nil, errors.New("failed to generate new scope from nil Client")
	}
	if params.Cluster == nil {
		return nil, errors.New("failed to generate new scope from nil Cluster")
	}
	if params.YandexCluster == nil {
		return nil, errors.New("failed to generate new scope from nil YandexCluster")
	}
	if params.YandexClientGetter == nil {
		return nil, errors.New("failed to generate new scope from nil ClientBuilder")
	}

	// Get Yandex Client for cluster
	yandexClient, err := getYandexClient(ctx, params)
	if err != nil {
		return nil, err
	}

	helper, err := patch.NewHelper(params.YandexCluster, params.Client)
	if err != nil {
		return nil, errors.Wrap(err, "failed to init patch helper")
	}

	return &ClusterScope{
		client:        params.Client,
		Cluster:       params.Cluster,
		YandexCluster: params.YandexCluster,
		patchHelper:   helper,
		yandexClient:  yandexClient,
	}, nil
}

// ClusterScope defines the basic context for an actuator to operate upon.
type ClusterScope struct {
	client      client.Client
	patchHelper *patch.Helper

	Cluster       *clusterv1.Cluster
	YandexCluster *infrav1.YandexCluster
	yandexClient  yandex.Client
}

// PatchObject persists the cluster configuration and status.
func (c *ClusterScope) PatchObject(ctx context.Context) error {
	// Always update the Ready condition by summarizing the state of the other conditions.
	if err := conditions.SetSummaryCondition(c.YandexCluster, c.YandexCluster, infrav1.ReadyCondition,
		conditions.ForConditionTypes{infrav1.LoadBalancerReadyCondition},
		conditions.CustomMergeStrategy{
			MergeStrategy: conditions.DefaultMergeStrategy(
				conditions.ComputeReasonFunc(conditions.GetDefaultComputeMergeReasonFunc(
					infrav1.NotReadyReason,
					infrav1.ReadyUnknownReason,
					infrav1.ReadyReason,
				)),
			),
		},
	); err != nil {
		return errors.Wrapf(err, "failed to set %s condition", infrav1.ReadyCondition)
	}

	// Patch the object, ignoring conflicts on the conditions owned by this controller.
	return c.patchHelper.Patch(ctx, c.YandexCluster, patch.WithOwnedConditions{Conditions: []string{
		infrav1.ReadyCondition,
		infrav1.LoadBalancerReadyCondition,
	}})
}

// Close closes the current scope persisting the cluster configuration and status.
func (c *ClusterScope) Close(ctx context.Context) error {
	// first path the object
	if err := c.PatchObject(ctx); err != nil {
		return err
	}

	// close the client, since we've build it inside the scope
	return c.yandexClient.Close(ctx)
}

// Name returns the CAPI cluster name.
func (c *ClusterScope) Name() string {
	return c.Cluster.GetName()
}

// SetProvisioned reports that the initial provisioning of the YandexCluster infrastructure is completed
// (status.initialization.provisioned, v1beta2 contract).
func (c *ClusterScope) SetProvisioned() {
	c.YandexCluster.Status.Initialization.Provisioned = ptr.To(true)
}

// GetClient gets client for YandexCloud API.
func (c *ClusterScope) GetClient() yandex.Client {
	return c.yandexClient
}

// GetLBType gets type of kubernetes API load balancer.
func (c *ClusterScope) GetLBType() infrav1.LoadBalancerType {
	return c.YandexCluster.Spec.LoadBalancer.Type
}

// GetFolderID gets the cluster Folder ID.
func (c *ClusterScope) GetFolderID() string {
	return c.YandexCluster.Spec.FolderID
}

// GetNetworkID gets the Yandex Network ID.
func (c *ClusterScope) GetNetworkID() string {
	return c.YandexCluster.Spec.NetworkSpec.ID
}

// GetLabels gets the set of cluster tags.
func (c *ClusterScope) GetLabels() infrav1.Labels {
	return c.YandexCluster.Spec.Labels
}

// ControlPlaneEndpoint gets the cluster API endpoit.
func (c *ClusterScope) ControlPlaneEndpoint() infrav1.APIEndpoint {
	return c.YandexCluster.Spec.ControlPlaneEndpoint
}

// GetLBName returns the load balancer name.
func (c *ClusterScope) GetLBName() string {
	if c.YandexCluster.Spec.LoadBalancer.Name == "" {
		return c.generateName()
	}

	return c.YandexCluster.Spec.LoadBalancer.Name
}

// GetLBSpec returns the load balancer specification.
func (c *ClusterScope) GetLBSpec() infrav1.LoadBalancerSpec {
	return c.YandexCluster.Spec.LoadBalancer
}

// generateName generates a resource name via:
// 1. concatenating the cluster name to the suffix provided
// 2. computing a hash for name, if resulting name length greater than
// maxNameLength characters.
func (c *ClusterScope) generateName() string {
	prefix := c.clearString(c.Name())
	name := fmt.Sprintf("%s-api", prefix)

	if len(name) < maxNameLength {
		return name
	}

	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(c.Name())))
	return fmt.Sprintf("cluster-%s-api", hash[:16])
}

// clearString remove all non alphnumeric characters from input.
func (c *ClusterScope) clearString(str string) string {
	return nonAlphanumericRegex.ReplaceAllString(str, "")
}

// AppendLabels appends labels to the cluster.
func (c *ClusterScope) AppendLabels(l map[string]string) {
	if c.Cluster.Labels == nil {
		c.Cluster.Labels = make(map[string]string)
	}

	for k, v := range l {
		c.Cluster.Labels[k] = v
	}
}

// getYandexClient returns Yandex Cloud client.
func getYandexClient(ctx context.Context, params ClusterScopeParams) (yandex.Client, error) {
	if params.YandexCluster.Spec.IdentityRef != nil {
		identity := &infrav1.YandexIdentity{}
		if err := params.Client.Get(ctx, params.YandexCluster.Spec.IdentityRef.NamespacedName(), identity); err != nil {
			return nil, errors.Wrapf(err, "failed to get identity %s/%s",
				params.YandexCluster.Spec.IdentityRef.Namespace, params.YandexCluster.Spec.IdentityRef.Name)
		}

		// A cluster which references an identity must be managed with the key of that identity only.
		// Falling back to the default key here would silently create or delete cloud resources on
		// behalf of another service account, and it would hide the real problem with the identity.
		yc, err := params.YandexClientGetter.GetFromSecret(ctx,
			params.Client, identity.Spec.SecretName, params.YandexCluster.Spec.IdentityRef.Namespace, identity.Spec.KeyName)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to build YandexCloud client from identity %s/%s",
				params.YandexCluster.Spec.IdentityRef.Namespace, params.YandexCluster.Spec.IdentityRef.Name)
		}

		return yc, nil
	}

	// Use the default client if no identity is provided
	yc, err := params.YandexClientGetter.GetDefault(ctx)
	if err != nil {
		return nil, err
	}

	return yc, nil
}
