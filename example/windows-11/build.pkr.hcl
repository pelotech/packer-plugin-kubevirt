packer {
  required_plugins {
    kubevirt = {
      source  = "github.com/pelotech/kubevirt"
      version = ">= 0.1.0"
    }
  }
}

source "kubevirt" "windows" {
  # a Windows ISO started with UEFI waits a few seconds for a key, then gives up
  boot_command         = [for i in range(30) : "<up><wait1s>"]
  boot_wait            = "1s"
  communicator         = "winrm"
  kubernetes_namespace = var.kubernetes_namespace
  # Sysprep generalizes Windows, then shuts it down. WinRM goes away while it runs
  shutdown_command             = "C:\\Windows\\System32\\Sysprep\\Sysprep.exe /generalize /oobe /shutdown /quiet /unattend:C:\\Windows\\Temp\\unattend.xml"
  shutdown_timeout             = "15m"
  source_aws_access_key_id     = var.source_aws_access_key_id     # Optional
  source_aws_secret_access_key = var.source_aws_secret_access_key # Optional
  source_url                   = var.source_url
  vm_autounattend              = file("${path.root}/autounattend.xml")
  vm_cpu                       = var.vm_cpu
  vm_disk_size                 = "64Gi"
  vm_export_timeout            = "20m"
  vm_install_timeout           = "90m"
  vm_memory                    = var.vm_memory
  vm_name                      = "base-windows-11"
  # UEFI, Secure Boot, the TPM and the virtio devices come from the preference
  vm_preference = "windows.11.virtio"
  winrm_timeout = "10m"
}

build {
  sources = ["source.kubevirt.windows"]

  provisioner "powershell" {
    inline = ["Get-ComputerInfo -Property OsName, OsVersion, BiosFirmwareType | Format-List"]
  }

  # read by the shutdown command
  provisioner "file" {
    source      = "${path.root}/unattend.xml"
    destination = "C:\\Windows\\Temp\\unattend.xml"
  }

  post-processor "kubevirt-s3" {
    image_format         = "qcow2"
    s3_access_key_id     = var.destination_s3_access_key_id
    s3_bucket            = var.destination_s3_bucket
    s3_endpoint_url      = var.destination_s3_endpoint_url # Optional
    s3_key_prefix        = var.destination_s3_key_prefix
    s3_region            = var.destination_s3_region
    s3_secret_access_key = var.destination_s3_secret_access_key
    upload_timeout       = "60m"
  }
}
