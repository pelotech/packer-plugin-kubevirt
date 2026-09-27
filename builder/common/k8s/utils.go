package k8s

import (
	"context"
	"fmt"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/portforward"
	watchtools "k8s.io/client-go/tools/watch"
	"k8s.io/client-go/transport/spdy"
	"k8s.io/utils/pointer"
	kubevirtv1 "kubevirt.io/api/core/v1"
	kvcorev1 "kubevirt.io/client-go/kubevirt/typed/core/v1"
	cdiv1beta1 "kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	PortFowardTimeout              = 5 * time.Second
	VirtualMachineStopPollInterval = time.Second
	ContainerLogsTailLines         = 10

	importerPodAnnotation    = "cdi.kubevirt.io/storage.import.importPodName"
	populatorClaimAnnotation = "cdi.kubevirt.io/storage.populator.pvcPrime"
)

func RunAsyncPortForward(clients *Clients, podName, namespace string, ports []string) (chan struct{}, error) {
	stopChan := make(chan struct{}, 1)
	readyChan := make(chan struct{})

	go func() {
		err := runPortForward(clients, podName, namespace, ports, readyChan, stopChan)
		if err != nil {
			log.Printf("error while running port forwarding: %v", err)
		}
	}()

	select {
	case <-readyChan:
		log.Printf("Port forwarding is ready.")
	case <-time.After(PortFowardTimeout):
		return nil, fmt.Errorf("timeout waiting for port forwarding to be ready")
	}

	return stopChan, nil
}

func runPortForward(clients *Clients, podName, namespace string, ports []string, ready, stop chan struct{}) error {
	url := clients.Kubernetes.CoreV1().RESTClient().Post().
		Namespace(namespace).
		Resource("pods").
		Name(podName).
		SubResource("portforward").
		URL()

	roundTripper, upgrader, err := spdy.RoundTripperFor(clients.RestConfig)
	if err != nil {
		return err
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, url)

	forwarder, err := portforward.New(dialer, ports, stop, ready, os.Stdout, os.Stderr)
	if err != nil {
		return err
	}

	return forwarder.ForwardPorts()
}

type HandleEventFunc func(context.Context, watch.Event) (bool, error)

func WaitForResource(client cache.Getter, namespace, resource, name, version string, timeout time.Duration, handleEvent watchtools.ConditionFunc) (*watch.Event, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()

	listWatch := cache.NewListWatchFromClient(client, resource, namespace, fields.OneTermEqualSelector("metadata.name", name))
	event, err := watchtools.Until(ctx, version, listWatch, handleEvent)
	if err != nil {
		return nil, err
	}

	return event, nil
}

func WaitForJobCompletion(client kubernetes.Interface, ui packersdk.Ui, job *batchv1.Job, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()

	watcher, err := client.BatchV1().Jobs(job.Namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: labels.SelectorFromSet(map[string]string{
			"metadata.name": job.Name,
		}).String(),
	})
	if err != nil {
		return fmt.Errorf("failed to get job state %s/%s: %w", job.Namespace, job.Name, err)
	}
	defer watcher.Stop()

	for {
		select {
		case event, ok := <-watcher.ResultChan():
			if !ok {
				if ctx.Err() != nil {
					return fmt.Errorf("timeout waiting for job to be completed: %s", describeJobPods(client, job))
				}
				return fmt.Errorf("watch closed before job was completed: %s", describeJobPods(client, job))
			}
			updatedJob, ok := event.Object.(*batchv1.Job)
			if !ok {
				continue
			}
			for index, condition := range updatedJob.Status.Conditions {
				if index == 0 {
					ui.Message(fmt.Sprintf("condition '%s' changed to '%s'", condition.Type, condition.Status))
				}
				if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
					return nil
				} else if (condition.Type == batchv1.JobFailed || condition.Type == batchv1.JobFailureTarget) && condition.Status == corev1.ConditionTrue {
					return fmt.Errorf("job condition changed to failed: %s", describeJobPods(client, job))
				}
			}

		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for job to be completed: %s", describeJobPods(client, job))
		}
	}
}

func describeJobPods(client kubernetes.Interface, job *batchv1.Job) string {
	pods, err := client.CoreV1().Pods(job.Namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return fmt.Sprintf("failed to list job pods: %s", err)
	}

	var latestPod *corev1.Pod
	for _, pod := range pods.Items {
		if !metav1.IsControlledBy(&pod, job) {
			continue
		}
		if latestPod == nil || latestPod.CreationTimestamp.Before(&pod.CreationTimestamp) {
			latestPod = &pod
		}
	}
	if latestPod == nil {
		return "no pod found for the job"
	}

	return describePod(client, latestPod)
}

func describePod(client kubernetes.Interface, pod *corev1.Pod) string {
	description := fmt.Sprintf("pod '%s' is '%s'", pod.Name, pod.Status.Phase)
	for _, condition := range pod.Status.Conditions {
		if pod.Status.Phase == corev1.PodPending && condition.Status == corev1.ConditionFalse && condition.Message != "" {
			description += fmt.Sprintf(", %s: %s", condition.Reason, condition.Message)
		}
	}
	statuses := append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...)
	for _, status := range statuses {
		if waiting := status.State.Waiting; waiting != nil {
			description += fmt.Sprintf(", container '%s' is waiting: %s %s", status.Name, waiting.Reason, waiting.Message)
		}
		if terminated := status.State.Terminated; terminated != nil && terminated.ExitCode != 0 {
			description += fmt.Sprintf(", container '%s' terminated with exit code %d: %s %s", status.Name, terminated.ExitCode, terminated.Reason, terminated.Message)
			description += fmt.Sprintf(", last logs: %s", readContainerLogs(client, pod, status.Name))
		}
	}

	return description
}

func readContainerLogs(client kubernetes.Interface, pod *corev1.Pod, container string) string {
	logs, err := client.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Container: container,
		TailLines: pointer.Int64(ContainerLogsTailLines),
	}).DoRaw(context.Background())
	if err != nil {
		return fmt.Sprintf("failed to read logs: %s", err)
	}

	return strings.TrimSpace(string(logs))
}

func WaitForVirtualMachineStopped(client kvcorev1.VirtualMachineInterface, name string, timeout time.Duration) error {
	err := wait.PollUntilContextTimeout(context.Background(), VirtualMachineStopPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		vm, err := client.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return vm.Status.PrintableStatus == kubevirtv1.VirtualMachineStatusStopped, nil
	})
	if err != nil {
		return fmt.Errorf("failed to wait for Virtual Machine to be stopped: %w", err)
	}

	return nil
}

func WaitForDataVolumeImport(clients *Clients, ui packersdk.Ui, dataVolume *cdiv1beta1.DataVolume, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()

	watcher, err := clients.CDI.CdiV1beta1().DataVolumes(dataVolume.Namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: labels.SelectorFromSet(map[string]string{
			"metadata.name": dataVolume.Name,
		}).String(),
	})
	if err != nil {
		return fmt.Errorf("failed to get Data Volume state %s/%s: %w", dataVolume.Namespace, dataVolume.Name, err)
	}
	defer watcher.Stop()

	var progress string
	for {
		select {
		case event, ok := <-watcher.ResultChan():
			if !ok {
				if ctx.Err() != nil {
					return fmt.Errorf("timeout waiting for Data Volume to be imported: %s", describeDataVolumeImport(clients, dataVolume))
				}
				return fmt.Errorf("watch closed before Data Volume was imported: %s", describeDataVolumeImport(clients, dataVolume))
			}
			updatedDataVolume, ok := event.Object.(*cdiv1beta1.DataVolume)
			if !ok {
				continue
			}
			status := updatedDataVolume.Status
			updatedProgress := fmt.Sprintf("phase '%s', progress '%s'", status.Phase, status.Progress)
			if status.Phase != cdiv1beta1.PhaseUnset && updatedProgress != progress {
				progress = updatedProgress
				ui.Message(progress)
			}
			if status.Phase == cdiv1beta1.Succeeded {
				return nil
			} else if status.Phase == cdiv1beta1.Failed {
				return fmt.Errorf("import of Data Volume failed: %s", describeDataVolumeImport(clients, dataVolume))
			}

		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for Data Volume to be imported: %s", describeDataVolumeImport(clients, dataVolume))
		}
	}
}

func describeDataVolumeImport(clients *Clients, dataVolume *cdiv1beta1.DataVolume) string {
	dataVolume, err := clients.CDI.CdiV1beta1().DataVolumes(dataVolume.Namespace).Get(context.Background(), dataVolume.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Sprintf("failed to get Data Volume: %s", err)
	}

	description := fmt.Sprintf("Data Volume '%s' is '%s' after %d restarts", dataVolume.Name, dataVolume.Status.Phase, dataVolume.Status.RestartCount)
	for _, condition := range dataVolume.Status.Conditions {
		if condition.Status != corev1.ConditionTrue && condition.Message != "" {
			description += fmt.Sprintf(", %s: %s", condition.Reason, condition.Message)
		}
	}

	return fmt.Sprintf("%s, %s", description, describeImporterPod(clients.Kubernetes, dataVolume))
}

func describeImporterPod(client kubernetes.Interface, dataVolume *cdiv1beta1.DataVolume) string {
	if dataVolume.Status.ClaimName == "" {
		return "no importer pod, the volume is not claimed yet"
	}

	claims := client.CoreV1().PersistentVolumeClaims(dataVolume.Namespace)
	claim, err := claims.Get(context.Background(), dataVolume.Status.ClaimName, metav1.GetOptions{})
	if err != nil {
		return fmt.Sprintf("failed to find the importer pod: %s", err)
	}
	if populatorClaimName, found := claim.Annotations[populatorClaimAnnotation]; found {
		// with a CSI storage class, the importer pod fills a temporary claim
		claim, err = claims.Get(context.Background(), populatorClaimName, metav1.GetOptions{})
		if err != nil {
			return fmt.Sprintf("failed to find the importer pod: %s", err)
		}
	}

	podName, found := claim.Annotations[importerPodAnnotation]
	if !found {
		return "no importer pod yet"
	}
	pod, err := client.CoreV1().Pods(claim.Namespace).Get(context.Background(), podName, metav1.GetOptions{})
	if err != nil {
		return fmt.Sprintf("failed to get the importer pod: %s", err)
	}

	return describePod(client, pod)
}
