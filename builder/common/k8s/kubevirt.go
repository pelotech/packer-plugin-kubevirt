package k8s

import (
	"fmt"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"kubevirt.io/client-go/containerizeddataimporter"
	"kubevirt.io/client-go/kubevirt"
	"os"
)

const (
	VirtualMachineResourceName = "virtualmachines"
	VirtualMachineExportKind   = "VirtualMachineExport"
)

type Clients struct {
	Kubernetes kubernetes.Interface
	Kubevirt   kubevirt.Interface
	CDI        containerizeddataimporter.Interface
	RestConfig *restclient.Config
}

func GetKubevirtClient() (*Clients, error) {
	var config *restclient.Config

	_, ciEnvExists := os.LookupEnv("CI")
	_, configEnvExists := os.LookupEnv(clientcmd.RecommendedConfigPathEnvVar)
	configFile, err := os.Stat(clientcmd.RecommendedHomeFile)
	if ciEnvExists || configEnvExists || configFile != nil {
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		loadingRules.DefaultClientConfig = &clientcmd.DefaultClientConfig
		overrides := &clientcmd.ConfigOverrides{ClusterDefaults: clientcmd.ClusterDefaults}
		config, err = clientcmd.NewInteractiveDeferredLoadingClientConfig(loadingRules, overrides, os.Stdin).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create default kube config: %w", err)
		}
	} else {
		config, err = restclient.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create in-cluster kube client: %w", err)
		}
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kube client: %w", err)
	}

	kubevirtClient, err := kubevirt.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubevirt client: %w", err)
	}

	cdiClient, err := containerizeddataimporter.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create CDI client: %w", err)
	}

	version, err := kubeClient.Discovery().ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve server version: %w", err)
	}
	fmt.Printf("Server version: %s\n", version.String())

	return &Clients{Kubernetes: kubeClient, Kubevirt: kubevirtClient, CDI: cdiClient, RestConfig: config}, nil
}
