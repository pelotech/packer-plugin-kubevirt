  Include a short description about the builder. This is a good place
  to call out what the builder does, and any requirements for the given
  builder environment. See https://www.packer.io/docs/builder/null
-->

The ISO builder is mostly used to create base VM images, an ISO of your choice will be the starting point.

<!-- Builder Configuration Fields -->

**Required fields**

- `kubernetes_name` (string) - Kubernetes resource name used for VM to be provisioned or as prefix for all resources enabling the process

- `kubernetes_namespace` (string) - Kubernetes namespace used to provision and export virtual machines

- `kubernetes_node_selectors` ([string]) - Kubernetes node selectors targeting the node where resources should be created

- `kubernetes_tolerations` (map[string]string) - Kubernetes tolerations resources should support to get eligible to the desired node

- `source_url` (string) - Kubernetes tolerations resources should support to get eligible to the desired node

- `kubevirt_os_preference` (string) - KubeVirt VM preference to apply to the VM. List of preferences available [here](https://github.com/kubevirt/common-instancetypes/tree/main/preferences)

- `vm_disk_space` (string) - KubeVirt VM disk space required to install the OS and its packages

<!--
  Optional Configuration Fields

  Configuration options that are not required or have reasonable defaults
  should be listed under the optionals section. Defaults values should be
  noted in the description of the field
-->

**Optional fields**

- `vm_linux_cloud_init` (string) - Cloud-init file content to inject into the VM at first boot.
Defaults to a default cloud-init file available in the source code

- `vm_deployment_timeout` (string) - Time out duration for VM to get its OS installed (including cloud-init or sysprep)
Defaults to '10m'

- `vm_deployment_timeout` (string) - Time out duration for VM export server to be up and ready for download
Defaults to '5m'

- `source_aws_access_key_id` (string) - AWS Access Key ID for S3 bucket containing VM images
Sensitive field - Defaults to empty string (will skip adding credentials)

- `source_aws_secret_access_key` (string) - AWS Secret Access Key for S3 bucket containing VM images
Sensitive field - Defaults to empty string (will skip adding credentials)

**Communicator configuration fields**

- `communicator` (string) - Packer communicator type
Accepted values: `ssh`, `winrm` - Defaults to `ssh`

- `ssh_port` (string) - SSH port
Accepted value: `>=1024` - Defaults to `2222`

- `winrm_port` (string) - WinRM port
Accepted value: `>=1024` - Defaults to `5389`

- `winrm_use_ssl` (string) - Use HTTPS for WinRM
Defaults to `false`

- `winrm_insecure` (string) - Skip server certificate chain and host name check
Defaults to `false`

- `winrm_timeout` (string) - WinRM connection timeout
Defaults to `30s`

<!--
  A basic example on the usage of the builder. Multiple examples
  can be provided to highlight various build configurations.

-->
### Example Usage

#### Linux
```hcl
source "kubevirt-iso" "ubuntu" {
  kubernetes_name             = "ubuntu"
  kubernetes_namespace        = "default"
  kubernetes_node_selectors   = {
    "kubevirt.io/schedulable" = "true"
  }
  kubernetes_tolerations      = [
    {
      key      = "pelo.tech/kvm"
      operator = "Equal"
      value    = "true"
      effect   = "NoSchedule"
    }
  ]
  source_url                  = "https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img"
  kubevirt_os_preference      = "ubuntu"
  vm_disk_space               = "10Gi"
  # Optional fields
  vm_linux_cloud_init          = file("/path/to/cloud-init.yaml") # default to generic cloud-init file
  vm_deployment_timeout        = "15m"                            # default to '10m'
  vm_export_timeout            = "10m"                            # default to '5m'
  source_aws_access_key_id     = var.source_aws_access_key_id     # default to ""
  source_aws_secret_access_key = var.source_aws_secret_access_key # default to ""

  communicator                 = "ssh"                            # default to 'ssh'
  ssh_port                     = 2222                             # default to 2222
}

 build {
   sources = ["source.kubevirt-iso.ubuntu"]
 }
```

#### Windows
```hcl
source "kubevirt-iso" "windows" {
}
```
