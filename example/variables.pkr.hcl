variable "aws_access_key_id" {
  description = "AWS Access Key ID for S3 bucket containing VM images (Empty will skip adding credentials)"
  type        = string
  sensitive   = true
  default     = ""
}

variable "aws_secret_access_key" {
  description = "AWS Secret Access Key for S3 bucket containing VM images (Empty will skip adding credentials)"
  type        = string
  sensitive   = true
  default     = ""
}