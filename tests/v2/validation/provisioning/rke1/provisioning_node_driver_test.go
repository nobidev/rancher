package rke1

import (
	"testing"

	"github.com/rancher/rancher/tests/framework/clients/rancher"
	management "github.com/rancher/rancher/tests/framework/clients/rancher/generated/management/v3"
	"github.com/rancher/rancher/tests/framework/extensions/clusters"
	nodepools "github.com/rancher/rancher/tests/framework/extensions/rke1/nodepools"
	"github.com/rancher/rancher/tests/framework/extensions/users"
	password "github.com/rancher/rancher/tests/framework/extensions/users/passwordgenerator"
	"github.com/rancher/rancher/tests/framework/pkg/config"
	namegen "github.com/rancher/rancher/tests/framework/pkg/namegenerator"
	"github.com/rancher/rancher/tests/framework/pkg/session"
	"github.com/rancher/rancher/tests/framework/pkg/wait"
	"github.com/rancher/rancher/tests/integration/pkg/defaults"
	provisioning "github.com/rancher/rancher/tests/v2/validation/provisioning"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RKE1NodeDriverProvisioningTestSuite struct {
	suite.Suite
	client             *rancher.Client
	standardUserClient *rancher.Client
	session            *session.Session
	kubernetesVersions []string
	cnis               []string
	providers          []string
}

func (r *RKE1NodeDriverProvisioningTestSuite) TearDownSuite() {
	r.session.Cleanup()
}

func (r *RKE1NodeDriverProvisioningTestSuite) SetupSuite() {
	testSession := session.NewSession(r.T())
	r.session = testSession

	clustersConfig := new(provisioning.Config)
	config.LoadConfig(provisioning.ConfigurationFileKey, clustersConfig)

	r.kubernetesVersions = clustersConfig.RKE1KubernetesVersions
	r.cnis = clustersConfig.CNIs
	r.providers = clustersConfig.Providers

	client, err := rancher.NewClient("", testSession)
	require.NoError(r.T(), err)

	r.client = client

	enabled := true
	var testuser = namegen.AppendRandomString("testuser-")
	var testpassword = password.GenerateUserPassword("testpass-")
	user := &management.User{
		Username: testuser,
		Password: testpassword,
		Name:     testuser,
		Enabled:  &enabled,
	}

	newUser, err := users.CreateUserWithRole(client, user, "user")
	require.NoError(r.T(), err)

	newUser.Password = user.Password

	standardUserClient, err := client.AsUser(newUser)
	require.NoError(r.T(), err)

	r.standardUserClient = standardUserClient
}

func (r *RKE1NodeDriverProvisioningTestSuite) TestProvisioningRKE1Cluster() {
	nodeRoles0 := []nodepools.NodeRoles{
		{
			ControlPlane: true,
			Etcd:         true,
			Worker:       true,
			Quantity:     1,
		},
	}

	nodeRoles1 := []nodepools.NodeRoles{
		{
			ControlPlane: true,
			Etcd:         false,
			Worker:       false,
			Quantity:     1,
		},
		{
			ControlPlane: false,
			Etcd:         true,
			Worker:       false,
			Quantity:     1,
		},
		{
			ControlPlane: false,
			Etcd:         false,
			Worker:       true,
			Quantity:     1,
		},
	}

	tests := []struct {
		name      string
		nodeRoles []nodepools.NodeRoles
		client    *rancher.Client
	}{
		{"1 Node all roles Admin User", nodeRoles0, r.client},
		{"1 Node all roles Standard User", nodeRoles0, r.standardUserClient},
		{"3 nodes - 1 role per node Admin User", nodeRoles1, r.client},
		{"3 nodes - 1 role per node Standard User", nodeRoles1, r.standardUserClient},
	}

	var name, scaleName string
	for _, tt := range tests {
		subSession := r.session.NewSession()
		defer subSession.Cleanup()

		client, err := tt.client.WithSession(subSession)
		require.NoError(r.T(), err)

		for _, providerName := range r.providers {
			provider := CreateProvider(providerName)
			providerName := " Node Provider: " + provider.Name
			for _, kubeVersion := range r.kubernetesVersions {
				name = tt.name + providerName + " Kubernetes version: " + kubeVersion
				for _, cni := range r.cnis {
					name += " cni: " + cni
					scaleName += name + " scaling"
					r.Run(name, func() {
						r.testProvisioningRKE1Cluster(client, provider, tt.nodeRoles, kubeVersion, cni)
					})

					r.Run(scaleName, func() {
						r.testScalingRKE1NodePools(client, provider, tt.nodeRoles, kubeVersion, cni)
					})
				}
			}
		}
	}
}

func (r *RKE1NodeDriverProvisioningTestSuite) TestProvisioningRKE1ClusterDynamicInput() {
	clustersConfig := new(provisioning.Config)
	config.LoadConfig(provisioning.ConfigurationFileKey, clustersConfig)
	nodesAndRoles := clustersConfig.NodesAndRolesRKE1

	if len(nodesAndRoles) == 0 {
		r.T().Skip()
	}

	tests := []struct {
		name   string
		client *rancher.Client
	}{
		{"Admin User", r.client},
		{"Standard User", r.standardUserClient},
	}

	var name, scaleName string
	for _, tt := range tests {
		subSession := r.session.NewSession()
		defer subSession.Cleanup()

		client, err := tt.client.WithSession(subSession)
		require.NoError(r.T(), err)

		for _, providerName := range r.providers {
			provider := CreateProvider(providerName)
			providerName := " Node Provider: " + provider.Name
			for _, kubeVersion := range r.kubernetesVersions {
				name = tt.name + providerName + " Kubernetes version: " + kubeVersion
				for _, cni := range r.cnis {
					name += " cni: " + cni
					scaleName += name + " scaling"
					r.Run(name, func() {
						r.testProvisioningRKE1Cluster(client, provider, nodesAndRoles, kubeVersion, cni)
					})

					r.Run(scaleName, func() {
						r.testScalingRKE1NodePools(client, provider, nodesAndRoles, kubeVersion, cni)
					})
				}
			}
		}
	}
}

func (r *RKE1NodeDriverProvisioningTestSuite) testProvisioningRKE1Cluster(client *rancher.Client, provider Provider, nodesAndRoles []nodepools.NodeRoles, kubeVersion, cni string) (*management.Cluster, error) {
	clusterName := namegen.AppendRandomString(provider.Name)

	cluster := clusters.NewRKE1ClusterConfig(clusterName, cni, kubeVersion, client)
	clusterResp, err := clusters.CreateRKE1Cluster(client, cluster)
	require.NoError(r.T(), err)

	nodeTemplateResp, err := provider.NodeTemplateFunc(client)
	require.NoError(r.T(), err)

	nodePool, err := nodepools.NodePoolSetup(client, nodesAndRoles, clusterResp.ID, nodeTemplateResp.ID)
	require.NoError(r.T(), err)

	nodePoolName := nodePool.Name

	opts := metav1.ListOptions{
		FieldSelector:  "metadata.name=" + clusterResp.ID,
		TimeoutSeconds: &defaults.WatchTimeoutSeconds,
	}
	watchInterface, err := r.client.GetManagementWatchInterface(management.ClusterType, opts)
	require.NoError(r.T(), err)

	checkFunc := clusters.IsHostedProvisioningClusterReady

	err = wait.WatchWait(watchInterface, checkFunc)
	require.NoError(r.T(), err)
	assert.Equal(r.T(), clusterName, clusterResp.Name)
	assert.Equal(r.T(), nodePoolName, nodePool.Name)

	clusterToken, err := clusters.CheckServiceAccountTokenSecret(client, clusterName)
	require.NoError(r.T(), err)
	assert.NotEmpty(r.T(), clusterToken)

	return clusterResp, nil
}

func (r *RKE1NodeDriverProvisioningTestSuite) testScalingRKE1NodePools(client *rancher.Client, provider Provider, nodesAndRoles []nodepools.NodeRoles, kubeVersion, cni string) {
	nodeTemplateResp, err := provider.NodeTemplateFunc(client)
	require.NoError(r.T(), err)

	clusterResp, err := r.testProvisioningRKE1Cluster(client, provider, nodesAndRoles, kubeVersion, cni)
	require.NoError(r.T(), err)

	err = nodepools.ScaleWorkerNodePool(client, nodesAndRoles, clusterResp.ID, nodeTemplateResp.ID)
	require.NoError(r.T(), err)
}

// In order for 'go test' to run this suite, we need to create
// a normal test function and pass our suite to suite.Run
func TestRKE1ProvisioningTestSuite(t *testing.T) {
	suite.Run(t, new(RKE1NodeDriverProvisioningTestSuite))
}
