packer {
  required_plugins {
    ansible = {
      source  = "github.com/hashicorp/ansible"
      version = "~> 1"
    }
    kubevirt = {
      source  = "github.com/pelotech/kubevirt"
      version = ">= 0.2.0" # x-release-please-version
    }
  }
}

source "kubevirt" "linux" {
  communicator         = "ssh" # Optional, default to 'ssh'
  kubernetes_namespace = "${var.kubernetes_namespace}-linux"
  kubernetes_node_selector = {
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
  source_url         = "https://cloud-images.ubuntu.com/minimal/releases/resolute/release/ubuntu-26.04-minimal-cloudimg-amd64.img"
  vm_cloud_init      = file("${path.root}/cloud-init.yaml") # Optional (default file will be picked up)
  vm_cpu             = var.vm_cpu                           # Optional, default to '4'
  vm_disk_size       = "4Gi"
  vm_export_timeout  = "10m"         # Optional, default to '5m'
  vm_install_timeout = "15m"         # Optional, default to '10m'
  vm_memory          = var.vm_memory # Optional, default to '8Gi'
  vm_name            = "base-ubuntu-2604"
  vm_preference      = "ubuntu"
}

build {
  sources = [
    "source.kubevirt.linux"
  ]

  provisioner "ansible" {
    playbook_file = "${path.root}/ansible/playbook.yaml"
    # Ansible connects to the forwarded SSH port with the login of the communicator
    use_proxy       = false
    user            = build.User
    extra_arguments = ["--extra-vars", "ansible_password=${build.Password}"]
  }

  post-processor "kubevirt-s3" {
    name = "s3"

    keep_export          = true # Optional, the next post-processor uses the export too
    s3_access_key_id     = var.destination_s3_access_key_id
    s3_bucket            = var.destination_s3_bucket
    s3_endpoint_url      = var.destination_s3_endpoint_url # Optional
    s3_key_prefix        = var.destination_s3_key_prefix
    s3_region            = var.destination_s3_region
    s3_secret_access_key = var.destination_s3_secret_access_key
    service_account_name = var.destination_service_account_name
    upload_timeout       = "10m" # Optional
  }

  post-processor "kubevirt-oci" {
    name = "oci"

    image             = var.destination_oci_image
    keep_export       = true                                  # Optional, the next post-processor uses the export too
    registry_insecure = var.destination_oci_registry_insecure # Optional
    registry_password = var.destination_oci_registry_password # Optional
    registry_username = var.destination_oci_registry_username # Optional
  }

  post-processor "kubevirt-datasource" {
    name = "datasource"

    datasource_name = "base-ubuntu" # Optional, default to vm_name
  }
}
