<!--
  Include a short overview about the plugin.

  This document is a great location for creating a table of contents for each
  of the components the plugin may provide. This document should load automatically
  when navigating to the docs directory for a plugin.

-->

### Installation

To install this plugin, copy and paste this code into your Packer configuration, then run [`packer init`](https://www.packer.io/docs/commands/init).

```hcl
packer {
  required_plugins {
    name = {
      # source represents the GitHub URI to the plugin repository without the `packer-plugin-` prefix.
      source  = "github.com/pelotech/kubevirt"
      version = ">=0.0.1"
    }
  }
}
```

Alternatively, you can use `packer plugins install` to manage installation of this plugin.

```sh
$ packer plugins install github.com/pelotech/kubevirt
```

### Components

The KubeVirt plugin is intended for creating VM base images.

#### Builders

- [builder](/packer/integrations/hashicorp/scaffolding/latest/components/builder/builder-name) - The ISO builder is used to spin up a KubeVirt VM, provision and export the associated disk image.

#### Post-processors

- [post-processor](/packer/integrations/hashicorp/scaffolding/latest/components/post-processor/postprocessor-name) - The S3 post-processor is used to export disk images using a Kubernetes job to an S3 bucket.
