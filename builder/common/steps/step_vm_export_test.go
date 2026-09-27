package steps

import (
	"context"
	"errors"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	kubevirtv1 "kubevirt.io/api/core/v1"
	exportv1 "kubevirt.io/api/export/v1"
	kubevirtfake "kubevirt.io/client-go/kubevirt/fake"
	"net/http"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	vmctx "packer-plugin-kubevirt/builder/common/vm"
	"strings"
	"testing"
	"time"
)

func TestStepExportVMRunsSysprepOnceVirtualMachineIsStopped(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"},
		Spec:       kubevirtv1.VirtualMachineSpec{Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{}},
		Status:     kubevirtv1.VirtualMachineStatus{PrintableStatus: kubevirtv1.VirtualMachineStatusRunning},
	}

	kubevirtClient := kubevirtfake.NewSimpleClientset(vm)
	vmClient := kubevirtClient.KubevirtV1().VirtualMachines(vm.Namespace)
	kubevirtClient.PrependReactor("put", "virtualmachines/stop", func(k8stesting.Action) (bool, runtime.Object, error) {
		go func() {
			time.Sleep(100 * time.Millisecond)
			stoppedVm := vm.DeepCopy()
			stoppedVm.Status.PrintableStatus = kubevirtv1.VirtualMachineStatusStopped
			_, _ = vmClient.UpdateStatus(context.Background(), stoppedVm, metav1.UpdateOptions{})
		}()
		return true, nil, nil
	})

	var statusAtJobCreation kubevirtv1.VirtualMachinePrintableStatus
	kubeClient := k8sfake.NewSimpleClientset()
	kubeClient.PrependReactor("create", "jobs", func(k8stesting.Action) (bool, runtime.Object, error) {
		currentVm, err := vmClient.Get(context.Background(), vm.Name, metav1.GetOptions{})
		if err != nil {
			return true, nil, err
		}
		statusAtJobCreation = currentVm.Status.PrintableStatus
		return true, nil, errors.New("halt the step once the job creation is reached")
	})

	osFamily := vmctx.Linux
	appContext := &common.AppContext{State: new(multistep.BasicStateBag)}
	appContext.Put(common.PackerUi, packersdk.TestUi(t))
	appContext.Put(common.VirtualMachine, vm)
	appContext.Put(common.VirtualMachineOsFamily, &osFamily)

	step := &StepExportVM{Clients: &k8s.Clients{Kubernetes: kubeClient, Kubevirt: kubevirtClient}, VmExportTimeOut: 5 * time.Second}
	action := step.Run(context.Background(), appContext.State)

	if action != multistep.ActionHalt {
		t.Fatalf("expected the step to halt on job creation, got action: %v", action)
	}
	if statusAtJobCreation != kubevirtv1.VirtualMachineStatusStopped {
		t.Fatalf("expected the 'libguestfs' job to be created once the Virtual Machine is stopped, status was: '%s'", statusAtJobCreation)
	}
}

func TestWaitForExportReadyIgnoresExportWithoutStatus(t *testing.T) {
	export := &exportv1.VirtualMachineExport{
		ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"},
	}
	readyExport := export.DeepCopy()
	readyExport.Status = &exportv1.VirtualMachineExportStatus{Phase: exportv1.Ready}

	kubevirtClient := kubevirtfake.NewSimpleClientset()
	watcher := watch.NewFake()
	kubevirtClient.PrependWatchReactor("virtualmachineexports", k8stesting.DefaultWatchReactor(watcher, nil))
	go func() {
		watcher.Add(export)
		watcher.Modify(readyExport)
	}()

	step := &StepExportVM{Clients: &k8s.Clients{Kubevirt: kubevirtClient}, VmExportTimeOut: 5 * time.Second}
	err := step.waitForExportReady(packersdk.TestUi(t), export)
	if err != nil {
		t.Fatalf("expected the Virtual Machine Export to be ready, got: %v", err)
	}
}

func TestWaitForExportReadyClosedWatch(t *testing.T) {
	export := &exportv1.VirtualMachineExport{
		ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"},
	}

	kubevirtClient := kubevirtfake.NewSimpleClientset()
	watcher := watch.NewFake()
	kubevirtClient.PrependWatchReactor("virtualmachineexports", k8stesting.DefaultWatchReactor(watcher, nil))
	watcher.Stop()

	step := &StepExportVM{Clients: &k8s.Clients{Kubevirt: kubevirtClient}, VmExportTimeOut: 5 * time.Second}
	err := step.waitForExportReady(packersdk.TestUi(t), export)
	if err == nil {
		t.Fatal("expected an error when the watch is closed before the Virtual Machine Export is ready")
	}
}

func TestExportIsCreatedAndWatchedWithV1(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"},
	}
	kubevirtClient := kubevirtfake.NewSimpleClientset()
	watcher := watch.NewFakeWithChanSize(1, false)
	kubevirtClient.PrependWatchReactor("virtualmachineexports", k8stesting.DefaultWatchReactor(watcher, nil))
	watcher.Modify(&exportv1.VirtualMachineExport{
		Status: &exportv1.VirtualMachineExportStatus{Phase: exportv1.Ready},
	})

	step := &StepExportVM{Clients: &k8s.Clients{Kubevirt: kubevirtClient}, VmExportTimeOut: 5 * time.Second}
	export, err := step.createExport(vm)
	if err != nil {
		t.Fatalf("expected the Virtual Machine Export to be created, got: %v", err)
	}
	err = step.waitForExportReady(packersdk.TestUi(t), export)
	if err != nil {
		t.Fatalf("expected the Virtual Machine Export to be ready, got: %v", err)
	}

	actions := kubevirtClient.Actions()
	if len(actions) != 2 {
		t.Fatalf("expected a creation and a watch, got: %v", actions)
	}
	expectedResource := schema.GroupVersionResource{Group: "export.kubevirt.io", Version: "v1", Resource: "virtualmachineexports"}
	for _, action := range actions {
		if action.GetResource() != expectedResource {
			t.Errorf("expected '%s' on '%s', got it on: '%s'", action.GetVerb(), expectedResource, action.GetResource())
		}
	}
}

func TestCreateExportExplainsApiVersionNotServed(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"},
	}
	exports := schema.GroupResource{Group: "export.kubevirt.io", Resource: "virtualmachineexports"}
	notServed := k8serrors.NewGenericServerResponse(http.StatusNotFound, "post", exports, "", "404 page not found", 0, true)
	kubevirtClient := kubevirtfake.NewSimpleClientset()
	kubevirtClient.PrependReactor("create", "virtualmachineexports", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, notServed
	})

	step := &StepExportVM{Clients: &k8s.Clients{Kubevirt: kubevirtClient}}
	_, err := step.createExport(vm)

	if !errors.Is(err, notServed) {
		t.Fatalf("expected the error of the server, got: %v", err)
	}
	for _, expected := range []string{"'export.kubevirt.io/v1'", "KubeVirt 1.9 or later"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("expected error to contain %q, got: %v", expected, err)
		}
	}
}

func TestCreateExportLeavesOtherErrorsAsTheyAre(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"},
	}
	kubevirtClient := kubevirtfake.NewSimpleClientset(&exportv1.VirtualMachineExport{ObjectMeta: vm.ObjectMeta})

	step := &StepExportVM{Clients: &k8s.Clients{Kubevirt: kubevirtClient}}
	_, err := step.createExport(vm)

	if !k8serrors.IsAlreadyExists(err) || strings.Contains(err.Error(), "KubeVirt") {
		t.Errorf("expected the error of the server only, got: %v", err)
	}
}
