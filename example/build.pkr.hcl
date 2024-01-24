packer {
  required_plugins {
    kubevirt = {
      version = ">=v0.1.0"
      source  = "github.com/chomatdam/kubevirt"
    }
  }
}

source "kubevirt-iso" "windows11" {
  mock = local.foo
}

build {
  sources = [
    "source.kubevirt-iso.windows11",
    "source.kubevirt-iso.ubuntu2204",
  ]
}
