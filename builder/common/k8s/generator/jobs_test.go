package generator

import (
	"testing"
)

// Test used as an entrypoint to validate guestfs container
func TestJob(t *testing.T) {
	//client, _ := k8s.GetKubevirtClient()
	//vm := kubevirtv1.VirtualMachine{
	//	ObjectMeta: metav1.ObjectMeta{
	//		Name:      "test-vm",
	//		Namespace: "packer",
	//	},
	//}
	//
	//job := GenerateGuestFSJob(&vm, vm.Name)
	//job, err := client.BatchV1().Jobs(vm.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
	//assert.NoError(t, err)
	//err = k8s.WaitForJobCompletion(client.BatchV1(), new(packersdk.MockUi), job, 30*time.Second)
	//assert.NoError(t, err)
}
