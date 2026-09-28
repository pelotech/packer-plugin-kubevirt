package kubevirt

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/communicator/sshkey"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	gossh "golang.org/x/crypto/ssh"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	stepDef "packer-plugin-kubevirt/builder/common/steps"
	"packer-plugin-kubevirt/builder/common/vm"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPrepareRejectsInvalidResources(t *testing.T) {
	for _, field := range []string{"vm_cpu", "vm_memory"} {
		builder := new(Builder)
		_, _, err := builder.Prepare(map[string]interface{}{
			"vm_name":      "base-ubuntu",
			"vm_disk_size": "10Gi",
			field:          "plenty",
		})
		if err == nil || !strings.Contains(err.Error(), "invalid '"+field+"' value 'plenty'") {
			t.Errorf("expected an invalid '%s' error, got: %v", field, err)
		}
	}
}

func TestPrepareRejectsNegativeExportTTL(t *testing.T) {
	builder := new(Builder)
	_, _, err := builder.Prepare(map[string]interface{}{
		"vm_name":       "base-ubuntu",
		"vm_disk_size":  "10Gi",
		"vm_export_ttl": "-1h",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid 'vm_export_ttl' value '-1h0m0s'") {
		t.Errorf("expected an invalid 'vm_export_ttl' error, got: %v", err)
	}
}

// useTestCluster points the Kubernetes client at a server that only answers the version request
func useTestCluster(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_, _ = response.Write([]byte(`{"major": "1", "minor": "33", "gitVersion": "v1.33.0"}`))
	}))
	t.Cleanup(server.Close)

	config := clientcmdapi.NewConfig()
	config.Clusters["test"] = &clientcmdapi.Cluster{Server: server.URL}
	config.Contexts["test"] = &clientcmdapi.Context{Cluster: "test"}
	config.CurrentContext = "test"
	kubeconfig := filepath.Join(t.TempDir(), "kubeconfig")
	if err := clientcmd.WriteToFile(*config, kubeconfig); err != nil {
		t.Fatalf("failed to write the kubeconfig: %v", err)
	}
	t.Setenv(clientcmd.RecommendedConfigPathEnvVar, kubeconfig)
}

func TestPrepareChecksRequiredSettings(t *testing.T) {
	useTestCluster(t)

	tests := map[string]struct {
		setting  string
		value    string
		expected string
	}{
		"example settings":              {},
		"longest vm_name":               {"vm_name", strings.Repeat("a", 52), ""},
		"missing kubernetes_namespace":  {"kubernetes_namespace", "", "'kubernetes_namespace' is required"},
		"invalid kubernetes_namespace":  {"kubernetes_namespace", "packer_linux", "invalid 'kubernetes_namespace' value 'packer_linux'"},
		"missing source_url":            {"source_url", "", "'source_url' is required"},
		"missing vm_preference":         {"vm_preference", "", "'vm_preference' is required"},
		"missing vm_disk_size":          {"vm_disk_size", "", "'vm_disk_size' is required"},
		"invalid vm_disk_size":          {"vm_disk_size", "plenty", "invalid 'vm_disk_size' value 'plenty'"},
		"invalid vm_install_media_size": {"vm_install_media_size", "plenty", "invalid 'vm_install_media_size' value 'plenty'"},
		"missing vm_name":               {"vm_name", "", "'vm_name' is required"},
		"too long vm_name":              {"vm_name", strings.Repeat("a", 53), "must be no more than 52 characters"},
		"vm_name with a dot":            {"vm_name", "base.ubuntu", "invalid 'vm_name' value 'base.ubuntu'"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			settings := map[string]interface{}{
				"kubernetes_namespace": "packer-linux",
				"source_url":           "https://cloud-images.ubuntu.com/minimal/releases/resolute/release/ubuntu-26.04-minimal-cloudimg-amd64.img",
				"vm_disk_size":         "4Gi",
				"vm_name":              "base-ubuntu-2604",
				"vm_preference":        "ubuntu",
			}
			if test.setting != "" {
				settings[test.setting] = test.value
			}

			_, _, err := new(Builder).Prepare(settings)
			if test.expected == "" && err != nil {
				t.Errorf("expected the settings to be valid, got: %v", err)
			}
			if test.expected != "" && (err == nil || !strings.Contains(err.Error(), test.expected)) {
				t.Errorf("expected an error containing %q, got: %v", test.expected, err)
			}
		})
	}
}

func TestPrepareChecksTolerations(t *testing.T) {
	useTestCluster(t)
	exampleToleration := map[string]string{"key": "pelo.tech/kvm", "operator": "Equal", "value": "true", "effect": "NoSchedule"}

	tests := map[string]struct {
		toleration map[string]string
		expected   string
	}{
		"example toleration":             {exampleToleration, ""},
		"misspelled field":               {map[string]string{"key": "pelo.tech/kvm", "operator": "Exists", "efect": "NoSchedule"}, "invalid 'kubernetes_tolerations[1]' field 'efect'"},
		"tolerationSeconds not a number": {map[string]string{"operator": "Exists", "effect": "NoExecute", "tolerationSeconds": "abc"}, "invalid 'kubernetes_tolerations[1].tolerationSeconds' value 'abc'"},
		"unknown operator":               {map[string]string{"key": "pelo.tech/kvm", "operator": "Equals", "value": "true"}, "invalid 'kubernetes_tolerations[1].operator' value 'Equals'"},
		"unknown effect":                 {map[string]string{"key": "pelo.tech/kvm", "operator": "Exists", "effect": "NoScheduled"}, "invalid 'kubernetes_tolerations[1].effect' value 'NoScheduled'"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			builder := new(Builder)
			_, _, err := builder.Prepare(map[string]interface{}{
				"kubernetes_namespace":   "packer-linux",
				"kubernetes_tolerations": []map[string]string{exampleToleration, test.toleration},
				"source_url":             "https://cloud-images.ubuntu.com/minimal/releases/resolute/release/ubuntu-26.04-minimal-cloudimg-amd64.img",
				"vm_disk_size":           "4Gi",
				"vm_name":                "base-ubuntu-2604",
				"vm_preference":          "ubuntu",
			})
			if test.expected != "" {
				if err == nil || !strings.Contains(err.Error(), test.expected) {
					t.Errorf("expected an error containing %q, got: %v", test.expected, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected the settings to be valid, got: %v", err)
			}

			var deploy *stepDef.StepDeployVM
			for _, step := range builder.steps() {
				if found, ok := step.(*stepDef.StepDeployVM); ok {
					deploy = found
				}
			}
			if deploy == nil {
				t.Fatal("expected a deploy step")
			}
			example := v1.Toleration{Key: "pelo.tech/kvm", Operator: v1.TolerationOpEqual, Value: "true", Effect: v1.TaintEffectNoSchedule}
			if expected := []v1.Toleration{example, example}; !reflect.DeepEqual(deploy.VmOptions.Tolerations, expected) {
				t.Errorf("expected the tolerations %+v, got: %+v", expected, deploy.VmOptions.Tolerations)
			}
		})
	}
}

func TestDeployStepReceivesTheInstallMediaSize(t *testing.T) {
	useTestCluster(t)

	for name, test := range map[string]struct {
		installMediaSize string
		expected         string
	}{
		"default": {installMediaSize: "", expected: "8Gi"},
		"set":     {installMediaSize: "6Gi", expected: "6Gi"},
	} {
		t.Run(name, func(t *testing.T) {
			settings := map[string]interface{}{
				"kubernetes_namespace": "packer-windows",
				"source_url":           "https://example.com/windows-11.iso",
				"vm_disk_size":         "64Gi",
				"vm_name":              "base-windows-11",
				"vm_preference":        "windows.11.virtio",
			}
			if test.installMediaSize != "" {
				settings["vm_install_media_size"] = test.installMediaSize
			}
			builder := new(Builder)
			if _, _, err := builder.Prepare(settings); err != nil {
				t.Fatalf("expected the settings to be valid, got: %v", err)
			}

			var deploy *stepDef.StepDeployVM
			for _, step := range builder.steps() {
				if found, ok := step.(*stepDef.StepDeployVM); ok {
					deploy = found
				}
			}
			if deploy == nil {
				t.Fatal("expected a deploy step")
			}
			if deploy.VmOptions.InstallMediaSize != test.expected || deploy.VmOptions.DiskSize != "64Gi" {
				t.Errorf("expected an install media of %s and a disk of 64Gi, got: %s and %s", test.expected, deploy.VmOptions.InstallMediaSize, deploy.VmOptions.DiskSize)
			}
		})
	}
}

func TestDecodeTolerations(t *testing.T) {
	tolerations, err := decodeTolerations([]map[string]string{
		{"key": "pelo.tech/kvm", "operator": "Equal", "value": "true", "effect": "NoSchedule"},
		{"operator": "Exists", "effect": "NoExecute", "tolerationSeconds": "300"},
	})
	if err != nil {
		t.Fatalf("expected the tolerations to be valid, got: %v", err)
	}

	seconds := int64(300)
	expected := []v1.Toleration{
		{Key: "pelo.tech/kvm", Operator: v1.TolerationOpEqual, Value: "true", Effect: v1.TaintEffectNoSchedule},
		{Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoExecute, TolerationSeconds: &seconds},
	}
	if !reflect.DeepEqual(tolerations, expected) {
		t.Errorf("expected the tolerations %+v, got: %+v", expected, tolerations)
	}
}

type login struct {
	user   string
	secret string
}

func startSSHServer(t *testing.T) (int, chan login) {
	_, hostKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate the host key: %v", err)
	}
	hostSigner, err := gossh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatalf("failed to read the host key: %v", err)
	}

	logins := make(chan login, 10)
	config := &gossh.ServerConfig{
		PasswordCallback: func(conn gossh.ConnMetadata, password []byte) (*gossh.Permissions, error) {
			logins <- login{conn.User(), string(password)}
			return nil, nil
		},
		PublicKeyCallback: func(conn gossh.ConnMetadata, key gossh.PublicKey) (*gossh.Permissions, error) {
			logins <- login{conn.User(), string(gossh.MarshalAuthorizedKey(key))}
			return nil, nil
		},
	}
	config.AddHostKey(hostSigner)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				_, channels, requests, err := gossh.NewServerConn(conn, config)
				if err != nil {
					return
				}
				go gossh.DiscardRequests(requests)
				for channel := range channels {
					_ = channel.Reject(gossh.Prohibited, "the test server only checks the login")
				}
			}()
		}
	}()

	return listener.Addr().(*net.TCPAddr).Port, logins
}

func TestCommunicatorReceivesSSHSettings(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	keyPair, err := sshkey.GeneratePair(sshkey.ED25519, nil, 0)
	if err != nil {
		t.Fatalf("failed to generate a key pair: %v", err)
	}
	keyFile := filepath.Join(t.TempDir(), "id_ed25519")
	if err = os.WriteFile(keyFile, keyPair.Private, 0600); err != nil {
		t.Fatalf("failed to write the private key: %v", err)
	}

	tests := map[string]struct {
		settings communicator.SSH
		expected login
	}{
		"defaults": {
			expected: login{"packer", "packer"},
		},
		"user name and password": {
			settings: communicator.SSH{SSHUsername: "ubuntu", SSHPassword: "secret"},
			expected: login{"ubuntu", "secret"},
		},
		"private key": {
			settings: communicator.SSH{SSHUsername: "ubuntu", SSHPrivateKeyFile: keyFile},
			expected: login{"ubuntu", string(keyPair.Public)},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			port, logins := startSSHServer(t)
			comm := communicator.Config{Type: "ssh", SSH: test.settings}
			comm.SSHPort = port
			if _, err := prepareCommunicator(&comm); err != nil {
				t.Fatalf("expected the communicator settings to be valid, got: %v", err)
			}

			state := new(multistep.BasicStateBag)
			state.Put("ui", packersdk.TestUi(t))
			action := connectStep(&comm).Run(context.Background(), state)

			if action != multistep.ActionContinue {
				t.Fatalf("expected the communicator to connect, got: %v", state.Get("error"))
			}
			if actual := <-logins; actual != test.expected {
				t.Errorf("expected login %v, got: %v", test.expected, actual)
			}
		})
	}
}

func TestCommunicatorReceivesWinRMSettings(t *testing.T) {
	tests := map[string]struct {
		settings communicator.WinRM
		expected login
	}{
		"defaults": {
			expected: login{"packer", "packer"},
		},
		"user name and password": {
			settings: communicator.WinRM{WinRMUser: "administrator", WinRMPassword: "secret"},
			expected: login{"administrator", "secret"},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			logins := make(chan login, 1)
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				user, password, _ := request.BasicAuth()
				select {
				case logins <- login{user, password}:
				default:
				}
				response.WriteHeader(http.StatusUnauthorized)
			}))
			defer server.Close()

			comm := communicator.Config{Type: "winrm", WinRM: test.settings}
			comm.WinRMPort = server.Listener.Addr().(*net.TCPAddr).Port
			if _, err := prepareCommunicator(&comm); err != nil {
				t.Fatalf("expected the communicator settings to be valid, got: %v", err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			state := new(multistep.BasicStateBag)
			state.Put("ui", packersdk.TestUi(t))
			stepEnded := make(chan struct{})
			go func() {
				connectStep(&comm).Run(ctx, state)
				close(stepEnded)
			}()

			select {
			case actual := <-logins:
				if actual != test.expected {
					t.Errorf("expected login %v, got: %v", test.expected, actual)
				}
			case <-time.After(5 * time.Second):
				t.Error("expected the communicator to send a login")
			}
			cancel()
			<-stepEnded
		})
	}
}

func TestPrepareCommunicatorValidatesSettings(t *testing.T) {
	tests := map[string]struct {
		comm     communicator.Config
		expected string
	}{
		"unsupported type": {
			comm:     communicator.Config{Type: "docker"},
			expected: "unsupported communicator type",
		},
		"password without user name": {
			comm:     communicator.Config{Type: "ssh", SSH: communicator.SSH{SSHPassword: "secret"}},
			expected: "ssh_username must be specified",
		},
		"missing private key": {
			comm:     communicator.Config{Type: "ssh", SSH: communicator.SSH{SSHUsername: "ubuntu", SSHPrivateKeyFile: "missing"}},
			expected: "ssh_private_key_file is invalid",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := prepareCommunicator(&test.comm)
			if err == nil || !strings.Contains(err.Error(), test.expected) {
				t.Errorf("expected an error containing %q, got: %v", test.expected, err)
			}
		})
	}
}

func TestPrepareCommunicatorLocalPort(t *testing.T) {
	tests := map[string]struct {
		comm     communicator.Config
		expected int
	}{
		"ssh port left to the port forwarding": {
			comm:     communicator.Config{Type: "ssh"},
			expected: 0,
		},
		"winrm port left to the port forwarding": {
			comm:     communicator.Config{Type: "winrm"},
			expected: 0,
		},
		"ssh port of the user": {
			comm:     communicator.Config{Type: "ssh", SSH: communicator.SSH{SSHPort: 2200}},
			expected: 2200,
		},
		"winrm port of the user": {
			comm:     communicator.Config{Type: "winrm", WinRM: communicator.WinRM{WinRMPort: 5900}},
			expected: 5900,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := prepareCommunicator(&test.comm); err != nil {
				t.Fatalf("expected the communicator settings to be valid, got: %v", err)
			}
			if actual := test.comm.Port(); actual != test.expected {
				t.Errorf("expected local port %d, got: %d", test.expected, actual)
			}
		})
	}
}

func TestPrepareCommunicatorRejectsReservedPort(t *testing.T) {
	tests := map[string]communicator.Config{
		"ssh":   {Type: "ssh", SSH: communicator.SSH{SSHPort: 1023}},
		"winrm": {Type: "winrm", WinRM: communicator.WinRM{WinRMPort: 1023}},
	}
	for name, comm := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := prepareCommunicator(&comm)
			if err == nil || !strings.Contains(err.Error(), "reserved") {
				t.Errorf("expected a reserved port error, got: %v", err)
			}
		})
	}
}

func TestPrepareCommunicatorKeepsWinRMTimeout(t *testing.T) {
	comm := communicator.Config{Type: "winrm"}
	if _, err := prepareCommunicator(&comm); err != nil {
		t.Fatalf("expected the communicator settings to be valid, got: %v", err)
	}
	if comm.WinRMTimeout != 30*time.Second {
		t.Errorf("expected a WinRM timeout of 30s, got: %s", comm.WinRMTimeout)
	}
}

func TestStepsReceiveTheGeneralizeSettings(t *testing.T) {
	for name, test := range map[string]struct {
		preference      string
		skipVirtSysprep bool
		osFamily        vm.OsFamily
	}{
		"linux":                      {preference: "ubuntu", osFamily: vm.Linux},
		"linux without virt-sysprep": {preference: "ubuntu", skipVirtSysprep: true, osFamily: vm.Linux},
		"windows":                    {preference: "windows.11.virtio", osFamily: vm.Windows},
	} {
		t.Run(name, func(t *testing.T) {
			builder := &Builder{config: Config{
				Comm:                          communicator.Config{SSH: communicator.SSH{SSHUsername: "ubuntu"}},
				VirtualMachinePreference:      test.preference,
				VirtualMachineSkipVirtSysprep: test.skipVirtSysprep,
				VirtualMachineExportTimeOut:   time.Minute,
			}}

			var generalize *stepDef.StepGeneralize
			for _, step := range builder.steps() {
				if found, ok := step.(*stepDef.StepGeneralize); ok {
					generalize = found
				}
			}
			if generalize == nil {
				t.Fatal("expected a generalize step")
			}
			if generalize.OsFamily != test.osFamily || generalize.SkipVirtSysprep != test.skipVirtSysprep || generalize.UserToKeep != "ubuntu" || generalize.VmExportTimeOut != time.Minute {
				t.Errorf("expected OS family %d, skip virt-sysprep %t, the user 'ubuntu' kept and a 1m timeout, got: %+v", test.osFamily, test.skipVirtSysprep, generalize)
			}
		})
	}
}

func TestRunReturnsAnErrorWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	artifact, err := new(Builder).Run(ctx, packersdk.TestUi(t), &packersdk.MockHook{})
	if artifact != nil || err == nil {
		t.Fatalf("expected no artifact and an error for a cancelled build, got: %v, %v", artifact, err)
	}
}
