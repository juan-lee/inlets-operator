package provision

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/services/containerinstance/mgmt/2018-10-01/containerinstance"
	"github.com/Azure/azure-sdk-for-go/services/resources/mgmt/2019-05-01/resources"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/Azure/go-autorest/autorest/to"
)

// AzureProvisioner provisions a VM on Azure
type AzureProvisioner struct {
	authorizer autorest.Authorizer
}

type azureHost struct {
	BasicHost
	SubscriptionID string
	SSHPublicKey   string
}

// NewAzureProvisioner creates an AzureProvisioner.
func NewAzureProvisioner() (*AzureProvisioner, error) {
	a, err := auth.NewAuthorizerFromFileWithResource(azure.PublicCloud.ResourceManagerEndpoint)
	if err != nil {
		return nil, err
	}

	return &AzureProvisioner{authorizer: a}, nil
}

func newAzureHost(host BasicHost) *azureHost {
	return &azureHost{
		BasicHost:      host,
		SubscriptionID: host.Additional["subscriptionID"],
	}
}

func (p *AzureProvisioner) Status(id string) (*ProvisionedHost, error) {
	subID, rg, name := parseContainerGroupID(id)
	c := p.containerGroups(subID)
	cg, err := c.Get(context.Background(), rg, name)
	if err != nil {
		return nil, err
	}
	if *cg.ProvisioningState != "Succeeded" ||
		*cg.IPAddress.IP == "" {
		return nil, fmt.Errorf("container group not ready [state: %s, ip: %s]", *cg.ProvisioningState, *cg.IPAddress.IP)
	}
	return &ProvisionedHost{ID: *cg.ID, IP: *cg.IPAddress.IP, Status: "active"}, nil
}

func (p *AzureProvisioner) Provision(host BasicHost) (*ProvisionedHost, error) {
	ctx := context.Background()
	azHost := newAzureHost(host)
	err := p.provisionResourceGroup(ctx, azHost)
	if err != nil {
		return nil, err
	}
	ph, err := p.provisionContainerGroup(ctx, azHost)
	if err != nil {
		return nil, err
	}
	return ph, nil
}

func (p *AzureProvisioner) groups(subID string) resources.GroupsClient {
	c := resources.NewGroupsClient(subID)
	c.Authorizer = p.authorizer
	return c
}

func (p *AzureProvisioner) containerGroups(subID string) containerinstance.ContainerGroupsClient {
	c := containerinstance.NewContainerGroupsClient(subID)
	c.Authorizer = p.authorizer
	return c
}

func (p *AzureProvisioner) provisionResourceGroup(ctx context.Context, host *azureHost) error {
	c := p.groups(host.SubscriptionID)
	group, err := c.Get(ctx, host.Name)
	if err != nil && !notFound(err) {
		return fmt.Errorf("failed to get resourceGroup [%w]", err)
	}

	clearState(&group)
	group.Location = &host.Region
	group, err = c.CreateOrUpdate(ctx, host.Name, group)
	if err != nil {
		return fmt.Errorf("failed to update resourceGroup [%w]", err)
	}
	return nil
}

func (p *AzureProvisioner) provisionContainerGroup(ctx context.Context, host *azureHost) (*ProvisionedHost, error) {
	c := p.containerGroups(host.SubscriptionID)
	cg := newContainerGroup(host)
	future, err := c.CreateOrUpdate(ctx, host.Name, host.Name, cg)
	if err != nil {
		return nil, fmt.Errorf("failed to update virtual machine [%w]", err)
	}
	err = future.WaitForCompletionRef(ctx, c.Client)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for update virtual machine [%w]", err)
	}
	cg, err = future.Result(c)
	if err != nil {
		return nil, fmt.Errorf("failed to get update virtual machine result [%w]", err)
	}
	return &ProvisionedHost{ID: *cg.ID}, nil
}

func newContainerGroup(host *azureHost) containerinstance.ContainerGroup {
	return containerinstance.ContainerGroup{
		Location: &host.Region,
		ContainerGroupProperties: &containerinstance.ContainerGroupProperties{
			IPAddress: &containerinstance.IPAddress{
				Type: containerinstance.Public,
				Ports: &[]containerinstance.Port{
					{
						Port:     to.Int32Ptr(80),
						Protocol: containerinstance.TCP,
					},
					{
						Port:     to.Int32Ptr(8000),
						Protocol: containerinstance.TCP,
					},
				},
			},
			OsType: containerinstance.Linux,
			Containers: &[]containerinstance.Container{
				{
					Name: to.StringPtr("inlets"),
					ContainerProperties: &containerinstance.ContainerProperties{
						Ports: &[]containerinstance.ContainerPort{
							{
								Port:     to.Int32Ptr(80),
								Protocol: containerinstance.ContainerNetworkProtocolTCP,
							},
							{
								Port:     to.Int32Ptr(8000),
								Protocol: containerinstance.ContainerNetworkProtocolTCP,
							},
						},
						Image:   to.StringPtr("jpangms/inlets:2.4.1"),
						Command: &[]string{"inlets", "server", "--port=80", "--control-port=8000", fmt.Sprintf("--token=%s", host.Token)},
						EnvironmentVariables: &[]containerinstance.EnvironmentVariable{
							{
								Name:        to.StringPtr("INLETSTOKEN"),
								SecureValue: &host.Token,
							},
						},
						Resources: &containerinstance.ResourceRequirements{
							Limits: &containerinstance.ResourceLimits{
								MemoryInGB: to.Float64Ptr(.5),
								CPU:        to.Float64Ptr(1),
							},
							Requests: &containerinstance.ResourceRequests{
								MemoryInGB: to.Float64Ptr(.5),
								CPU:        to.Float64Ptr(1),
							},
						},
					},
				},
			},
		},
	}
}

func notFound(e error) bool {
	if err, ok := e.(autorest.DetailedError); ok && err.StatusCode == 404 {
		return true
	}
	return false
}

func clearState(group *resources.Group) {
	if group.Properties != nil {
		group.Properties.ProvisioningState = nil
	}
}

func parseContainerGroupID(id string) (string, string, string) {
	s := strings.Split(id, "/")
	return s[2], s[4], s[8]
}
