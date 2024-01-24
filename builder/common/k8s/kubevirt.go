package k8s

import (
	"fmt"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/homedir"
	kubevirtv1 "kubevirt.io/api/core/v1"
	exportv1 "kubevirt.io/api/export/v1alpha1"
	"kubevirt.io/client-go/kubecli"
	"os"
	"path/filepath"
)

var VirtualMachineGroupVersionResource = schema.GroupVersionResource{
	Group:    kubevirtv1.VirtualMachineGroupVersionKind.Group,
	Version:  kubevirtv1.VirtualMachineGroupVersionKind.Version,
	Resource: "VirtualMachines",
}

var VirtualMachineGroupVersionKind = schema.GroupVersionKind{
	Group:   kubevirtv1.VirtualMachineGroupVersionKind.Group,
	Version: kubevirtv1.VirtualMachineGroupVersionKind.Version,
	Kind:    "VirtualMachine",
}

var VirtualMachineExportGroupVersionResource = schema.GroupVersionKind{
	Group:   exportv1.SchemeGroupVersion.Group,
	Version: exportv1.SchemeGroupVersion.Version,
	Kind:    "VirtualMachineExport",
}

func GetKubevirtClient() (kubecli.KubevirtClient, error) {
	kubeconfig := os.Getenv("KUBECONFIG")

	if kubeconfig == "" {
		if home := homedir.HomeDir(); home != "" {
			kubeconfig = filepath.Join(home, ".kube", "config")
		}
	}

	client, err := kubecli.GetKubevirtClientFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubevirt client: %w", err)
	}

	return client, nil
}
