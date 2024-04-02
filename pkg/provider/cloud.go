package provider

import (
	"errors"
	"fmt"
	virtInstall "github.com/deckhouse/virtualization/api/core/install"
	"io"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	cloudprovider "k8s.io/cloud-provider"
	"os"
	"regexp"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ProviderName is the name of the virtualization provider
	ProviderName = "dvp"
)

var scheme = runtime.NewScheme()

func init() {
	cloudprovider.RegisterCloudProvider(ProviderName, dvpCloudProviderFactory)
	if err := corev1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	virtInstall.Install(scheme)

}

func dvpCloudProviderFactory(config io.Reader) (cloudprovider.Interface, error) {
	cloudConfig, err := NewCloudConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to get cloudConfig: %w", err)
	}
	err = cloudConfig.Validate()
	if err != nil {
		return nil, err
	}
	restConfig, ctxNamespace, err := loadKubeconfig(cloudConfig.KubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}
	if cloudConfig.Namespace == "" {
		cloudConfig.Namespace = ctxNamespace
	}
	c, err := client.New(restConfig, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	return &cloud{
		client: c,
		config: cloudConfig,
	}, nil
}

type cloud struct {
	client client.Client
	config *CloudConfig
}

func (c cloud) Initialize(_ cloudprovider.ControllerClientBuilder, _ <-chan struct{}) {
}

func (c cloud) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	return &loadBalancer{
		namespace:   c.config.Namespace,
		client:      c.client,
		config:      c.config.LoadBalancer,
		infraLabels: c.config.InfraLabels,
	}, true

}

func (c cloud) Instances() (cloudprovider.Instances, bool) {
	return nil, false
}

func (c cloud) InstancesV2() (cloudprovider.InstancesV2, bool) {
	return &instancesV2{
		client:               c.client,
		namespace:            c.config.Namespace,
		ZoneAndRegionEnabled: c.config.ZoneAndRegionEnabled,
	}, true
}

func (c cloud) Zones() (cloudprovider.Zones, bool) {
	return nil, false
}

func (c cloud) Clusters() (cloudprovider.Clusters, bool) {
	return nil, false
}

func (c cloud) Routes() (cloudprovider.Routes, bool) {
	return nil, false
}

func (c cloud) ProviderName() string {
	return ProviderName
}

func (c cloud) HasClusterID() bool {
	return true
}

func loadKubeconfig(filepath string) (conf *rest.Config, ns string, Err error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, "", err
	}
	cc, err := clientcmd.NewClientConfigFromBytes(data)
	if err != nil {
		return nil, "", err
	}
	conf, err = cc.ClientConfig()
	if err != nil {
		Err = errors.Join(Err, err)
	}
	ns, _, err = cc.Namespace()
	if err != nil {
		Err = errors.Join(Err, err)
	}
	return conf, ns, Err
}

func GetProviderID(instanceID string) string {
	return fmt.Sprintf("%s://%s", ProviderName, instanceID)
}

var providerIDRegexp = regexp.MustCompile(`^` + ProviderName + `://([0-9A-Za-z_-]+)$`)

// ParseProviderID extracts the instance ID from a provider ID.
func ParseProviderID(providerID string) (instanceID string, err error) {
	matches := providerIDRegexp.FindStringSubmatch(providerID)
	if len(matches) != 2 {
		return "", fmt.Errorf("ProviderID %q didn't match expected format \"%s://<instance-id>\"", providerID, ProviderName)
	}
	return matches[1], nil
}
