package k8s

import (
	"context"
	"errors"
	"fmt"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/apimachinery/pkg/util/httpstream/spdy"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	restclient "k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/portforward"
	kubevirtv1 "kubevirt.io/api/core/v1"
	cdifake "kubevirt.io/client-go/containerizeddataimporter/fake"
	kubevirtfake "kubevirt.io/client-go/kubevirt/fake"
	cdiv1beta1 "kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// the KubeVirt and CDI fakes do not serve the streaming lists that client-go asks for by default
	_ = os.Setenv("KUBE_FEATURE_WatchListClient", "false")
	os.Exit(m.Run())
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

// closeFirstWatch runs change, then closes the first watch at once, as the API server does after 30 to 60 minutes
func closeFirstWatch(client *k8stesting.Fake, resource string, change func()) {
	var watches atomic.Int32
	client.PrependWatchReactor(resource, func(k8stesting.Action) (bool, watch.Interface, error) {
		if watches.Add(1) > 1 {
			return false, nil, nil
		}
		change()
		closedWatch := watch.NewFake()
		closedWatch.Stop()
		return true, closedWatch, nil
	})
}

func TestWaitForJobCompletionOutlivesAClosedWatch(t *testing.T) {
	job, pod := newJobWithPod(corev1.ContainerState{})
	client := fake.NewSimpleClientset(job, pod)
	completedJob := job.DeepCopy()
	completedJob.Status.Conditions = []batchv1.JobCondition{
		{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
	}
	closeFirstWatch(&client.Fake, "jobs", func() {
		_ = client.Tracker().Update(batchv1.SchemeGroupVersion.WithResource("jobs"), completedJob, job.Namespace)
	})

	err := WaitForJobCompletion(client, packersdk.TestUi(t), job, 5*time.Second)
	if err != nil {
		t.Fatalf("expected the job to be seen as completed once watched again, got: %v", err)
	}
}

func TestWaitForJobCompletionReportsAJobThatCannotBeListed(t *testing.T) {
	job, _ := newJobWithPod(corev1.ContainerState{})
	client := fake.NewSimpleClientset(job)
	forbidden := k8serrors.NewForbidden(batchv1.Resource("jobs"), "", errors.New("cannot list resource \"jobs\""))
	client.PrependReactor("list", "jobs", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, forbidden
	})

	err := WaitForJobCompletion(client, packersdk.TestUi(t), job, 5*time.Second)
	if !errors.Is(err, forbidden) {
		t.Errorf("expected the error of the list instead of waiting for the timeout, got: %v", err)
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

func newDataVolume(phase cdiv1beta1.DataVolumePhase) *cdiv1beta1.DataVolume {
	return &cdiv1beta1.DataVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu-20260926153045", Namespace: "images"},
		Status: cdiv1beta1.DataVolumeStatus{
			Phase:        phase,
			ClaimName:    "base-ubuntu-20260926153045",
			RestartCount: 3,
			Conditions: []cdiv1beta1.DataVolumeCondition{
				{Type: cdiv1beta1.DataVolumeBound, Status: corev1.ConditionTrue, Reason: "Bound", Message: "PVC base-ubuntu-20260926153045 Bound"},
				{Type: cdiv1beta1.DataVolumeRunning, Status: corev1.ConditionFalse, Reason: "Error", Message: "expected status code 200, got 401"},
			},
		},
	}
}

func newImporterPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "importer-base-ubuntu", Namespace: "images"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "importer", State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff", Message: "back-off restarting failed container"},
				}},
			},
		},
	}
}

func newClaim(name string, annotations map[string]string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "images", Annotations: annotations},
	}
}

func TestWaitForDataVolumeImportSucceeds(t *testing.T) {
	dataVolume := newDataVolume(cdiv1beta1.ImportInProgress)
	cdiClient := cdifake.NewSimpleClientset(dataVolume)
	watcher := watch.NewFake()
	cdiClient.PrependWatchReactor("datavolumes", k8stesting.DefaultWatchReactor(watcher, nil))
	go func() {
		watcher.Add(dataVolume)
		watcher.Modify(newDataVolume(cdiv1beta1.Succeeded))
	}()

	clients := &Clients{Kubernetes: fake.NewSimpleClientset(), CDI: cdiClient}
	err := WaitForDataVolumeImport(clients, packersdk.TestUi(t), dataVolume, 5*time.Second)
	if err != nil {
		t.Fatalf("expected the import to succeed, got: %v", err)
	}
}

func TestWaitForDataVolumeImportFailureReportsImporterPod(t *testing.T) {
	dataVolume := newDataVolume(cdiv1beta1.Failed)
	cdiClient := cdifake.NewSimpleClientset(dataVolume)
	watcher := watch.NewFake()
	cdiClient.PrependWatchReactor("datavolumes", k8stesting.DefaultWatchReactor(watcher, nil))
	go watcher.Modify(dataVolume)
	pod := newImporterPod()
	claim := newClaim(dataVolume.Name, map[string]string{"cdi.kubevirt.io/storage.import.importPodName": pod.Name})
	kubeClient := fake.NewSimpleClientset(pod, claim)

	clients := &Clients{Kubernetes: kubeClient, CDI: cdiClient}
	err := WaitForDataVolumeImport(clients, packersdk.TestUi(t), dataVolume, 5*time.Second)
	if err == nil {
		t.Fatal("expected a failure error")
	}
	for _, expected := range []string{"'Failed'", "3 restarts", "expected status code 200, got 401", pod.Name, "CrashLoopBackOff"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("expected error to contain %q, got: %v", expected, err)
		}
	}
	if strings.Contains(err.Error(), "PVC base-ubuntu-20260926153045 Bound") {
		t.Errorf("expected error to leave out the conditions which are met, got: %v", err)
	}
}

func TestWaitForDataVolumeImportTimeoutReportsImporterPodOfPopulator(t *testing.T) {
	dataVolume := newDataVolume(cdiv1beta1.ImportInProgress)
	cdiClient := cdifake.NewSimpleClientset(dataVolume)
	pod := newImporterPod()
	claim := newClaim(dataVolume.Name, map[string]string{"cdi.kubevirt.io/storage.populator.pvcPrime": "prime-claim-uid"})
	populatorClaim := newClaim("prime-claim-uid", map[string]string{"cdi.kubevirt.io/storage.import.importPodName": pod.Name})
	kubeClient := fake.NewSimpleClientset(pod, claim, populatorClaim)

	clients := &Clients{Kubernetes: kubeClient, CDI: cdiClient}
	err := WaitForDataVolumeImport(clients, packersdk.TestUi(t), dataVolume, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	for _, expected := range []string{"timeout", "'ImportInProgress'", "expected status code 200, got 401", pod.Name, "CrashLoopBackOff"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("expected error to contain %q, got: %v", expected, err)
		}
	}
}

func TestWaitForDataVolumeImportTimeoutWithoutImporterPod(t *testing.T) {
	dataVolume := newDataVolume(cdiv1beta1.Pending)
	dataVolume.Status.ClaimName = ""
	clients := &Clients{Kubernetes: fake.NewSimpleClientset(), CDI: cdifake.NewSimpleClientset(dataVolume)}

	err := WaitForDataVolumeImport(clients, packersdk.TestUi(t), dataVolume, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	for _, expected := range []string{"timeout", "'Pending'", "no importer pod"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("expected error to contain %q, got: %v", expected, err)
		}
	}
}

func TestWaitForDataVolumeImportOutlivesAClosedWatch(t *testing.T) {
	dataVolume := newDataVolume(cdiv1beta1.ImportInProgress)
	cdiClient := cdifake.NewSimpleClientset(dataVolume)
	closeFirstWatch(&cdiClient.Fake, "datavolumes", func() {
		_ = cdiClient.Tracker().Update(cdiv1beta1.SchemeGroupVersion.WithResource("datavolumes"), newDataVolume(cdiv1beta1.Succeeded), dataVolume.Namespace)
	})

	clients := &Clients{Kubernetes: fake.NewSimpleClientset(), CDI: cdiClient}
	err := WaitForDataVolumeImport(clients, packersdk.TestUi(t), dataVolume, 5*time.Second)
	if err != nil {
		t.Fatalf("expected the import to be seen as done once watched again, got: %v", err)
	}
}
