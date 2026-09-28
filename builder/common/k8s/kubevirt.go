package k8s

import (
	"fmt"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"kubevirt.io/client-go/containerizeddataimporter"
	"kubevirt.io/client-go/kubevirt"
)

const (
	VirtualMachineExportKind = "VirtualMachineExport"
)

type Clients struct {
	Kubernetes kubernetes.Interface
	Kubevirt   kubevirt.Interface
	CDI        containerizeddataimporter.Interface
	RestConfig *restclient.Config
}

func GetKubevirtClient() (*Clients, error) {
	// the kube config of the environment, or the service account of the pod when there is none
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load the kube config: %w", err)
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
