# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

# Details on using this Integration template can be found at https://github.com/hashicorp/integration-template
# Alternatively this metadata.hcl file can be placed under the docs/ subdirectory or any other config subdirectory that
# makes senses for the plugin.
integration {
  name = "KubeVirt"
  description = "Builds Virtual Machine disk images on Kubernetes with KubeVirt and exports them."
  identifier = "packer/pelotech/kubevirt"
  flags = [ "community" ]
  docs {
    process_docs = true
    # We recommend using the default readme_location of just `./README.md` here
    # This projects README needs to document the interface of an integration.
    #
    # If you need a separate README from what you will display on GitHub vs
    # what is shown on HashiCorp Developer, this is totally valid, though!
    readme_location = "./README.md"
    external_url = "https://github.com/pelotech/packer-plugin-kubevirt"
  }
  license {
    type = "MPL-2.0"
    url = "https://github.com/pelotech/packer-plugin-kubevirt/blob/main/LICENSE"
  }
  component {
    type = "builder"
    name = "KubeVirt"
    slug = "builder"
  }
  component {
    type = "post-processor"
    name = "KubeVirt S3"
    slug = "post-processor"
  }
  component {
    type = "post-processor"
    name = "KubeVirt OCI"
    slug = "oci"
  }
  component {
    type = "post-processor"
    name = "KubeVirt DataSource"
    slug = "datasource"
  }
}
