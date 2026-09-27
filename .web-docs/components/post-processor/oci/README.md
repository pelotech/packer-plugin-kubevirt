Type: `kubevirt-oci`

<!--
  Include a short description about the post-processor. This is a good place
  to call out what the post-processor does, and any additional text that might
  be helpful to a user. See https://www.packer.io/docs/provisioner/null
-->

The OCI post-processor is used to publish the disk image produced by the `kubevirt-iso` builder as a [containerDisk](https://kubevirt.io/user-guide/storage/disks_and_volumes/#containerdisk) image in a container registry.
A Kubernetes job downloads the disk image from the Virtual Machine Export and pushes an image with a single layer, holding the disk at `/disk/<kubernetes_name>.qcow2` and owned by the user and group `107`.
KubeVirt boots that image with a `containerDisk` volume and CDI imports it with a `registry` source.

The registry has to be reachable from the cluster. The job builds an image archive and pushes it once with `krane push`.
[krane](https://github.com/google/go-containerregistry/blob/main/cmd/krane/README.md) is `crane` with the credential helpers of the cloud providers.


<!-- Post-Processor Configuration Fields -->

**Required**

- `image` (string) -  Image the disk is pushed to, with its registry and its tag. Example: `ghcr.io/pelotech/base-ubuntu:22.04`
An image without a tag or with a digest is rejected, the digest is only known once the image is pushed

<!--
  Optional Configuration Fields

  Configuration options that are not required or have reasonable defaults
  should be listed under the optionals section. Defaults values should be
  noted in the description of the field
-->

**Optional**

- Credentials of the registry, the image is pushed anonymously without them:
  - `registry_username` (string) and `registry_password` (string) - Credentials stored as a docker config in a secret owned by the job.
  Sensitive fields
  - `registry_secret_name` (string) - Name of an existing secret of type `kubernetes.io/dockerconfigjson` in the namespace of the build.
  It cannot be used with `registry_username` and `registry_password`
  - `service_account_name` (string) - Service Account Name of the job, for the registries of cloud providers that authenticate the workload

- `registry_insecure` (bool) -  Allow a registry served over plain HTTP or with an untrusted certificate
Defaults to `false`

- `image_format` (string) -  Format of the disk in the image, converted with `qemu-img`. A `raw` disk is stored as `/disk/<kubernetes_name>.img`.
The job needs scratch space for twice the size of the raw disk
Accepted values: `qcow2`, `raw` - Defaults to `qcow2`

- `default_preference` (string) -  KubeVirt preference a Virtual Machine created from the image defaults to.
It is set on the image as `INSTANCETYPE_KUBEVIRT_IO_DEFAULT_PREFERENCE`, CDI turns it into the label `instancetype.kubevirt.io/default-preference` on import
Defaults to the `kubevirt_os_preference` of the builder

- `default_instance_type` (string) -  KubeVirt instance type a Virtual Machine created from the image defaults to.
It is set on the image as `INSTANCETYPE_KUBEVIRT_IO_DEFAULT_INSTANCETYPE`, CDI turns it into the label `instancetype.kubevirt.io/default-instancetype` on import
Defaults to empty string (not set on the image)

- `upload_timeout` (duration string) -  Timeout duration for the download, the conversion and the push
Defaults to `10m`

<!--
  A basic example on the usage of the post-processor. Multiple examples
  can be provided to highlight various configurations.

-->
### Example Usage


```hcl
 source "kubevirt-iso" "linux" {
  ...
 }

build {
  sources = ["source.kubevirt-iso.linux"]

  post-processor "kubevirt-oci" {
    image                 = "ghcr.io/pelotech/base-ubuntu:22.04"
    registry_username     = "REGISTRY_USERNAME"
    registry_password     = "REGISTRY_PASSWORD"
    image_format          = "qcow2"                       # Optional
    default_instance_type = "u1.medium"                   # Optional
    upload_timeout        = "10m"                         # Optional
  }
}
```

The image can then be used by a Virtual Machine:

```yaml
volumes:
  - name: disk
    containerDisk:
      image: ghcr.io/pelotech/base-ubuntu:22.04
```
