package generator

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"testing"
)

func TestGenerateGuestFSJobIsRetriedOnce(t *testing.T) {
	job := GenerateGuestFSJob(&kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"},
		Spec:       kubevirtv1.VirtualMachineSpec{Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{}},
	})

	// Kubernetes retries a job 6 times when its backoff limit is unset
	if retries := ptr.Deref(job.Spec.BackoffLimit, 6); retries != 1 {
		t.Errorf("expected the job to be retried once, got %d retries", retries)
	}
}
