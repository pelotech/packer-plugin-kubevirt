packer {
  required_plugins {
    kubevirt = {
      version = ">= 0.1.0"
      source  = "github.com/pelotech/kubevirt"
    }
  }
}

source "kubevirt-iso" "windows" {
  vm_name              = "base-windows-11"
  kubernetes_namespace = var.kubernetes_namespace
  # UEFI, Secure Boot, the TPM and the virtio devices come from the preference
  kubevirt_os_preference       = "windows.11.virtio"
  source_url                   = var.source_url
  source_aws_access_key_id     = var.source_aws_access_key_id     # Optional
  source_aws_secret_access_key = var.source_aws_secret_access_key # Optional
  vm_disk_space                = "64Gi"
  vm_cpu                       = var.vm_cpu
  vm_memory                    = var.vm_memory
  vm_windows_sysprep           = file("${path.root}/autounattend.xml")
  vm_deployment_timeout        = "90m"
  vm_export_timeout            = "20m"

  # a Windows ISO started with UEFI waits a few seconds for a key, then gives up
  boot_wait    = "1s"
  boot_command = [for i in range(30) : "<up><wait1s>"]

  communicator  = "winrm"
  winrm_timeout = "10m"

  # Sysprep generalizes Windows, then shuts it down. WinRM goes away while it runs
  shutdown_command = "C:\\Windows\\System32\\Sysprep\\Sysprep.exe /generalize /oobe /shutdown /quiet /unattend:C:\\Windows\\Temp\\unattend.xml"
  shutdown_timeout = "15m"
}

build {
  sources = ["source.kubevirt-iso.windows"]

  provisioner "powershell" {
    inline = ["Get-ComputerInfo -Property OsName, OsVersion, BiosFirmwareType | Format-List"]
  }

  # read by the shutdown command
  provisioner "file" {
    source      = "${path.root}/unattend.xml"
    destination = "C:\\Windows\\Temp\\unattend.xml"
  }

  post-processor "kubevirt-s3" {
    s3_bucket             = var.destination_aws_s3_bucket
    s3_key_prefix         = var.destination_aws_s3_key_prefix
    s3_endpoint_url       = var.destination_s3_endpoint_url # Optional
    aws_region            = var.destination_aws_region
    aws_access_key_id     = var.destination_aws_access_key_id
    aws_secret_access_key = var.destination_aws_secret_access_key
    image_format          = "qcow2"
    upload_timeout        = "60m"
  }
}
