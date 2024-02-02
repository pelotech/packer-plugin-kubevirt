packer {
  required_plugins {
    kubevirt = {
      version = "v0.0.1"
      source  = "github.com/pelotech/kubevirt"
    }
  }
}

source "kubevirt-iso" "ubuntu2204" {
  kubernetes_name              = "base-ubuntu-2204"
  kubernetes_namespace         = "packer"
  kubernetes_node_autoscaler   = "karpenter"
  kubevirt_os_preference       = "ubuntu"
  vm_disk_space                = "50Gi"
  ssh_port                     = 2222
  winrm_port                   = 5389
  source_url = "https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img"
  #  source_url                         = "https://releases.ubuntu.com/22.04.3/ubuntu-22.04.3-desktop-amd64.iso"
  source_aws_access_key_id     = var.aws_access_key_id
  source_aws_secret_access_key = var.aws_secret_access_key
}

build {
  sources = [
    "source.kubevirt-iso.ubuntu2204"
  ]
}
