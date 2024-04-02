package provider

import (
	"context"
	"github.com/deckhouse/virtualization/api/core/v1alpha2"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// InstanceGetter allows fetching virtual machine instances with multiple fetching strategies
type InstanceGetter interface {
	// GetByName gets a virtual machine
	GetByName(ctx context.Context, name, namespace string) (*v1alpha2.VirtualMachine, error)
	// GetByProviderID gets a virtual machine
	GetByProviderID(ctx context.Context, providerID, namespace string) (*v1alpha2.VirtualMachine, error)
}

type instanceGetter struct {
	client client.Client
}

func (i instanceGetter) GetByName(ctx context.Context, name, namespace string) (*v1alpha2.VirtualMachine, error) {
	var instance v1alpha2.VirtualMachine

	if err := i.client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &instance); err != nil {
		if errors.IsNotFound(err) {
			return nil, cloudprovider.InstanceNotFound
		}
		return nil, err
	}
	return &instance, nil
}

func (i instanceGetter) GetByProviderID(ctx context.Context, providerID, namespace string) (*v1alpha2.VirtualMachine, error) {
	instanceName, err := ParseProviderID(providerID)
	if err != nil {
		return nil, err
	}
	return i.GetByName(ctx, instanceName, namespace)
}

func NewInstanceGetter(c client.Client) InstanceGetter {
	return &instanceGetter{client: c}
}
