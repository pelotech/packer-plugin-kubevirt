package k8s

import (
	"fmt"
	"github.com/spf13/pflag"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"kubevirt.io/client-go/kubecli"
	"os"
)

const (
	VirtualMachineResourceName = "virtualmachines"
)

func GetKubevirtClient() (kubecli.KubevirtClient, error) {
	var client kubecli.KubevirtClient
	var err error

	kubeconfig := os.Getenv(clientcmd.RecommendedConfigPathEnvVar)
	if kubeconfig != "" {
		var config *restclient.Config
		config, err = kubecli.DefaultClientConfig(&pflag.FlagSet{}).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create default kube config: %w", err)
		}

		client, err = kubecli.GetKubevirtClientFromRESTConfig(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create kube client: %w", err)
		}

	} else {
		client, err = kubecli.GetKubevirtClientFromFlags("", "")
		if err != nil {
			return nil, fmt.Errorf("failed to create in-cluster kube client: %w", err)
		}
	}

	_, err = client.ServerVersion().Get()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve server version. Please check your authentication credentials: %w", err)
	}

	return client, nil
}

func GetInClusterKubevirtClient() (kubecli.KubevirtClient, error) {
	client, err := kubecli.GetKubevirtClientFromFlags("", "")
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubevirt client: %w", err)
	}

	return client, nil
}
