variable "kubernetes_namespace" {
  description = "Kubernetes namespace used to provision and export virtual machines"
  type        = string
}

variable "source_aws_access_key_id" {
  description = "AWS Access Key ID for S3 bucket containing VM images (Empty will skip adding credentials)"
  type        = string
  sensitive   = true
  default     = ""
}

variable "source_aws_secret_access_key" {
  description = "AWS Secret Access Key for S3 bucket containing VM images (Empty will skip adding credentials)"
  type        = string
  sensitive   = true
  default     = ""
}

# variable "ansible_host" {
#   description = "Target host defined in Ansible Playbook used."
#   type        = string
# }

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
