package provider

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"k8s.io/apimachinery/pkg/util/yaml"
	"os"
)

const (
	envKubeconfigPath string = "DVP_CCM_KUBECONFIG_PATH"
	envNamespace      string = "DVP_CCM_NAMESPACE"
)

var ErrConfigInvalid = errors.New("configuration is invalid")

type CloudConfig struct {
	KubeconfigPath       string             `yaml:"kubeconfigPath"`
	LoadBalancer         LoadBalancerConfig `yaml:"loadBalancer"`
	Namespace            string             `yaml:"namespace"`
	ZoneAndRegionEnabled bool               `yaml:"zoneAndRegionEnabled"`
	InfraLabels          map[string]string  `yaml:"infraLabels"`
}

type LoadBalancerConfig struct {
	// CreationPollInterval determines how many seconds to wait for the load balancer creation between retries
	CreationPollInterval int `yaml:"creationPollInterval,omitempty"`

	// CreationPollTimeout determines how many seconds to wait for the load balancer creation
	CreationPollTimeout int `yaml:"creationPollTimeout,omitempty"`

	// SelectorLess delegate endpointslices creation on third party by
	// skipping service selector creation
	SelectorLess bool `yaml:"selectorLess,omitempty"`
}

func (cc *CloudConfig) Validate() error {
	if cc.KubeconfigPath == "" {
		return fmt.Errorf("kubeconfig not found: %w", ErrConfigInvalid)
	}
	return nil
}

func (cc *CloudConfig) setEnv() {
	if e, found := os.LookupEnv(envKubeconfigPath); found {
		cc.KubeconfigPath = e
	}
	if e, found := os.LookupEnv(envNamespace); found {
		cc.Namespace = e
	}
}

func NewCloudConfig(reader io.Reader) (*CloudConfig, error) {
	cloudConf := defaultCloudConfig()
	if reader != nil {
		buf := new(bytes.Buffer)
		_, err := buf.ReadFrom(reader)
		if err != nil {
			return nil, fmt.Errorf("failed to read cloud provider config: %w", err)
		}
		if err := yaml.Unmarshal(buf.Bytes(), &cloudConf); err != nil {
			return nil, err
		}
	}
	cloudConf.setEnv()
	return &cloudConf, nil
}

func defaultCloudConfig() CloudConfig {
	return CloudConfig{
		LoadBalancer: LoadBalancerConfig{
			CreationPollInterval: int(defaultLoadBalancerCreatePollInterval.Seconds()),
			CreationPollTimeout:  int(defaultLoadBalancerCreatePollTimeout.Seconds()),
		},
		ZoneAndRegionEnabled: true,
	}
}
