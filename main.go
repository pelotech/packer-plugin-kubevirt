package main

import (
	"fmt"
	"os"
	"packer-plugin-kubevirt/builder/iso"
	"packer-plugin-kubevirt/post-processor/s3"
	kubevirtVersion "packer-plugin-kubevirt/version"

	"github.com/hashicorp/packer-plugin-sdk/plugin"
)

func main() {
	pps := plugin.NewSet()
	pps.RegisterBuilder("iso", new(iso.Builder))
	pps.RegisterPostProcessor("s3", new(s3.PostProcessor))
	pps.SetVersion(kubevirtVersion.PluginVersion)
	err := pps.Run()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
