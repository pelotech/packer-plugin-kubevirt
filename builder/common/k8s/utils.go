package k8s

import (
	"context"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"kubevirt.io/client-go/kubecli"
	"net/http"
	"os"
	"time"
)

func RunPortForward(client kubecli.KubevirtClient, podName, namespace string, ports []string) error {
	url := client.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("portforward").
		URL()

	roundTripper, upgrader, err := spdy.RoundTripperFor(client.Config())
	if err != nil {
		return err
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, url)

	forwarder, err := portforward.New(dialer, ports, make(chan struct{}, 1), make(chan struct{}, 1), os.Stdout, os.Stderr)
	if err != nil {
		return err
	}
	return forwarder.ForwardPorts()
}

type HandleEventFunc func(context.Context, watch.Event) (bool, error)

func WaitForResource(client dynamic.Interface, resource schema.GroupVersionResource, namespace, name string, timeout time.Duration, handleEvent HandleEventFunc) error {
	ctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()

	current, err := client.Resource(resource).Namespace(namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name=%s", name),
	})
	if err != nil {
		return err
	}

	for event := range current.ResultChan() {
		done, err := handleEvent(ctx, event)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}

	return fmt.Errorf("resource %s/%s did not meet the required condition within the specified timeout", resource, name)
}
