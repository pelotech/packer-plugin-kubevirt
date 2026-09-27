# KubeVirt Packer Plugin

Build virtual machine images on Kubernetes with [KubeVirt](https://kubevirt.io), then export them to S3, to a container registry, or keep them in the cluster.

## How it works

1. The builder creates a Virtual Machine from an ISO or a cloud image and waits for the guest to be ready.
2. Packer provisioners run in the guest over SSH or WinRM (shell, Ansible and so on).
3. The Virtual Machine is stopped. Linux disks are generalized with `virt-sysprep`.
4. The disk is exposed through a Virtual Machine Export.
5. Post-processors export the disk. Several can run on the same build.

Everything runs in the cluster, as Virtual Machines and jobs. Nothing else than Packer is needed on your machine.

## Components

| Component | Type | What it does |
|---|---|---|
| [kubevirt-iso](docs/builders/builder.mdx) | builder | Creates and provisions the Virtual Machine |
| [kubevirt-s3](docs/post-processors/post-processor.mdx) | post-processor | Uploads the disk to S3 or S3-compatible storage, as it is or converted to `qcow2`, `vmdk`, `vhdx` or `vdi` |
| [kubevirt-oci](docs/post-processors/oci.mdx) | post-processor | Pushes the disk to a container registry as a containerDisk image |
| [kubevirt-datasource](docs/post-processors/datasource.mdx) | post-processor | Imports the disk into a volume of the cluster and points a DataSource to it |

Images pushed by `kubevirt-oci` and DataSources created by `kubevirt-datasource` carry the preference of the build, so Virtual Machines created from them can infer it.

Linux builds are covered by the integration test. Windows builds are not.

## Requirements

- A Kubernetes cluster with [KubeVirt](https://kubevirt.io/user-guide/cluster_admin/installation/) 1.9 or later and [CDI](https://github.com/kubevirt/containerized-data-importer), and nodes with KVM
- The [preferences](https://github.com/kubevirt/common-instancetypes) you refer to with `kubevirt_os_preference`
- A kubeconfig for that cluster. The plugin uses your current context, or the service account of its pod when Packer runs in the cluster
- Packer. The integration test runs with Packer 1.16.1

## Install

```hcl
packer {
  required_plugins {
    kubevirt = {
      source  = "github.com/pelotech/kubevirt"
      version = ">= 0.1.0"
    }
  }
}
```

Then run `packer init`. It installs the latest release. To use what is on `main` before it is released, build the plugin from the sources, see [Development](#development).

Each release is also published as a container image, to copy the plugin into your own image:

```dockerfile
FROM hashicorp/packer:1.16.1
COPY --from=ghcr.io/pelotech/packer-plugin-kubevirt:<version> / /root/.config/packer/plugins/
```

## Quick start

```hcl
source "kubevirt-iso" "ubuntu" {
  kubernetes_name        = "base-ubuntu-2604"
  kubernetes_namespace   = "packer"
  source_url             = "https://cloud-images.ubuntu.com/minimal/releases/resolute/release/ubuntu-26.04-minimal-cloudimg-amd64.img"
  kubevirt_os_preference = "ubuntu"
  vm_disk_space          = "10Gi"
}

build {
  sources = ["source.kubevirt-iso.ubuntu"]

  provisioner "shell" {
    inline = ["sudo apt-get install --yes nginx"]
  }

  post-processor "kubevirt-datasource" {}
}
```

```shell
packer init .
packer build .
```

This keeps the image in the cluster. To export it somewhere else as well, chain post-processors. Each one except the last keeps the export for the next:

```hcl
  post-processor "kubevirt-s3" {
    s3_bucket             = "virtual-machine-images"
    aws_region            = "us-east-1"
    aws_access_key_id     = var.aws_access_key_id
    aws_secret_access_key = var.aws_secret_access_key
    keep_export           = true
  }

  post-processor "kubevirt-oci" {
    image             = "ghcr.io/pelotech/base-ubuntu:26.04"
    registry_username = var.registry_username
    registry_password = var.registry_password
  }
```

The [example](example) is a complete template. Its Linux source is the one built by the integration test.

## Documentation

- [Builder](docs/builders/builder.mdx)
- [S3 post-processor](docs/post-processors/post-processor.mdx)
- [OCI post-processor](docs/post-processors/oci.mdx)
- [DataSource post-processor](docs/post-processors/datasource.mdx)

## Development

Build the plugin and install it for Packer:

```shell
go build .
packer plugins install --path ./packer-plugin-kubevirt "github.com/pelotech/kubevirt"
```

Run the example. With `-debug` Packer pauses between each step:

```shell
PACKER_LOG=1 packer build -debug ./example
```

Regenerate the HCL specifications and the web docs after a change of a configuration or of the docs:

```shell
make generate
```

### Tests

Unit tests:

```shell
go test ./...
```

The integration test builds the [example](example) on a KinD cluster with KubeVirt and CDI, in the `test plugin` workflow.
A push to a branch builds it once per export, side by side. A push to `main` builds it once with the three exports in a row.
It also runs on demand, both ways:

```shell
gh workflow run tests.yml --ref <branch>
```

The image is uploaded to a [Garage](https://garagehq.deuxfleurs.fr) bucket, pushed to a registry and imported behind a DataSource, all in the cluster, so the test needs no account. A Virtual Machine is then started from the DataSource to check that the image boots.

### Release

[release-please](https://github.com/googleapis/release-please) reads the commits of `main` and keeps a release pull request open, with the next version and the changelog.
A `fix` bumps the patch version. A `feat` or, until 1.0.0, a breaking change bumps the minor version.

Merging that pull request creates the tag and the GitHub release. GoReleaser then builds the binaries, attaches them to the release and pushes the container image.

To release by hand, push a tag `v*` on a commit of `main`: GoReleaser runs the same way and creates the release.
Then set that version in `version/version.go` and `.release-please-manifest.json`, so that release-please starts from it.
