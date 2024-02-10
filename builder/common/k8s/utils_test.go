package k8s

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	kubevirtv1 "kubevirt.io/api/core/v1"
	exportv1 "kubevirt.io/api/export/v1alpha1"
	"testing"
	"time"
)

func TestWaitForVirtualMachine(t *testing.T) {
	ns := "packer"
	resource := "virtualmachines"
	name := "image-builder"
	client, _ := GetKubevirtClient()

	vm, _ := client.VirtualMachine(ns).Get(context.TODO(), name, &metav1.GetOptions{})

	conditionFunc := func(event watch.Event) (bool, error) {
		vm, ok := event.Object.(*kubevirtv1.VirtualMachine)
		if !ok {
			return false, fmt.Errorf("unexpected type for %v", event.Object)
		}

		for _, condition := range vm.Status.Conditions {
			if condition.Type == kubevirtv1.VirtualMachineReady && condition.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	}

	_, err := WaitForResource(client.RestClient(), vm.Namespace, resource, vm.Name, "51162567", 10*time.Minute, conditionFunc)
	assert.NoError(t, err)
}

func TestWaitForVirtualMachineExport(t *testing.T) {
	ns := "packer"
	//resource := "virtualmachineexports"
	name := "base-ubuntu-2204"
	client, _ := GetKubevirtClient()

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	defer cancel()

	watcher, _ := client.GeneratedKubeVirtClient().ExportV1alpha1().VirtualMachineExports(ns).Watch(ctx, metav1.ListOptions{
		FieldSelector: labels.SelectorFromSet(map[string]string{
			"metadata.name": name,
		}).String(),
	})
	defer watcher.Stop()

	for {
		select {
		case event, _ := <-watcher.ResultChan():
			updatedExport, _ := event.Object.(*exportv1.VirtualMachineExport)
			if updatedExport.Status.Phase == exportv1.Ready {
				println("congrats!")
				return
			}

		case <-ctx.Done():
			// that's it
		}
	}
}

func TestRunAsyncPortForward(t *testing.T) {
	ns := "packer"
	podName := "virt-launcher-image-builder-q4fvf"
	client, _ := GetKubevirtClient()

	stopChan, err := RunAsyncPortForward(client, podName, ns, []string{"3389:3389"})
	assert.NoError(t, err)
	close(stopChan)
}

func TestString(t *testing.T) {
	println(labels.SelectorFromSet(map[string]string{
		kubevirtv1.VirtualMachineNameLabel: "name",
	}).String())
}
