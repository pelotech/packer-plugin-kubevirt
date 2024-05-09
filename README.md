# Packer Plugin KubeVirt

This repository is a Packer
- A builder ([builder/scaffolding](builder/windows))
- Post-processors ([post-processor/scaffolding](post-processor/kubevirt))
  - S3 Export
- Docs ([docs](docs))
- A working example ([example](example))

In this repository you will also find a pre-defined GitHub Action configuration for the release workflow
(`.goreleaser.yml` and `.github/workflows/release.yml`). The release workflow configuration makes sure the GitHub
release artifacts are created with the correct binaries and naming conventions.

## Build the plugin

```shell
go build .
```
## Local installation of the plugin

```shell
packer plugins install --path ./packer-plugin-kubevirt "github.com/pelotech/kubevirt"
```

## Run the plugin

```shell
# If needed, the arg '-debug' will pause the process between each step
PACKER_LOG=1 packer build -debug ./example
```

## Running Acceptance Tests

Make sure to install the plugin with `go build .` and to have Packer installed locally.
Then source the built binary to the plugin path with `cp packer-plugin-kubevirt ~/.packer.d/plugins/packer-plugin-kubevirt`
or using `packer plugins install`. Once everything needed is set up, run:
```
PACKER_ACC=1 go test -count 1 -v ./... -timeout=120m
```

This will run the acceptance tests for all plugins in this set.

## Test Plugin Example Action

This scaffolding configures a [manually triggered plugin test action](/.github/workflows/test-plugin-example.yml).
By default, the action will run Packer at the latest version to init, validate, and build the example configuration
within the [example](example) folder. This is useful to quickly test a basic template of your plugin against Packer.

The example must contain the `required_plugins` block and require your plugin at the latest or any other released version.
This will help test and validate plugin releases.

## Registering Plugin as Packer Integration

Partner and community plugins can be hard to find if a user doesn't know what 
they are looking for. To assist with plugin discovery Packer offers an integration
portal at https://developer.hashicorp.com/packer/integrations to list known integrations 
that work with the latest release of Packer. 

Registering a plugin as an integration requires [metadata configuration](./metadata.hcl) within the plugin
repository and approval by the Packer team. To initiate the process of registering your 
plugin as a Packer integration refer to the [Developing Plugins](https://developer.hashicorp.com/packer/docs/plugins/creation#registering-plugins) page.

# Requirements

-	[packer-plugin-sdk](https://github.com/hashicorp/packer-plugin-sdk) >= v0.5.2
-	[Go](https://golang.org/doc/install) >= 1.21
-   Packer >= >= v1.10
