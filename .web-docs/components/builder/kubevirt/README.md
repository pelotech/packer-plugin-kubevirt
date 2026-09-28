Type: `kubevirt`

<!--
  Include a short description about the builder. This is a good place
  to call out what the builder does, and any requirements for the given
  builder environment. See https://www.packer.io/docs/builder/null
-->

The builder is mostly used to create base VM images, an ISO or a cloud image of your choice will be the starting point.

The builder runs against the Kubernetes cluster of your current kube context, with KubeVirt 1.9 or later and CDI installed.
Once provisioned, the Virtual Machine is stopped, Linux disks are generalized with `virt-sysprep`
(running as a Kubernetes job in the cluster, nothing to install locally) and the disk is exposed through a Virtual Machine Export.
The export owns the stopped Virtual Machine: the disk stays as long as the export, and deleting the export removes the Virtual Machine and its disk.
Windows is generalized by your shutdown command, see [Windows](#windows). `vm_skip_virt_sysprep` skips `virt-sysprep` for Linux.
`virt-sysprep` removes the bash history, the machine ID and the user accounts, but keeps the SSH user of the build (`ssh_username`), root and the system accounts.

<!-- Builder Configuration Fields -->

**Required fields**

- `kubernetes_namespace` (string) - Kubernetes namespace used to provision and export virtual machines, created when it is missing, see [Permissions](#permissions).
A DNS-1123 label: at most 63 characters, lowercase letters, digits and `-`, starting and ending with a letter or a digit

- `source_url` (string) - URL of the ISO or cloud image used as the starting point. It is read from inside the cluster, over HTTP or HTTPS.
With `source_aws_access_key_id` and `source_aws_secret_access_key` it is read from S3, and has to be `https://s3.<region>.amazonaws.com/<bucket>/<key>`

- `vm_disk_size` (string) - KubeVirt VM disk size required to install the OS and its packages (e.g. `10Gi`)

- `vm_name` (string) - Name of the Virtual Machine the build creates in the cluster. Its disks, secrets, jobs and Virtual Machine Export are named after it.
The post-processors also name the S3 object and the DataSource after it, unless `s3_object_name` or `datasource_name` is set.
A DNS-1123 label of at most 52 characters: lowercase letters, digits and `-`, starting and ending with a letter or a digit.
The `virt-sysprep` job is named `<vm_name>-libguestfs`, and Kubernetes copies that name into a label of its pod, limited to 63 characters.
Dots are refused: KubeVirt cuts the name at its first dot in the label of the pod the port forwarding looks for.
Builds running at the same time in one namespace need different names, see [Several builds of one template](#several-builds-of-one-template)

- `vm_preference` (string) - KubeVirt VM preference to apply to the VM. List of preferences available [here](https://github.com/kubevirt/common-instancetypes/tree/main/preferences).
A preference containing `windows` selects the Windows installation flow, any other value selects the Linux one

<!--
  Optional Configuration Fields

  Configuration options that are not required or have reasonable defaults
  should be listed under the optionals section. Defaults values should be
  noted in the description of the field
-->

**Optional fields**

- `kubernetes_node_selector` (map[string]string) - Kubernetes node selector targeting the node where resources should be created
Defaults to no node selector

- `kubernetes_tolerations` ([]map[string]string) - Kubernetes tolerations resources should support to get eligible to the desired node
Each toleration takes the fields of a Kubernetes toleration: `key`, `operator`, `value`, `effect` and `tolerationSeconds`
Defaults to no toleration

- `source_aws_access_key_id` (string) - AWS Access Key ID for S3 bucket containing VM images. Keys of an IAM user: temporary credentials are not supported, use a presigned URL as `source_url` instead
Sensitive field - Defaults to empty string (will skip adding credentials)

- `source_aws_secret_access_key` (string) - AWS Secret Access Key for S3 bucket containing VM images
Sensitive field - Defaults to empty string (will skip adding credentials)

- `vm_autounattend` (string) - Answer file content (`autounattend.xml`) used by Windows Setup to install Windows.
Defaults to a default answer file available in the source code

- `vm_cloud_init` (string) - Cloud-init file content to inject into the Linux VM at first boot.
Defaults to a default cloud-init file available in the source code

- `vm_cpu` (string) - CPUs requested by the VM
Defaults to `4`

- `vm_export_timeout` (duration string) - Time out duration for each stage of the export: VM stop when there is no `shutdown_command` (`shutdown_timeout` applies otherwise), `virt-sysprep` job (Linux only) and export server to be up and ready for download
Defaults to `5m`

- `vm_export_ttl` (duration string) - Lifetime of the Virtual Machine Export, from its creation. When it expires, KubeVirt deletes the export, and with it the stopped VM and its disk.
The post-processors download from the export: raise it for several post-processors, big disks or slow uploads
Defaults to `2h`, the KubeVirt default

- `vm_install_timeout` (duration string) - Time out duration for VM to get its OS installed and its guest agent answering (including cloud-init or Windows Setup).
With a `boot_command`, it also limits the wait for the VM to run and the typing on the VNC console, then the wait for the OS gets the full duration again
Defaults to `10m`

- `vm_memory` (string) - Memory requested by the VM
Defaults to `8Gi`

- `vm_skip_virt_sysprep` (bool) - Skip the `virt-sysprep` job on Linux disks, the VM is still stopped before the export. It changes nothing for Windows, generalized by your shutdown command
Defaults to `false`

**Boot command fields**

Keys typed in the console of the VM once it runs, before the builder waits for the OS to be installed.
They follow the [boot command](https://developer.hashicorp.com/packer/docs/templates/legacy_json_templates/communicator#boot-command) syntax of Packer, such as `<enter>`, `<up>` or `<wait5s>`.

- `boot_command` ([]string) - Keys to type
Defaults to no key, the console is left alone

- `boot_key_interval` (duration string) - Time to wait between two keys
Defaults to `100ms`

- `boot_keygroup_interval` (duration string) - Time to wait between two items of `boot_command`
Defaults to no wait

- `boot_wait` (duration string) - Time to wait after the VM runs, before the first key
Defaults to `10s`

**Shutdown fields**

The builder stops the VM once the provisioners are done. A shutdown command lets the guest do it, when it has work to do on its way down.
The VM runs once: a guest that shuts down is not started again.

- `shutdown_command` (string) - Command run in the VM after the provisioners, that ends with the shutdown of the guest
Defaults to no command, the builder stops the VM

- `shutdown_timeout` (duration string) - Time to wait for the VM to be stopped after the shutdown command
Defaults to `5m`

**Communicator configuration fields**

The builder connects to the VM through a local port forwarded to it, so `ssh_host` and `winrm_host` are ignored.
The other settings of the Packer [SSH](https://developer.hashicorp.com/packer/docs/communicators/ssh) and
[WinRM](https://developer.hashicorp.com/packer/docs/communicators/winrm) communicators apply, with their usual defaults unless listed here.

- `communicator` (string) - Packer communicator type
Accepted values: `ssh`, `winrm` - Defaults to `ssh`

- `ssh_password` (string) - Password to connect to the VM with SSH
Sensitive field - Defaults to `packer` when `ssh_username` and `ssh_password` are both unset

- `ssh_port` (int) - Local port forwarded to the VM SSH port
Accepted value: `>=1024` - Defaults to a free local port

- `ssh_private_key_file` (string) - Path to the private key to connect to the VM with SSH, your cloud-init file has to authorize its public key
Defaults to no key

- `ssh_timeout` (duration string) - Time to wait for SSH to become available
Defaults to `5m`

- `ssh_username` (string) - User name to connect to the VM with SSH
Defaults to `packer`, the user of the default cloud-init file, when `ssh_username` and `ssh_password` are both unset

- `winrm_insecure` (bool) - Skip server certificate chain and host name check
Defaults to `false`

- `winrm_password` (string) - Password to connect to the VM with WinRM
Sensitive field - Defaults to `packer` when `winrm_username` and `winrm_password` are both unset

- `winrm_port` (int) - Local port forwarded to the VM WinRM port
Accepted value: `>=1024` - Defaults to a free local port

- `winrm_timeout` (duration string) - WinRM connection timeout
Defaults to `30s`

- `winrm_use_ssl` (bool) - Use HTTPS for WinRM
Defaults to `false`

- `winrm_username` (string) - User name to connect to the VM with WinRM
Defaults to `packer`, the user of the default answer file, when `winrm_username` and `winrm_password` are both unset

<!--
  A basic example on the usage of the builder. Multiple examples
  can be provided to highlight various build configurations.

-->
### Example Usage

#### Linux
```hcl
source "kubevirt" "ubuntu" {
  communicator         = "ssh" # Optional, default to 'ssh'
  kubernetes_namespace = "default"
  kubernetes_node_selector = {
    "kubevirt.io/schedulable" = "true"
  }
  kubernetes_tolerations = [
    {
      key      = "pelo.tech/kvm"
      operator = "Equal"
      value    = "true"
      effect   = "NoSchedule"
    }
  ]
  source_aws_access_key_id     = var.source_aws_access_key_id     # Optional, default to ""
  source_aws_secret_access_key = var.source_aws_secret_access_key # Optional, default to ""
  source_url                   = "https://cloud-images.ubuntu.com/minimal/releases/resolute/release/ubuntu-26.04-minimal-cloudimg-amd64.img"
  vm_cloud_init                = file("/path/to/cloud-init.yaml") # Optional, default to a generic cloud-init file
  vm_cpu                       = "2"                              # Optional, default to '4'
  vm_disk_size                 = "10Gi"
  vm_export_timeout            = "10m" # Optional, default to '5m'
  vm_install_timeout           = "15m" # Optional, default to '10m'
  vm_memory                    = "4Gi" # Optional, default to '8Gi'
  vm_name                      = "ubuntu"
  vm_preference                = "ubuntu"
}

build {
  sources = ["source.kubevirt.ubuntu"]
}
```

#### Windows
```hcl
source "kubevirt" "windows" {
  boot_command         = [for i in range(30) : "<up><wait1s>"]
  boot_wait            = "1s"
  communicator         = "winrm"
  kubernetes_namespace = "default"
  shutdown_command     = "C:\\Windows\\System32\\Sysprep\\Sysprep.exe /generalize /oobe /shutdown /quiet"
  shutdown_timeout     = "30m"
  source_url           = "https://example.com/windows-11.iso"
  vm_autounattend      = file("/path/to/autounattend.xml")
  vm_disk_size         = "64Gi"
  vm_export_timeout    = "20m"
  vm_install_timeout   = "90m"
  vm_name              = "windows"
  vm_preference        = "windows.11.virtio"
  winrm_timeout        = "10m"
}

build {
  sources = ["source.kubevirt.windows"]
}
```

The [Windows 11 example](https://github.com/pelotech/packer-plugin-kubevirt/tree/main/example/windows-11) is a complete template, with its answer files.

### Several builds of one template

Builds of one template run side by side in one namespace when each has its own `vm_name`, since the Virtual Machine and everything the build creates are named after it.
Two builds with the same `vm_name` collide: the second one fails when it creates the Virtual Machine, and leaves the first one alone.

A `dynamic "source"` block in the `build` block gives each build its own name.
`vm_name` then goes in that block only: Packer refuses a setting set both there and in the top-level `source` block.

```hcl
source "kubevirt" "ubuntu" {
  kubernetes_namespace = "packer"
  source_url           = "https://cloud-images.ubuntu.com/minimal/releases/resolute/release/ubuntu-26.04-minimal-cloudimg-amd64.img"
  vm_disk_size         = "10Gi"
  vm_preference        = "ubuntu"
}

build {
  dynamic "source" {
    for_each = ["a", "b"]
    labels   = ["kubevirt.ubuntu"]
    content {
      name    = source.value
      vm_name = "base-ubuntu-${source.value}"
    }
  }

  post-processor "kubevirt-datasource" {
    datasource_name = "base-ubuntu-${source.name}"
  }
}
```

The label is `<builder type>.<source name>`. Inside `content`, `source.value` is the item of `for_each`. Elsewhere in the build, `source.name` is the `name` of the build.

- Leave `ssh_port` and `winrm_port` unset, so each build forwards its own free local port.
- Give each build its own destination, or the builds overwrite each other. `datasource_name` and `s3_object_name` default to `vm_name`: when you set them, make them differ per build, as above.
The OCI `image` needs a tag per build, such as `ghcr.io/pelotech/base-ubuntu:${source.name}`.

### Permissions

The plugin calls the Kubernetes API as the user of your kube context, or as the service account of its pod.
A build needs this Role in `kubernetes_namespace`, and in `datasource_namespace` when it is another namespace.
Bind it to that user or service account with a RoleBinding in each namespace.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: packer-plugin-kubevirt
  namespace: packer
rules:
  - apiGroups: ["kubevirt.io"]
    resources: ["virtualmachines"]
    verbs: ["create", "get", "list", "watch", "patch", "delete"]
  - apiGroups: ["kubevirt.io"]
    resources: ["virtualmachineinstances"]
    verbs: ["get"]
  - apiGroups: ["subresources.kubevirt.io"]
    resources: ["virtualmachines/stop"]
    verbs: ["update"]
  # boot_command only
  - apiGroups: ["subresources.kubevirt.io"]
    resources: ["virtualmachineinstances/vnc"]
    verbs: ["get"]
  - apiGroups: ["export.kubevirt.io"]
    resources: ["virtualmachineexports"]
    verbs: ["create", "get", "list", "watch", "delete"]
  - apiGroups: ["batch"]
    resources: ["jobs"]
    verbs: ["create", "list", "watch"]
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["create", "delete"]
  # list finds the pod of the VM, get and list describe failed pods
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list"]
  - apiGroups: [""]
    resources: ["pods/portforward"]
    verbs: ["create"]
  - apiGroups: [""]
    resources: ["pods/log"]
    verbs: ["get"]
  # kubevirt-datasource only
  - apiGroups: ["cdi.kubevirt.io"]
    resources: ["datavolumes"]
    verbs: ["create", "get", "list", "watch", "delete"]
  - apiGroups: ["cdi.kubevirt.io"]
    resources: ["datasources"]
    verbs: ["create", "get", "update"]
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["create", "delete"]
  - apiGroups: [""]
    resources: ["persistentvolumeclaims"]
    verbs: ["get"]
  # only with the OwnerReferencesPermissionEnforcement admission plugin, as on OpenShift
  - apiGroups: ["kubevirt.io"]
    resources: ["virtualmachines/finalizers"]
    verbs: ["update"]
  - apiGroups: ["export.kubevirt.io"]
    resources: ["virtualmachineexports/finalizers"]
    verbs: ["update"]
  - apiGroups: ["batch"]
    resources: ["jobs/finalizers"]
    verbs: ["update"]
  - apiGroups: ["cdi.kubevirt.io"]
    resources: ["datavolumes/finalizers"]
    verbs: ["update"]
```

To let the build create `kubernetes_namespace` when it does not exist yet, also grant `create` on `namespaces` with a ClusterRole and a ClusterRoleBinding.
Without it, the namespace has to exist before the build.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: packer-plugin-kubevirt-namespaces
rules:
  - apiGroups: [""]
    resources: ["namespaces"]
    verbs: ["create"]
```

The plugin also reads the version of the cluster at `/version`, open to every user by default.

The `virt-sysprep` job and the jobs of the S3 and OCI post-processors do not call the Kubernetes API.
They run with the `default` service account, or with `service_account_name`, which needs no Role: it only carries the cloud identity of the upload, such as an IAM role for S3.

### Windows

The VM gets four drives. Your answer file has to follow them:

| Drive | Content | Attached as |
|---|---|---|
| `C:` | System disk, blank, of `vm_disk_size`. This is the disk that is exported | disk, on the bus of the preference |
| `D:` | Install ISO of `source_url` | SATA CD-ROM |
| `E:` | [virtio drivers](https://fedorapeople.org/groups/virt/virtio-win/direct-downloads/stable-virtio/) and the guest agent | SATA CD-ROM |
| `F:` | `autounattend.xml` of `vm_autounattend` | SATA CD-ROM |

The firmware, Secure Boot and the TPM come from the preference: `windows.11` and `windows.11.virtio` give UEFI with Secure Boot and a TPM.
With a `virtio` preference the system disk is a virtio disk, so the answer file has to load the `viostor` driver of `E:` during the setup.

**Boot.** The system disk boots first, then the install ISO. A Windows ISO started with UEFI shows `Press any key to boot from CD or DVD`
for a few seconds and gives up without a key. Type one every second for a while, as in the example above.

**Readiness.** The VM is ready when its guest agent answers. Install it as the last command of your answer file
(`E:\guest-agent\qemu-ga-x86_64.msi`), after WinRM is set up, so the provisioners start on a finished install.

**Generalization.** Run Sysprep as the `shutdown_command`, with `/generalize /oobe /shutdown`. It cannot be a provisioner:
Sysprep takes WinRM down while it runs, so Packer loses the connection before the end. The builder waits for the VM to be stopped.
Set `PersistAllDeviceInstalls` to `true` in the answer file given to Sysprep: otherwise generalize uninstalls the network adapter
that carries WinRM, and Sysprep stops there without shutting Windows down.

**KubeVirt `OCIExport` feature gate.** On KubeVirt 1.9.0 with this Alpha gate turned on, the export of a VM with a persistent TPM or EFI
stays `Pending` and serves nothing ([kubevirt/kubevirt#19084](https://github.com/kubevirt/kubevirt/issues/19084)).
The `windows.11` preferences give both, so the build waits until `vm_export_timeout`. Leave the gate off, or use a KubeVirt release with the fix
([kubevirt/kubevirt#19085](https://github.com/kubevirt/kubevirt/pull/19085), not in a 1.9 release as of 2026-09-28).

**Default answer file.** The one of the source code installs Windows 10 Pro on a BIOS machine. Bring your own for anything else.
