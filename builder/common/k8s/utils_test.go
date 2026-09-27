package k8s

import (
	"context"
	"errors"
	"fmt"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/apimachinery/pkg/util/httpstream/spdy"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	restclient "k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/portforward"
	kubevirtv1 "kubevirt.io/api/core/v1"
	kubevirtfake "kubevirt.io/client-go/kubevirt/fake"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

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

func TestPortForwardReturnsErrorBeforeTimeout(t *testing.T) {
	forwardingError := errors.New("pods \"virt-launcher-base-ubuntu\" is forbidden")
	start := time.Now()

	_, err := forwardPortsUntilStopped(func(ready, stop chan struct{}) error {
		return forwardingError
	}, 5*time.Second, time.Millisecond)

	if !errors.Is(err, forwardingError) {
		t.Errorf("expected the forwarding error, got: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("expected the error without waiting for the timeout, waited: %s", elapsed)
	}
}

func TestPortForwardIsStoppedOnTimeout(t *testing.T) {
	stopped := make(chan struct{})

	_, err := forwardPortsUntilStopped(func(ready, stop chan struct{}) error {
		<-stop
		close(stopped)
		return nil
	}, 10*time.Millisecond, time.Millisecond)

	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected a timeout error, got: %v", err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Error("expected port forwarding to be stopped after the timeout")
	}
}

func TestPortForwardIsSetUpAgainUntilStopped(t *testing.T) {
	retryInterval := 10 * time.Millisecond
	var attempts atomic.Int32
	connectionLost := make(chan struct{})
	forwardingAgain := make(chan struct{})

	stop, err := forwardPortsUntilStopped(func(ready, stop chan struct{}) error {
		close(ready)
		if attempts.Add(1) == 1 {
			<-connectionLost
			return errors.New("lost connection to pod")
		}
		close(forwardingAgain)
		<-stop
		return nil
	}, 5*time.Second, retryInterval)
	if err != nil {
		t.Fatalf("expected port forwarding to be ready, got: %v", err)
	}

	close(connectionLost)
	select {
	case <-forwardingAgain:
	case <-time.After(5 * time.Second):
		t.Fatal("expected port forwarding to be set up again after its error")
	}
	close(stop)
	time.Sleep(10 * retryInterval)
	if count := attempts.Load(); count != 2 {
		t.Errorf("expected no attempt once stopped, got %d attempts", count)
	}
}

func TestPortForwardWaitsBetweenAttempts(t *testing.T) {
	retryInterval := 20 * time.Millisecond
	var attempts atomic.Int32
	connectionLost := make(chan struct{})

	stop, err := forwardPortsUntilStopped(func(ready, stop chan struct{}) error {
		if attempts.Add(1) == 1 {
			close(ready)
			<-connectionLost
		}
		return errors.New("pod is not running")
	}, 5*time.Second, retryInterval)
	if err != nil {
		t.Fatalf("expected port forwarding to be ready, got: %v", err)
	}

	close(connectionLost)
	time.Sleep(10 * retryInterval)
	close(stop)
	if count := attempts.Load(); count < 2 || count > 11 {
		t.Errorf("expected an attempt every %s, got %d attempts in %s", retryInterval, count, 10*retryInterval)
	}
}

func TestPortForwardRefusesLocalPortInUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if _, err := httpstream.Handshake(request, response, []string{portforward.PortForwardProtocolV1Name}); err != nil {
			return
		}
		connection := spdy.NewResponseUpgrader().UpgradeResponse(response, request, httpstream.NoOpNewStreamHandler)
		if connection != nil {
			<-connection.CloseChan()
		}
	}))
	defer server.Close()
	config := &restclient.Config{Host: server.URL}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatalf("failed to create the client: %v", err)
	}

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()
	portMapping := fmt.Sprintf("%d:22", listener.Addr().(*net.TCPAddr).Port)

	stop, err := RunAsyncPortForward(&Clients{Kubernetes: client, RestConfig: config}, "virt-launcher-base-ubuntu", "packer", []string{portMapping})
	if err == nil {
		close(stop)
		t.Fatal("expected an error when the local port is in use on 127.0.0.1")
	}
}
