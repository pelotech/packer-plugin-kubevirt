<!--
  Include a short overview about the plugin.

  This document is a great location for creating a table of contents for each
  of the components the plugin may provide. This document should load automatically
  when navigating to the docs directory for a plugin.

-->

### Installation

To install this plugin, copy and paste this code into your Packer configuration, then run [`packer init`](https://www.packer.io/docs/commands/init).

<!-- x-release-please-start-version -->
```hcl
packer {
  required_plugins {
    name = {
      # source represents the GitHub URI to the plugin repository without the `packer-plugin-` prefix.
      source  = "github.com/pelotech/kubevirt"
      version = ">=0.1.0"
    }
  }
}
```
<!-- x-release-please-end -->

Alternatively, you can use `packer plugins install` to manage installation of this plugin.

```sh
$ packer plugins install github.com/pelotech/kubevirt
```

Each release is also published as a container image for `linux/amd64` and `linux/arm64`.
The image only holds the plugin, laid out as a Packer plugin directory, to be copied into your own image:

```dockerfile
FROM hashicorp/packer:1.16.1
COPY --from=ghcr.io/pelotech/packer-plugin-kubevirt:<version> / /root/.config/packer/plugins/
```

### Components

The KubeVirt plugin is intended for creating VM base images.

#### Builders

- [kubevirt](https://github.com/pelotech/packer-plugin-kubevirt/blob/main/docs/builders/kubevirt.mdx) - The builder is used to spin up a KubeVirt VM, provision and export the associated disk image.

#### Post-processors

- [kubevirt-s3](https://github.com/pelotech/packer-plugin-kubevirt/blob/main/docs/post-processors/s3.mdx) - The S3 post-processor is used to export disk images using a Kubernetes job to an S3 bucket.
- [kubevirt-oci](https://github.com/pelotech/packer-plugin-kubevirt/blob/main/docs/post-processors/oci.mdx) - The OCI post-processor is used to publish disk images using a Kubernetes job to a container registry, as containerDisk images.
- [kubevirt-datasource](https://github.com/pelotech/packer-plugin-kubevirt/blob/main/docs/post-processors/datasource.mdx) - The DataSource post-processor is used to import disk images into a volume of the cluster and to point a DataSource to it.
