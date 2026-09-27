packer {
  required_plugins {
    kubevirt = {
      version = ">= 0.1.0"
      source  = "github.com/pelotech/kubevirt"
    }
  }
}

source "kubevirt-iso" "linux" {
  kubernetes_name      = "base-ubuntu-2604"
  kubernetes_namespace = "${var.kubernetes_namespace}-linux"
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
  kubevirt_os_preference = "ubuntu"
  vm_disk_space          = "4Gi"
  vm_cpu                 = var.vm_cpu    # Optional, default to '4'
  vm_memory              = var.vm_memory # Optional, default to '8Gi'
  vm_linux_cloud_init    = file("${path.root}/cloud-init.yaml")
  # Optional (default file will be picked up)
  vm_deployment_timeout        = "15m" # Optional, default to '10m'
  vm_export_timeout            = "10m" # Optional, default to '5m'
  source_url                   = "https://cloud-images.ubuntu.com/minimal/releases/resolute/release/ubuntu-26.04-minimal-cloudimg-amd64.img"
  source_aws_access_key_id     = var.source_aws_access_key_id     # Optional
  source_aws_secret_access_key = var.source_aws_secret_access_key # Optional
  communicator                 = "ssh"                            # Optional, default to 'ssh'
  ssh_port                     = 2222                             # Optional, default to a free local port
}

build {
  sources = [
    "source.kubevirt-iso.linux"
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
    name                  = "s3"
    s3_bucket             = var.destination_aws_s3_bucket
    s3_key_prefix         = var.destination_aws_s3_key_prefix
    s3_endpoint_url       = var.destination_s3_endpoint_url # Optional
    aws_region            = var.destination_aws_region
    aws_access_key_id     = var.destination_aws_access_key_id
    aws_secret_access_key = var.destination_aws_secret_access_key
    upload_timeout        = "10m" # Optional
    keep_export           = true  # Optional, the next post-processor uses the export too
  }

  post-processor "kubevirt-oci" {
    name              = "oci"
    image             = var.destination_oci_image
    registry_username = var.destination_oci_registry_username # Optional
    registry_password = var.destination_oci_registry_password # Optional
    registry_insecure = var.destination_oci_registry_insecure # Optional
    keep_export       = true                                  # Optional, the next post-processor uses the export too
  }

  post-processor "kubevirt-datasource" {
    name            = "datasource"
    datasource_name = "base-ubuntu" # Optional, default to the Kubernetes name
  }
}
