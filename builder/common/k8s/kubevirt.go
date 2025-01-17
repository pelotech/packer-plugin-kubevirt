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
	VirtualMachineExportKind   = "VirtualMachineExport"
)

func GetKubevirtClient() (kubecli.KubevirtClient, error) {
	var client kubecli.KubevirtClient
	var err error

	_, ciEnvExists := os.LookupEnv("CI")
	_, configEnvExists := os.LookupEnv(clientcmd.RecommendedConfigPathEnvVar)
	configFile, err := os.Stat(clientcmd.RecommendedHomeFile)
	if ciEnvExists || configEnvExists || configFile != nil {
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

	version, err := client.Discovery().ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve server version: %w", err)
	} else {
		fmt.Printf("Server version: %s\n", version.String())
	}

	return client, nil
}
