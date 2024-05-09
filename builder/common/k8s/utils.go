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
	"k8s.io/apimachinery/pkg/watch"
	v1 "k8s.io/client-go/kubernetes/typed/batch/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/portforward"
	watchtools "k8s.io/client-go/tools/watch"
	"k8s.io/client-go/transport/spdy"
	"kubevirt.io/client-go/kubecli"
	"log"
	"net/http"
	"os"
	"time"
)

const (
	ImageBuilderTaintKey   = "pelo.tech/uki-labs"
	ImageBuilderTaintValue = "builder"
	PortFowardTimeout      = 5 * time.Second
)

func RunAsyncPortForward(client kubecli.KubevirtClient, podName, namespace string, ports []string) (chan struct{}, error) {
	stopChan := make(chan struct{}, 1)
	readyChan := make(chan struct{})

	go func() {
		err := runPortForward(client, podName, namespace, ports, readyChan, stopChan)
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

func runPortForward(client kubecli.KubevirtClient, podName, namespace string, ports []string, ready, stop chan struct{}) error {
	url := client.CoreV1().RESTClient().Post().
		Namespace(namespace).
		Resource("pods").
		Name(podName).
		SubResource("portforward").
		URL()

	roundTripper, upgrader, err := spdy.RoundTripperFor(client.Config())
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

func WaitForResource(client *rest.RESTClient, namespace, resource, name, version string, timeout time.Duration, handleEvent watchtools.ConditionFunc) (*watch.Event, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()

	listWatch := cache.NewListWatchFromClient(client, resource, namespace, fields.OneTermEqualSelector("metadata.name", name))
	event, err := watchtools.Until(ctx, version, listWatch, handleEvent)
	if err != nil {
		return nil, err
	}

	return event, nil
}

func WaitForJobCompletion(client v1.BatchV1Interface, ui packersdk.Ui, job *batchv1.Job, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()

	watcher, err := client.Jobs(job.Namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: labels.SelectorFromSet(map[string]string{
			"metadata.name": job.Name,
		}).String(),
	})
	if err != nil {
		return fmt.Errorf("failed to get job state %s/%s: %w", job.Namespace, job.Name, err)
	}

	for {
		select {
		case event, _ := <-watcher.ResultChan():
			updatedJob, _ := event.Object.(*batchv1.Job)
			for index, condition := range updatedJob.Status.Conditions {
				if index == 0 {
					ui.Message(fmt.Sprintf("condition '%s' changed to '%s'", condition.Type, condition.Status))
				}
				if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
					return nil
				} else if (condition.Type == batchv1.JobFailed || condition.Type == batchv1.JobFailureTarget) && condition.Status == corev1.ConditionTrue {
					return fmt.Errorf("job condition changed to failed")
				}
			}

		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for job to be completed")
		}
	}
}
