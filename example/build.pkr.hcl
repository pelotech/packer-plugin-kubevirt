packer {
  required_plugins {
    kubevirt = {
      version = "v0.0.1"
      source  = "github.com/pelotech/kubevirt"
    }
  }
}

locals {
  images = {
    linux = [
      {
        name            = "base-ubuntu-2204"
        os_distribution = "ubuntu"
        url             = "https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img"
        disk_space      = "50Gi"
      }
    ]
  }
}

source "kubevirt-iso" "linux" {
  kubernetes_name              = local.images.linux.0.name
  kubernetes_namespace         = var.kubernetes_namespace
  kubernetes_node_autoscaler   = var.kubernetes_node_autoscaler             # Optional, default to 'default' autoscaler
  kubevirt_os_preference       = local.images.linux.0.os_distribution
  vm_disk_space                = local.images.linux.0.disk_space
  vm_deployment_timeout        = "10m"                                      # Optional, default to '10m'
  vm_export_timeout            = "5m"                                       # Optional, default to '5m'
  source_url                   = local.images.linux.0.url
  source_aws_access_key_id     = var.source_aws_access_key_id               # Optional
  source_aws_secret_access_key = var.source_aws_secret_access_key           # Optional
  communicator                 = "ssh"                                      # Optional, default to 'ssh'
  ssh_port                     = 2222                                       # Optional, default to 22
}

build {
  sources = [
    "source.kubevirt-iso.linux"
  ]

  #   provisioner "ansible" {
  #     playbook_file = "./playbook.yml"
  #     roles_path    = "/path/to/your/roles"
  #   }

  post-processor "kubevirt-s3" {
    s3_bucket             = var.destination_aws_s3_bucket
    s3_key_prefix         = var.destination_aws_s3_key_prefix
    aws_region            = var.destination_aws_region
    aws_access_key_id     = var.destination_aws_access_key_id
    aws_secret_access_key = var.destination_aws_secret_access_key
    upload_timeout        = "10m"                                      # Optional
  }
}
