# KubeVirt Packer Plugin

This repository contains the following sections:
- Builders:
  - [ISO builder](builder/iso)
- Post-processors
  - [S3 Export](post-processor/s3)
  - [OCI Export](post-processor/oci)
- [Docs](docs)
- [Example](example)

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

## Tests

Unit tests:

```shell
go test ./...
```

The integration test builds the [example](example) on a KinD cluster with KubeVirt and CDI, in the `test plugin` workflow.
It runs when a pull request is approved, or on demand:

```shell
gh workflow run tests.yml --ref <branch>
```

A push only runs `packer init` and `packer validate`.

## Pipeline
- integration tests (packer running against KinD cluster)
- release (manual) for any documentation update
- release (tag event) for the binary
