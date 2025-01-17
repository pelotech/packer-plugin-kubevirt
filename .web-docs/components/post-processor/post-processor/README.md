  Include a short description about the post-processor. This is a good place
  to call out what the post-processor does, and any additional text that might
  be helpful to a user. See https://www.packer.io/docs/provisioner/null
-->

The S3 post-processor is used to export Packer Scaffolding to an S3 bucket.


<!-- Post-Processor Configuration Fields -->

**Required**

- `s3_bucket` (string) -  AWS S3 Bucket where exported VM images are stored

- `s3_key_prefix` (string) -  AWS S3 Key prefix for all the exported VM images

- `aws_region` (string) -  AWS region used to initialize the AWS CLI uploading the exported VM image

<!--
  Optional Configuration Fields

  Configuration options that are not required or have reasonable defaults
  should be listed under the optionals section. Defaults values should be
  noted in the description of the field
-->

**Optional**
- `service_account_name` (string) - Service Account Name with associated S3 permissions to export a disk image to S3.

- `aws_access_key_id` (string) -  AWS Access Key ID for S3 bucket containing VM images
Sensitive field - Defaults to empty string (will skip adding credentials)

- `aws_secret_access_key` (string) -  AWS Secret Access Key for S3 bucket containing VM images
Sensitive field - Defaults to empty string (will skip adding credentials)

- `upload_timeout` (string) -  Upload timeout duration
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

  post-processor "kubevirt-s3" {
    s3_bucket             = "virtual-machine-disk-images"
    s3_key_prefix         = "kubevirt"
    aws_region            = "us-east-1"
    aws_access_key_id     = "AWS_ACCESS_KEY_ID"
    aws_secret_access_key = "AWS_SECRET_ACCESS_KEY"
    upload_timeout        = "10m"                         # Optional
  }
}
```
