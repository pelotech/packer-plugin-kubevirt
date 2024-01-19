source "kubevirt-iso" "ubuntu-base" {
  mock = "mock-config"
}

build {
  sources = [
    "source.kubevirt-iso-builder.ubuntu-base"
  ]

  provisioner "shell-local" {
    inline = [
      "echo build generated data: ${build.GeneratedMockData}",
    ]
  }
}
