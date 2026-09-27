variable "kubernetes_namespace" {
  description = "Kubernetes namespace used to provision and export virtual machines"
  type        = string
  default     = "packer"
}

variable "vm_cpu" {
  description = "CPUs requested by the virtual machine"
  type        = string
  default     = "4"
}

variable "vm_memory" {
  description = "Memory requested by the virtual machine"
  type        = string
  default     = "8Gi"
}

variable "destination_aws_s3_bucket" {
  description = "AWS S3 Bucket where exported VM images are stored"
  type        = string
}

variable "destination_aws_s3_key_prefix" {
  description = "AWS S3 Key prefix for all the exported VM images"
  type        = string
  default     = "exports/"

  validation {
    condition     = length(var.destination_aws_s3_key_prefix) == 0 || (length(var.destination_aws_s3_key_prefix) > 0 && length(regexall("[a-zA-Z0-9]+/", var.destination_aws_s3_key_prefix)) > 0)
    error_message = "The 'destination_aws_s3_key_prefix' value must end with a trailing slash."
  }
}

variable "destination_s3_endpoint_url" {
  description = "URL of an S3-compatible storage where exported VM images are stored (Empty will use AWS S3)"
  type        = string
  default     = ""
}

variable "destination_service_account_name" {
  description = "Service Account Name with S3 bucket permissions to write disk images to it (recommended)"
  type        = string
  sensitive   = true
  default     = ""
}

variable "destination_aws_access_key_id" {
  description = "AWS Access Key ID for S3 bucket containing VM images (static credentials are not recommended)"
  type        = string
  sensitive   = true
  default     = ""
}

variable "destination_aws_secret_access_key" {
  description = "AWS Secret Access Key for S3 bucket containing VM images (static credentials are not recommended)"
  type        = string
  sensitive   = true
  default     = ""
}

variable "destination_aws_region" {
  description = "AWS region used to initialize the AWS CLI uploading the exported VM image"
  type        = string
}

variable "destination_oci_image" {
  description = "Image reference, with a tag, the exported VM image is pushed to"
  type        = string
  default     = "ghcr.io/pelotech/base-ubuntu:26.04"
}

variable "destination_oci_registry_username" {
  description = "User name of the registry (Empty with the password pushes without credentials)"
  type        = string
  sensitive   = true
  default     = ""
}

variable "destination_oci_registry_password" {
  description = "Password of the registry (Empty with the user name pushes without credentials)"
  type        = string
  sensitive   = true
  default     = ""
}

variable "destination_oci_registry_insecure" {
  description = "Allow a registry served over plain HTTP"
  type        = bool
  default     = false
}
