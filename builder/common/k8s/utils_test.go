package k8s

import (
	"context"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	kubevirtv1 "kubevirt.io/api/core/v1"
	kubevirtfake "kubevirt.io/client-go/kubevirt/fake"
	"strings"
	"testing"
	"time"
)

func TestWaitForVirtualMachine(t *testing.T) {
	//ns := "packer"
	//resource := "virtualmachines"
	//name := "image-builder"
	//client, _ := GetKubevirtClient()
	//
	//vm, _ := client.VirtualMachine(ns).Get(context.TODO(), name, metav1.GetOptions{})
	//
	//conditionFunc := func(event watch.Event) (bool, error) {
	//	vm, ok := event.Object.(*kubevirtv1.VirtualMachine)
	//	if !ok {
	//		return false, fmt.Errorf("unexpected type for %v", event.Object)
	//	}
	//
	//	for _, condition := range vm.Status.Conditions {
	//		if condition.Type == kubevirtv1.VirtualMachineReady && condition.Status == corev1.ConditionTrue {
	//			return true, nil
	//		}
	//	}
	//	return false, nil
	//}
	//
	//_, err := WaitForResource(client.RestClient(), vm.Namespace, resource, vm.Name, "51162567", 10*time.Minute, conditionFunc)
	//assert.NoError(t, err)
}

func TestWaitForVirtualMachineExport(t *testing.T) {
	//ns := "packer"
	//resource := "virtualmachineexports"
	//name := "base-ubuntu-2204"
	//client, _ := GetKubevirtClient()
	//
	//ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	//defer cancel()
	//
	//watcher, _ := client.GeneratedKubeVirtClient().ExportV1alpha1().VirtualMachineExports(ns).Watch(ctx, metav1.ListOptions{
	//	FieldSelector: labels.SelectorFromSet(map[string]string{
	//		"metadata.name": name,
	//	}).String(),
	//})
	//defer watcher.Stop()
	//
	//for {
	//	select {
	//	case event, _ := <-watcher.ResultChan():
	//		updatedExport, _ := event.Object.(*exportv1.VirtualMachineExport)
	//		if updatedExport.Status.Phase == exportv1.Ready {
	//			println("congrats!")
	//			return
	//		}
	//
	//	case <-ctx.Done():
	//		// that's it
	//	}
	//}
}

func TestRunAsyncPortForward(t *testing.T) {
	//ns := "packer"
	//podName := "virt-launcher-image-builder-q4fvf"
	//client, _ := GetKubevirtClient()
	//
	//stopChan, err := RunAsyncPortForward(client, podName, ns, []string{"3389:3389"})
	//assert.NoError(t, err)
	//close(stopChan)
}

func TestString(t *testing.T) {
	println(labels.SelectorFromSet(map[string]string{
		kubevirtv1.DeprecatedVirtualMachineNameLabel: "name",
	}).String())
}

func newJobWithPod(containerState corev1.ContainerState) (*batchv1.Job, *corev1.Pod) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu-libguestfs", Namespace: "packer", UID: "job-uid"},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "base-ubuntu-libguestfs-abcde",
			Namespace: job.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(job, batchv1.SchemeGroupVersion.WithKind("Job")),
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "libguestfs", State: containerState},
			},
		},
	}
	return job, pod
}

func TestWaitForJobCompletionSucceeds(t *testing.T) {
	job, pod := newJobWithPod(corev1.ContainerState{})
	client := fake.NewSimpleClientset(job, pod)
	watcher := watch.NewFake()
	client.PrependWatchReactor("jobs", k8stesting.DefaultWatchReactor(watcher, nil))

	completedJob := job.DeepCopy()
	completedJob.Status.Conditions = []batchv1.JobCondition{
		{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
	}
	go watcher.Modify(completedJob)

	err := WaitForJobCompletion(client, packersdk.TestUi(t), job, 5*time.Second)
	if err != nil {
		t.Fatalf("expected job to complete, got: %v", err)
	}
}

func TestWaitForJobCompletionTimeoutReportsPodState(t *testing.T) {
	job, pod := newJobWithPod(corev1.ContainerState{
		Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "back-off pulling image"},
	})
	client := fake.NewSimpleClientset(job, pod)

	err := WaitForJobCompletion(client, packersdk.TestUi(t), job, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	for _, expected := range []string{"timeout", pod.Name, "ImagePullBackOff"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("expected error to contain %q, got: %v", expected, err)
		}
	}
}

func TestWaitForJobCompletionFailureReportsPodState(t *testing.T) {
	job, pod := newJobWithPod(corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 1},
	})
	client := fake.NewSimpleClientset(job, pod)
	watcher := watch.NewFake()
	client.PrependWatchReactor("jobs", k8stesting.DefaultWatchReactor(watcher, nil))

	failedJob := job.DeepCopy()
	failedJob.Status.Conditions = []batchv1.JobCondition{
		{Type: batchv1.JobFailed, Status: corev1.ConditionTrue},
	}
	go watcher.Modify(failedJob)

	err := WaitForJobCompletion(client, packersdk.TestUi(t), job, 5*time.Second)
	if err == nil {
		t.Fatal("expected a failure error")
	}
	for _, expected := range []string{"failed", pod.Name, "exit code 1", "fake logs"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("expected error to contain %q, got: %v", expected, err)
		}
	}
}

func TestWaitForJobCompletionReportsLatestPodOnly(t *testing.T) {
	job, firstPod := newJobWithPod(corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 1},
	})
	firstPod.CreationTimestamp = metav1.NewTime(time.Now().Add(-time.Minute))
	latestPod := firstPod.DeepCopy()
	latestPod.Name = "base-ubuntu-libguestfs-latest"
	latestPod.CreationTimestamp = metav1.Now()
	client := fake.NewSimpleClientset(job, firstPod, latestPod)

	err := WaitForJobCompletion(client, packersdk.TestUi(t), job, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if !strings.Contains(err.Error(), latestPod.Name) || strings.Contains(err.Error(), firstPod.Name) {
		t.Errorf("expected error to describe the latest pod only, got: %v", err)
	}
}

func TestWaitForJobCompletionClosedWatch(t *testing.T) {
	job, _ := newJobWithPod(corev1.ContainerState{})
	client := fake.NewSimpleClientset(job)
	watcher := watch.NewFake()
	client.PrependWatchReactor("jobs", k8stesting.DefaultWatchReactor(watcher, nil))
	watcher.Stop()

	err := WaitForJobCompletion(client, packersdk.TestUi(t), job, 5*time.Second)
	if err == nil {
		t.Fatal("expected an error when the watch is closed before the job completes")
	}
}

func TestWaitForVirtualMachineStopped(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"},
		Status:     kubevirtv1.VirtualMachineStatus{PrintableStatus: kubevirtv1.VirtualMachineStatusStopping},
	}
	client := kubevirtfake.NewSimpleClientset(vm).KubevirtV1().VirtualMachines(vm.Namespace)

	err := WaitForVirtualMachineStopped(client, vm.Name, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected a timeout while the Virtual Machine is still stopping")
	}

	vm.Status.PrintableStatus = kubevirtv1.VirtualMachineStatusStopped
	if _, err = client.UpdateStatus(context.Background(), vm, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update Virtual Machine status: %v", err)
	}

	err = WaitForVirtualMachineStopped(client, vm.Name, 5*time.Second)
	if err != nil {
		t.Fatalf("expected Virtual Machine to be seen as stopped, got: %v", err)
	}
}

func TestWaitForJobCompletionReportsConditionsOfPendingPodsOnly(t *testing.T) {
	unschedulable := corev1.PodCondition{
		Type:    corev1.PodScheduled,
		Status:  corev1.ConditionFalse,
		Reason:  "Unschedulable",
		Message: "0/1 nodes are available",
	}

	job, pendingPod := newJobWithPod(corev1.ContainerState{})
	pendingPod.Status.Conditions = []corev1.PodCondition{unschedulable}
	err := WaitForJobCompletion(fake.NewSimpleClientset(job, pendingPod), packersdk.TestUi(t), job, 50*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), unschedulable.Message) {
		t.Errorf("expected error to explain why the pod is pending, got: %v", err)
	}

	job, failedPod := newJobWithPod(corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 1},
	})
	failedPod.Status.Phase = corev1.PodFailed
	failedPod.Status.Conditions = []corev1.PodCondition{unschedulable}
	err = WaitForJobCompletion(fake.NewSimpleClientset(job, failedPod), packersdk.TestUi(t), job, 50*time.Millisecond)
	if err == nil || strings.Contains(err.Error(), unschedulable.Message) {
		t.Errorf("expected error to leave out the conditions of a pod that ran, got: %v", err)
	}
}
