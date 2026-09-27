variable "kubernetes_namespace" {
  description = "Kubernetes namespace used to provision and export virtual machines"
  type        = string
  default     = "packer-windows"
}

variable "source_url" {
  description = "URL of the Windows 11 install ISO, here the evaluation of Windows 11 Enterprise LTSC 2024"
  type        = string
  default     = "https://software-static.download.prss.microsoft.com/dbazure/888969d5-f34g-4e03-ac9d-1f9786c66749/26100.1742.240906-0331.ge_release_svc_refresh_CLIENT_LTSC_EVAL_x64FRE_en-us.iso"
}

variable "source_aws_access_key_id" {
  description = "AWS Access Key ID for S3 bucket containing the ISO (Empty will skip adding credentials)"
  type        = string
  sensitive   = true
  default     = ""
}

variable "source_aws_secret_access_key" {
  description = "AWS Secret Access Key for S3 bucket containing the ISO (Empty will skip adding credentials)"
  type        = string
  sensitive   = true
  default     = ""
}

variable "vm_cpu" {
  description = "CPUs requested by the virtual machine, Windows 11 needs 2 or more"
  type        = string
  default     = "4"
}

variable "vm_memory" {
  description = "Memory requested by the virtual machine, Windows 11 needs 4Gi or more"
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
}

variable "destination_s3_endpoint_url" {
  description = "URL of an S3-compatible storage where exported VM images are stored (Empty will use AWS S3)"
  type        = string
  default     = ""
}

variable "destination_aws_access_key_id" {
  description = "AWS Access Key ID for S3 bucket containing VM images"
  type        = string
  sensitive   = true
  default     = ""
}

variable "destination_aws_secret_access_key" {
  description = "AWS Secret Access Key for S3 bucket containing VM images"
  type        = string
  sensitive   = true
  default     = ""
}

variable "destination_aws_region" {
  description = "AWS region used to initialize the AWS CLI uploading the exported VM image"
  type        = string
}
