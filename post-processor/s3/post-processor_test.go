package s3

import (
	"strings"
	"testing"
)

func TestConfigureRejectsInvalidEndpointUrl(t *testing.T) {
	for _, endpointUrl := range []string{"garage.garage.svc:3900", "s3://garage.garage.svc", "http://"} {
		postProcessor := new(PostProcessor)
		err := postProcessor.Configure(map[string]interface{}{
			"s3_endpoint_url": endpointUrl,
		})
		if err == nil || !strings.Contains(err.Error(), "invalid 's3_endpoint_url' value '"+endpointUrl+"'") {
			t.Errorf("expected an invalid 's3_endpoint_url' error for '%s', got: %v", endpointUrl, err)
		}
	}
}

func TestValidateEndpointUrl(t *testing.T) {
	for _, endpointUrl := range []string{"", "http://garage.garage.svc:3900", "https://s3.example.com"} {
		if err := validateEndpointUrl(endpointUrl); err != nil {
			t.Errorf("expected endpoint URL '%s' to be valid, got: %v", endpointUrl, err)
		}
	}
}
