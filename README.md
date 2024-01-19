# KubeVirt Packer Plugin

This repository contains the following sections:
- Builders:
  - [ISO builder](builder/iso)
  - IMG builder _(to be implemented)_
- Post-processors
  - [S3 Export](post-processor/s3)
  - OCI Export _(to be implemented)_
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

## Running Acceptance Tests

Make sure to build and setup the binary with:

```sh
# Build
go build .
# Move binary
cp packer-plugin-kubevirt ~/.packer.d/plugins/packer-plugin-kubevirt # Option 1
packer plugins install --path packer-plugin-kubevirt "github.com/pelotech/kubevirt" # Option 2
```

Once everything required is set up, run:
```
PACKER_ACC=1 go test -count 1 -v ./... -timeout=120m
```
This will run unit tests for all plugins in this set.

## Pipeline
- integration tests (packer running against KinD cluster)
- release (manual) for any documentation update
- release (tag event) for the binary
