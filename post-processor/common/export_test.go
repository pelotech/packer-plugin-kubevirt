package common

import (
	"context"
	"errors"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	kubevirtfake "kubevirt.io/client-go/kubevirt/fake"
	"packer-plugin-kubevirt/builder/common/k8s"
	"testing"
	"time"
)

func TestDeleteOrKeepExport(t *testing.T) {
	for name, test := range map[string]struct {
		keep          bool
		expectedCount int
	}{
		"deleted by default": {keep: false, expectedCount: 0},
		"kept when asked":    {keep: true, expectedCount: 1},
	} {
		t.Run(name, func(t *testing.T) {
			export := newExport()
			clients := &k8s.Clients{Kubevirt: kubevirtfake.NewSimpleClientset(export)}

			DeleteOrKeepExport(clients, packersdk.TestUi(t), export.Namespace, export.Name, test.keep)

			exports, err := clients.Kubevirt.ExportV1().VirtualMachineExports(export.Namespace).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				t.Fatalf("failed to list Virtual Machine Exports: %v", err)
			}
			if len(exports.Items) != test.expectedCount {
				t.Errorf("expected %d Virtual Machine Export, got: %d", test.expectedCount, len(exports.Items))
			}
		})
	}
}

func TestRunUploadJobStopsWaitingWhenCancelled(t *testing.T) {
	clients := &k8s.Clients{Kubernetes: k8sfake.NewSimpleClientset()}
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu-s3-uploader", Namespace: "packer"}}
	generateSecret := func(job *batchv1.Job) *corev1.Secret {
		return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: job.Name, Namespace: job.Namespace}}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(100*time.Millisecond, cancel)

	// the job never completes
	started := time.Now()
	err := RunUploadJob(ctx, clients, packersdk.TestUi(t), "S3 uploader", job, generateSecret, 10*time.Second)
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("expected the upload to stop once cancelled, waited: %s", elapsed)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected an error saying the build was cancelled, got: %v", err)
	}
}
