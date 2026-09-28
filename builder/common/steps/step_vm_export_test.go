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
	"k8s.io/utils/ptr"
	kubevirtv1 "kubevirt.io/api/core/v1"
	exportv1 "kubevirt.io/api/export/v1"
	kubevirtfake "kubevirt.io/client-go/kubevirt/fake"
	"net/http"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStepExportVMOnlyExports(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"},
		Spec:       kubevirtv1.VirtualMachineSpec{Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{}},
		Status:     kubevirtv1.VirtualMachineStatus{PrintableStatus: kubevirtv1.VirtualMachineStatusStopped},
	}
	kubevirtClient := kubevirtfake.NewSimpleClientset(vm)
	watcher := watch.NewFakeWithChanSize(1, false)
	kubevirtClient.PrependWatchReactor("virtualmachineexports", k8stesting.DefaultWatchReactor(watcher, nil))
	watcher.Modify(&exportv1.VirtualMachineExport{
		Status: &exportv1.VirtualMachineExportStatus{Phase: exportv1.Ready},
	})
	kubeClient := k8sfake.NewSimpleClientset()

	appContext := &common.AppContext{State: new(multistep.BasicStateBag)}
	appContext.Put(common.PackerUi, packersdk.TestUi(t))
	appContext.Put(common.VirtualMachine, vm)

	step := &StepExportVM{Clients: &k8s.Clients{Kubernetes: kubeClient, Kubevirt: kubevirtClient}, VmExportTimeOut: 5 * time.Second}
	if action := step.Run(context.Background(), appContext.State); action != multistep.ActionContinue {
		t.Errorf("expected the step to continue, got action: %v, error: %v", action, appContext.GetPackerError())
	}

	for _, action := range kubevirtClient.Actions() {
		if action.Matches("put", "virtualmachines/stop") {
			t.Errorf("expected the Virtual Machine to be left as it is, got: %v", action)
		}
	}
	for _, action := range kubeClient.Actions() {
		if action.Matches("create", "jobs") {
			t.Errorf("expected no job to be created")
		}
	}
	if appContext.GetVirtualMachineExport() == nil {
		t.Errorf("expected the Virtual Machine Export to be created")
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

func TestStepExportVMHandsTheVirtualMachineToItsExport(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"},
	}
	kubevirtClient := kubevirtfake.NewSimpleClientset(vm)
	kubevirtClient.PrependReactor("create", "virtualmachineexports", func(action k8stesting.Action) (bool, runtime.Object, error) {
		action.(k8stesting.CreateAction).GetObject().(*exportv1.VirtualMachineExport).UID = "export-uid"
		return false, nil, nil
	})
	watcher := watch.NewFakeWithChanSize(1, false)
	kubevirtClient.PrependWatchReactor("virtualmachineexports", k8stesting.DefaultWatchReactor(watcher, nil))
	watcher.Modify(&exportv1.VirtualMachineExport{
		Status: &exportv1.VirtualMachineExportStatus{Phase: exportv1.Ready},
	})

	appContext := &common.AppContext{State: new(multistep.BasicStateBag)}
	appContext.Put(common.PackerUi, packersdk.TestUi(t))
	appContext.Put(common.VirtualMachine, vm)

	step := &StepExportVM{Clients: &k8s.Clients{Kubernetes: k8sfake.NewSimpleClientset(), Kubevirt: kubevirtClient}, VmExportTimeOut: 5 * time.Second}
	if action := step.Run(context.Background(), appContext.State); action != multistep.ActionContinue {
		t.Fatalf("expected the step to continue, got action: %v, error: %v", action, appContext.GetPackerError())
	}

	updatedVm, err := kubevirtClient.KubevirtV1().VirtualMachines(vm.Namespace).Get(context.Background(), vm.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get the Virtual Machine: %v", err)
	}
	// not a controller and not blocking: deleting the export removes the Virtual Machine, then its disk
	expected := []metav1.OwnerReference{{
		APIVersion:         "export.kubevirt.io/v1",
		Kind:               "VirtualMachineExport",
		Name:               "base-ubuntu",
		UID:                "export-uid",
		Controller:         ptr.To(false),
		BlockOwnerDeletion: ptr.To(false),
	}}
	if !reflect.DeepEqual(updatedVm.OwnerReferences, expected) {
		t.Errorf("expected the Virtual Machine to be owned by its export, got: %+v", updatedVm.OwnerReferences)
	}
	if !appContext.IsVirtualMachineOwnedByExport() {
		t.Errorf("expected the hand over to be recorded")
	}
}

func TestStepExportVMCleanup(t *testing.T) {
	for name, test := range map[string]struct {
		failure    string
		keepExport bool
	}{
		"successful build": {failure: "", keepExport: true},
		"halted build":     {failure: multistep.StateHalted, keepExport: false},
		"cancelled build":  {failure: multistep.StateCancelled, keepExport: false},
	} {
		t.Run(name, func(t *testing.T) {
			export := &exportv1.VirtualMachineExport{ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"}}
			kubevirtClient := kubevirtfake.NewSimpleClientset(export)
			appContext := &common.AppContext{State: new(multistep.BasicStateBag)}
			appContext.Put(common.PackerUi, packersdk.TestUi(t))
			appContext.Put(common.VirtualMachineExport, export)
			if test.failure != "" {
				appContext.State.Put(test.failure, true)
			}

			step := &StepExportVM{Clients: &k8s.Clients{Kubevirt: kubevirtClient}}
			step.Cleanup(appContext.State)

			_, err := kubevirtClient.ExportV1().VirtualMachineExports(export.Namespace).Get(context.Background(), export.Name, metav1.GetOptions{})
			if kept := err == nil; kept != test.keepExport {
				t.Errorf("expected the export to be kept: %t, got: %t (%v)", test.keepExport, kept, err)
			}
		})
	}
}
