Type: `kubevirt-s3`

<!--
  Include a short description about the post-processor. This is a good place
  to call out what the post-processor does, and any additional text that might
  be helpful to a user. See https://www.packer.io/docs/provisioner/null
-->

The S3 post-processor is used to export the disk image produced by the `kubevirt-iso` builder to an S3 bucket.
A Kubernetes job downloads the disk image from the Virtual Machine Export and uploads it to the bucket as `<kubernetes_name>.img.gz`.


<!-- Post-Processor Configuration Fields -->

**Required**

- `s3_bucket` (string) -  AWS S3 Bucket where exported VM images are stored

- `aws_region` (string) -  AWS region used to initialize the AWS CLI uploading the exported VM image

- Credentials, one of:
  - `service_account_name` (string) - Service Account Name with associated S3 permissions to export a disk image to S3 (recommended, takes priority)
  - `aws_access_key_id` (string) and `aws_secret_access_key` (string) - AWS static credentials with S3 permissions.
  Sensitive fields

<!--
  Optional Configuration Fields

  Configuration options that are not required or have reasonable defaults
  should be listed under the optionals section. Defaults values should be
  noted in the description of the field
-->

**Optional**

- `s3_key_prefix` (string) -  AWS S3 Key prefix for all the exported VM images
Defaults to empty string (image stored at the root of the bucket)

- `upload_timeout` (duration string) -  Upload timeout duration
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
