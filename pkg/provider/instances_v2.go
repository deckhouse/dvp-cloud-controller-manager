package provider

import (
	"context"
	"errors"
	"github.com/deckhouse/virtualization/api/core/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	cloudprovider "k8s.io/cloud-provider"
	nodehelper "k8s.io/cloud-provider/node/helpers"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type instancesV2 struct {
	client               client.Client
	namespace            string
	ZoneAndRegionEnabled bool
}

func (i instancesV2) InstanceExists(ctx context.Context, node *corev1.Node) (bool, error) {
	_, err := NewInstanceGetter(i.client).GetByProviderID(ctx, node.Spec.ProviderID, i.namespace)
	if err != nil {
		if errors.Is(err, cloudprovider.InstanceNotFound) {
			return false, nil
		}
		klog.Errorf("Failed to get instance. providerID=%q, namespace=%q: %v", node.Spec.ProviderID, i.namespace, err)
		return false, err
	}
	return true, nil
}

func (i instancesV2) InstanceShutdown(ctx context.Context, node *corev1.Node) (bool, error) {
	instance, err := NewInstanceGetter(i.client).GetByProviderID(ctx, node.Spec.ProviderID, i.namespace)
	if err != nil {
		klog.Errorf("Failed to get instance. providerID=%q, namespace=%q: %v", node.Spec.ProviderID, i.namespace, err)
		return false, err
	}
	return instance.Status.Phase == v1alpha2.MachineStopped, nil
}

func (i instancesV2) InstanceMetadata(ctx context.Context, node *corev1.Node) (*cloudprovider.InstanceMetadata, error) {
	instance, err := NewInstanceGetter(i.client).GetByProviderID(ctx, node.Spec.ProviderID, i.namespace)
	if err != nil {
		klog.Errorf("Failed to get instance. providerID=%q, namespace=%q: %v", node.Spec.ProviderID, i.namespace, err)
		return nil, err
	}
	addrs := i.getNodeAddresses(instance.Status, node.Status.Addresses)

	region, zone, err := i.getRegionAndZone(ctx, instance.Status.NodeName)
	if err != nil {
		return nil, err
	}
	return &cloudprovider.InstanceMetadata{
		ProviderID:    GetProviderID(instance.Name),
		NodeAddresses: addrs,
		Region:        region,
		Zone:          zone,
	}, nil
}

func (i *instancesV2) getNodeAddresses(status v1alpha2.VirtualMachineStatus, prevAddrs []corev1.NodeAddress) []corev1.NodeAddress {
	var addrs []corev1.NodeAddress
	foundInternalIP := false

	if status.IPAddress != "" {
		nodehelper.AddToNodeAddresses(&addrs, corev1.NodeAddress{
			Type:    corev1.NodeInternalIP,
			Address: status.IPAddress,
		})
		foundInternalIP = true
	}
	if !foundInternalIP {
		for _, prevAddr := range prevAddrs {
			if prevAddr.Type == corev1.NodeInternalIP {
				nodehelper.AddToNodeAddresses(&addrs, prevAddr)
			}
		}
	}
	return addrs
}

func (i *instancesV2) getRegionAndZone(ctx context.Context, nodeName string) (string, string, error) {
	var region, zone string
	if !i.ZoneAndRegionEnabled {
		return region, zone, nil
	}

	node := corev1.Node{}

	err := i.client.Get(ctx, client.ObjectKey{Name: nodeName}, &node)
	if err != nil {
		return region, zone, err
	}

	if val, ok := node.Labels[corev1.LabelTopologyRegion]; ok {
		region = val
	}
	if val, ok := node.Labels[corev1.LabelTopologyZone]; ok {
		zone = val
	}

	return region, zone, nil
}
