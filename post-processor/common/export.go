package common

import (
	"context"
	"fmt"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	exportv1 "kubevirt.io/api/export/v1"
	buildercommon "packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"time"
)

// GetExport reads the export named by the artifact of the builder, and the token of its server
func GetExport(clients *k8s.Clients, source packersdk.Artifact) (*exportv1.VirtualMachineExport, string, error) {
	namespace, _ := source.State(buildercommon.NamespaceArtifactKey).(string)
	name, _ := source.State(buildercommon.VirtualMachineExportNameArtifactKey).(string)
	token, _ := source.State(buildercommon.VirtualMachineExportTokenArtifactKey).(string)
	if namespace == "" || name == "" || token == "" {
		return nil, "", fmt.Errorf("the artifact has no Virtual Machine Export, it must come from the kubevirt builder")
	}

	export, err := clients.Kubevirt.ExportV1().VirtualMachineExports(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, "", fmt.Errorf("failed to get Virtual Machine Export: %w", err)
	}
	return export, token, nil
}

func DeleteOrKeepExport(clients *k8s.Clients, ui packersdk.Ui, namespace, name string, keep bool) {
	if keep {
		ui.Message(fmt.Sprintf("Virtual Machine Export %s/%s has been kept", namespace, name))
		return
	}

	err := clients.Kubevirt.ExportV1().VirtualMachineExports(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err == nil {
		ui.Message(fmt.Sprintf("Virtual Machine Export %s/%s has been deleted", namespace, name))
	} else {
		ui.Error(fmt.Sprintf("failed to delete Virtual Machine Export %s/%s: %v", namespace, name, err))
	}
}

// RunUploadJob creates an uploader job, then its secret owned by the job, and waits for the job to complete
func RunUploadJob(clients *k8s.Clients, ui packersdk.Ui, label string, job *batchv1.Job, generateSecret func(*batchv1.Job) *corev1.Secret, timeout time.Duration) error {
	job, err := clients.Kubernetes.BatchV1().Jobs(job.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to deploy %s job: %w", label, err)
	}

	_, err = clients.Kubernetes.CoreV1().Secrets(job.Namespace).Create(context.TODO(), generateSecret(job), metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create %s secret: %w", label, err)
	}

	err = k8s.WaitForJobCompletion(clients.Kubernetes, ui, job, timeout)
	if err != nil {
		return fmt.Errorf("error with '%s' job: %w", label, err)
	}
	return nil
}
