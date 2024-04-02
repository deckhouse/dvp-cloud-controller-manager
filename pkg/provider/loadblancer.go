package provider

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

const (
	// Default interval between polling the service after creation
	defaultLoadBalancerCreatePollInterval = 5 * time.Second

	// Default timeout between polling the service after creation
	defaultLoadBalancerCreatePollTimeout = 5 * time.Minute
)

type loadBalancer struct {
	namespace   string
	client      client.Client
	config      LoadBalancerConfig
	infraLabels map[string]string
}

func (lb loadBalancer) GetLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service) (status *corev1.LoadBalancerStatus, exists bool, err error) {
	name := lb.GetLoadBalancerName(ctx, clusterName, service)
	svc, err := lb.getLoadBalancerByName(ctx, name)
	if err != nil {
		klog.Errorf("Failed to get LoadBalancer service %q in namespace %q: %v", name, lb.namespace, err)
		return nil, false, err
	}
	if svc == nil {
		return nil, false, nil
	}
	status = &svc.Status.LoadBalancer
	return status, true, nil
}

func (lb loadBalancer) GetLoadBalancerName(_ context.Context, _ string, service *corev1.Service) string {
	// TODO: replace DefaultLoadBalancerName to generate more meaningful loadbalancer names.
	return cloudprovider.DefaultLoadBalancerName(service)
}

func (lb loadBalancer) EnsureLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) (*corev1.LoadBalancerStatus, error) {
	name := lb.GetLoadBalancerName(ctx, clusterName, service)
	svc, err := lb.getLoadBalancerByName(ctx, name)
	if err != nil {
		klog.Errorf("Failed to get LoadBalancer service %q in namespace %q: %v", name, lb.namespace, err)
		return nil, err
	}
	ports := lb.createLoadBalancerPorts(service)

	if svc != nil {
		return &svc.Status.LoadBalancer, lb.updateLoadBalancerPorts(ctx, svc, ports)
	}

	// TODO: fix labels.
	vmLabels := map[string]string{
		"cluster.x-k8s.io/cluster-name": clusterName,
	}
	// TODO: fix labels.
	svcLabels := map[string]string{
		"cluster.x-k8s.io/tenant-service-name":      service.Name,
		"cluster.x-k8s.io/tenant-service-namespace": service.Namespace,
		"cluster.x-k8s.io/cluster-name":             clusterName,
	}

	for k, v := range lb.infraLabels {
		svcLabels[k] = v
	}

	svc, err = lb.createLoadBalancer(ctx, name, service, vmLabels, svcLabels, ports)
	if err != nil {
		klog.Errorf("Failed to create LoadBalancer service %q in namespace %q: %v", name, lb.namespace, err)
		return nil, err
	}

	err = wait.PollUntilContextTimeout(ctx,
		time.Duration(lb.config.CreationPollInterval)*time.Second,
		time.Duration(lb.config.CreationPollTimeout)*time.Second,
		true,
		func(context.Context) (done bool, err error) {
			if len(svc.Status.LoadBalancer.Ingress) > 0 {
				return true, nil
			}
			s, err := lb.getLoadBalancerByName(ctx, name)
			if err != nil {
				klog.Errorf("Failed to get LoadBalancer service %q in namespace %q: %v", name, lb.namespace, err)
				return false, err
			}
			if s != nil && len(s.Status.LoadBalancer.Ingress) > 0 {
				svc = s
				return true, nil
			}
			return false, nil
		})
	if err != nil {
		klog.Errorf("Failed to poll LoadBalancer service %q in namespace %q: %v", name, lb.namespace, err)
		return nil, err
	}

	return &svc.Status.LoadBalancer, nil
}

func (lb loadBalancer) UpdateLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, _ []*corev1.Node) error {
	name := lb.GetLoadBalancerName(ctx, clusterName, service)

	var svc corev1.Service
	if err := lb.client.Get(ctx, client.ObjectKey{Name: name, Namespace: lb.namespace}, &svc); err != nil {
		klog.Errorf("Failed to get LoadBalancer service %q in namespace %q: %v", name, lb.namespace, err)
		return err
	}

	ports := lb.createLoadBalancerPorts(service)
	// LoadBalancer already exist, update the ports if changed
	return lb.updateLoadBalancerPorts(ctx, &svc, ports)

}

func (lb loadBalancer) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *corev1.Service) error {
	name := lb.GetLoadBalancerName(ctx, clusterName, service)

	svc, err := lb.getLoadBalancerByName(ctx, name)
	if err != nil {
		klog.Errorf("Failed to get LoadBalancer service %q in namespace %q: %v", name, lb.namespace, err)
		return err
	}
	if svc != nil {
		if err = lb.client.Delete(ctx, svc); err != nil {
			klog.Errorf("Failed to delete LoadBalancer service %q in namespace %q: %v", service.GetName(), lb.namespace, err)
			return err
		}
	}
	return nil

}

func (lb loadBalancer) getLoadBalancerByName(ctx context.Context, name string) (*corev1.Service, error) {
	var svc corev1.Service
	if err := lb.client.Get(ctx, types.NamespacedName{Name: name, Namespace: lb.namespace}, &svc); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &svc, nil
}

func (lb *loadBalancer) createLoadBalancerPorts(service *corev1.Service) []corev1.ServicePort {
	ports := make([]corev1.ServicePort, len(service.Spec.Ports))
	for i, port := range service.Spec.Ports {
		ports[i].Name = port.Name
		ports[i].Protocol = port.Protocol
		ports[i].Port = port.Port
		ports[i].TargetPort = intstr.IntOrString{
			Type:   intstr.Int,
			IntVal: port.NodePort,
		}
	}
	return ports
}

func (lb *loadBalancer) updateLoadBalancerPorts(ctx context.Context, service *corev1.Service, ports []corev1.ServicePort) error {
	if !equality.Semantic.DeepEqual(ports, service.Spec.Ports) {
		service.Spec.Ports = ports
		if err := lb.client.Update(ctx, service); err != nil {
			klog.Errorf("Failed to update LoadBalancer service %q in namespace %q: %v", service.GetName(), lb.namespace, err)
			return err
		}
		return nil
	}
	return nil
}

func (lb *loadBalancer) createLoadBalancer(ctx context.Context, name string, service *corev1.Service, vmLabels map[string]string, serviceLabels map[string]string, ports []corev1.ServicePort) (*corev1.Service, error) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   lb.namespace,
			Annotations: service.Annotations,
			Labels:      serviceLabels,
		},
		Spec: corev1.ServiceSpec{
			Ports:                 ports,
			Type:                  corev1.ServiceTypeLoadBalancer,
			ExternalTrafficPolicy: service.Spec.ExternalTrafficPolicy,
		},
	}
	if !lb.config.SelectorLess {
		svc.Spec.Selector = vmLabels
	}
	if len(service.Spec.ExternalIPs) > 0 {
		svc.Spec.ExternalIPs = service.Spec.ExternalIPs
	}
	if service.Spec.LoadBalancerClass != nil {
		svc.Spec.LoadBalancerClass = ptr.To(*service.Spec.LoadBalancerClass)
	}
	if service.Spec.LoadBalancerIP != "" {
		svc.Spec.LoadBalancerIP = service.Spec.LoadBalancerIP
	}
	if service.Spec.HealthCheckNodePort > 0 {
		svc.Spec.HealthCheckNodePort = service.Spec.HealthCheckNodePort
	}
	return svc, lb.client.Create(ctx, svc)
}
