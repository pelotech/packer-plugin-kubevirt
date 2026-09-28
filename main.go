package main

import (
	"fmt"
	"os"
	"packer-plugin-kubevirt/builder/kubevirt"
	"packer-plugin-kubevirt/post-processor/datasource"
	"packer-plugin-kubevirt/post-processor/oci"
	"packer-plugin-kubevirt/post-processor/s3"
	kubevirtVersion "packer-plugin-kubevirt/version"

	"github.com/hashicorp/packer-plugin-sdk/plugin"
)

func main() {
	pps := plugin.NewSet()
	pps.RegisterBuilder(plugin.DEFAULT_NAME, new(kubevirt.Builder))
	pps.RegisterPostProcessor("s3", new(s3.PostProcessor))
	pps.RegisterPostProcessor("oci", new(oci.PostProcessor))
	pps.RegisterPostProcessor("datasource", new(datasource.PostProcessor))
	pps.SetVersion(kubevirtVersion.PluginVersion)
	err := pps.Run()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
