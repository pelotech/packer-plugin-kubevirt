Type: `kubevirt-datasource`

<!--
  Include a short description about the post-processor. This is a good place
  to call out what the post-processor does, and any additional text that might
  be helpful to a user. See https://www.packer.io/docs/provisioner/null
-->

The DataSource post-processor keeps the disk image produced by the `kubevirt` builder inside the cluster.
CDI imports the disk image from the Virtual Machine Export into a new volume, then the DataSource is created, or updated when it exists, to point to that volume.

A `DataSource` holds no data. It is a named pointer to a volume that new Virtual Machines clone from, like an image tag inside the cluster.
A Virtual Machine clones the volume with a `sourceRef` of kind `DataSource` in its `dataVolumeTemplates`.
With `inferFromVolume`, it also takes its preference and its instance type from the labels of the DataSource.

Each build imports into a new volume named `<datasource_name>-<UTC time of the build>`, for example `ubuntu-26.04-20260926153045`.
The volumes of previous builds are left in place, delete them once they are not needed.
The import needs temporary scratch space of the size of the volume.


<!-- Post-Processor Configuration Fields -->

**Required**

No field is required.

<!--
  Optional Configuration Fields

  Configuration options that are not required or have reasonable defaults
  should be listed under the optionals section. Defaults values should be
  noted in the description of the field
-->

**Optional**

- `datasource_name` (string) -  Name of the DataSource
`name` is kept by Packer for the post-processor block itself and does not name the DataSource
Defaults to the name of the Virtual Machine Export (`vm_name` of the builder)

- `datasource_namespace` (string) -  Namespace of the DataSource and of the volume
Defaults to the namespace of the build (`kubernetes_namespace` of the builder)

- `default_instance_type` (string) -  Instance type that Virtual Machines infer from the DataSource, set as the label `instancetype.kubevirt.io/default-instancetype`
Defaults to empty string (no label)

- `default_preference` (string) -  Preference that Virtual Machines infer from the DataSource, set as the label `instancetype.kubevirt.io/default-preference`
Defaults to the preference of the build (`vm_preference` of the builder)

- `import_timeout` (duration string) -  Timeout duration for the import of the disk image
Defaults to `10m`

- `keep_export` (bool) -  Keep the Virtual Machine Export once done, for another post-processor to use it.
The last post-processor of a build should delete it: with the export go the stopped Virtual Machine and its disk. Otherwise they stay until the export expires, after 2 hours by default
Defaults to `false`

- `volume_size` (string) -  Size of the volume
Defaults to the disk size of the build (`vm_disk_size` of the builder)

- `volume_storage_class` (string) -  Storage class of the volume
Defaults to the default storage class of the cluster

<!--
  A basic example on the usage of the post-processor. Multiple examples
  can be provided to highlight various configurations.

-->
### Example Usage


```hcl
 source "kubevirt" "linux" {
  ...
 }

build {
  sources = ["source.kubevirt.linux"]

  post-processor "kubevirt-datasource" {
    datasource_name       = "ubuntu-26.04" # Optional
    datasource_namespace  = "images"       # Optional
    default_instance_type = "u1.medium"    # Optional
    default_preference    = "ubuntu"       # Optional
    import_timeout        = "10m"          # Optional
    keep_export           = false          # Optional
    volume_size           = "12Gi"         # Optional
    volume_storage_class  = "standard"     # Optional
  }
}
```
