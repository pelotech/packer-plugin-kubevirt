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
        name               = "base-ubuntu-2204"
        os_distribution    = "ubuntu"
        url                = "https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img"
        disk_space         = "10Gi"
        deployment_timeout = "15m"
        export_timeout     = "10m"
      }
    ]
    windows = [
      {
        name               = "base-windows-10"
        os_distribution    = "windows.10.virtio"
        url                = "https://software.download.prss.microsoft.com/dbazure/Win10_22H2_English_x64v1.iso"
        disk_space         = "15Gi"
        deployment_timeout = "20m"
        export_timeout     = "15m"
      }
    ]
  }
}

source "kubevirt-iso" "linux" {
  kubernetes_name      = local.images.linux.0.name
  kubernetes_namespace = var.kubernetes_namespace
  kubernetes_node_selectors = {
    "kubevirt.io/schedulable" = "true"
  }
  kubernetes_tolerations = [
    {
      key      = "pelo.tech/kvm"
      operator = "Equal"
      value    = "true"
      effect   = "NoSchedule"
    }
  ]
  kubevirt_os_preference = local.images.linux.0.os_distribution
  vm_disk_space          = local.images.linux.0.disk_space
  vm_linux_cloud_init    = file("/Users/chomatdam/IdeaProjects/pelotech/packer-plugin-kubevirt/builder/common/k8s/generator/scripts/cloud-init.yaml")
  # Optional (default file will be picked up)
  vm_deployment_timeout        = local.images.linux.0.deployment_timeout # Optional, default to '10m'
  vm_export_timeout            = local.images.linux.0.export_timeout     # Optional, default to '5m'
  source_url                   = local.images.linux.0.url
  source_aws_access_key_id     = var.source_aws_access_key_id     # Optional
  source_aws_secret_access_key = var.source_aws_secret_access_key # Optional
  communicator                 = "ssh"                            # Optional, default to 'ssh'
  ssh_port                     = 2222                             # Optional, default to 22
}

source "kubevirt-iso" "windows" {
  kubernetes_name      = local.images.windows.0.name
  kubernetes_namespace = var.kubernetes_namespace
  kubernetes_node_selectors = {
    "kubevirt.io/schedulable" = "true"
  }
  kubernetes_node_autoscaler   = var.kubernetes_node_autoscaler
  kubevirt_os_preference       = local.images.windows.0.os_distribution
  vm_disk_space                = local.images.windows.0.disk_space
  vm_windows_sysprep           = file("/Users/chomatdam/IdeaProjects/pelotech/packer-plugin-kubevirt/builder/common/k8s/generator/scripts/autounattend.xml")
  vm_deployment_timeout        = local.images.windows.0.deployment_timeout
  vm_export_timeout            = local.images.windows.0.export_timeout
  source_url                   = local.images.windows.0.url
  source_aws_access_key_id     = var.source_aws_access_key_id
  source_aws_secret_access_key = var.source_aws_secret_access_key
  communicator                 = "winrm"
  winrm_port                   = 5985
  winrm_use_ssl                = false
  winrm_insecure               = true
  winrm_timeout                = "30s"
}

build {
  sources = [
    "source.kubevirt-iso.linux",
    # "source.kubevirt-iso.windows"
  ]

  #   provisioner "ansible" {
  #     playbook_file = "${path.root}/ansible/playbook.yml"
  #     galaxy_file   = "${path.root}/ansible/requirements.yaml"
  #     extra_arguments = [
  #       "--extra-vars",
  #       "ansible_host=${var.ansible_host}"
  #     ]
  #   }

  post-processor "kubevirt-s3" {
    s3_bucket             = var.destination_aws_s3_bucket
    s3_key_prefix         = var.destination_aws_s3_key_prefix
    aws_region            = var.destination_aws_region
    aws_access_key_id     = var.destination_aws_access_key_id
    aws_secret_access_key = var.destination_aws_secret_access_key
    upload_timeout        = "10m" # Optional
  }
}
