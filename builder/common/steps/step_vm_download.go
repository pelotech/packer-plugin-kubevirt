package steps

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"kubevirt.io/client-go/kubecli"
	"net/http"
	"os"
	"packer-plugin-kubevirt/builder/common/k8s"
)

const (
	exportTokenHeader = "x-kubevirt-export-token"
)

type StepDownloadVM struct {
	VirtClient kubecli.KubevirtClient
}

func (s *StepDownloadVM) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packersdk.Ui)
	ns := state.Get("namespace").(string)
	name := state.Get("name").(string)

	// TOKEN
	// CERT

	serviceName := fmt.Sprintf("virt-export-%s", name)
	service, err := s.VirtClient.CoreV1().Services(ns).Get(context.TODO(), serviceName, metav1.GetOptions{})
	podSelector := labels.SelectorFromSet(service.Spec.Selector)
	podList, err := s.VirtClient.CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{LabelSelector: podSelector.String()})
	if len(podList.Items) == 0 {
		err := fmt.Errorf("failed to resolve Virtual Machine Export Server %s/%s: %s", ns, name, err)
		state.Put("error", err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}
	// TODO: should update export server's service on 8081 (opinionated default) exposed as Packer builder field

	localPort := 8081
	err = k8s.RunPortForward(s.VirtClient, podList.Items[0].Name, ns, []string{fmt.Sprintf("%d:443", localPort)})
	if err != nil {
		err := fmt.Errorf("failed to port-forward Virtual Machine Export Server %s/%s: %s", ns, name, err)
		state.Put("error", err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}

	exportUrl := fmt.Sprintf("127.0.0.1:%d", localPort)
	cert := export.Status.Links.Internal.Cert
	err = downloadExport(exportUrl, name, token, cert)
	if err != nil {
		err := fmt.Errorf("failed to download Virtual Machine Export %s/%s: %s", ns, name, err)
		state.Put("error", err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}
	// TODO: No mention to desired volume to be downloaded / exported => digging in KubeVirt code required

	return multistep.ActionContinue
}

func downloadExport(sourceURL, destinationFilepath, exportToken, endpointCert string) error {
	out, err := os.Create(destinationFilepath)
	defer out.Close()

	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM([]byte(endpointCert))
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots},
	}
	client := &http.Client{Transport: transport}

	req, err := http.NewRequest("GET", sourceURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set(exportTokenHeader, exportToken)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

func (s *StepDownloadVM) Cleanup(_ multistep.StateBag) {
}
