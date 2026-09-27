package oci

import (
	buildercommon "packer-plugin-kubevirt/builder/common"
	"strings"
	"testing"
)

func newConfig() Config {
	return Config{
		Image:       "ghcr.io/pelotech/base-ubuntu:22.04",
		ImageFormat: "qcow2",
	}
}

func TestValidateAcceptsEachAuthentication(t *testing.T) {
	anonymous := newConfig()
	credentials := newConfig()
	credentials.RegistryUsername, credentials.RegistryPassword = "packer", "secret"
	secretName := newConfig()
	secretName.RegistrySecretName = "registry-credentials"
	serviceAccount := newConfig()
	serviceAccount.ServiceAccountName = "oci-uploader"

	for name, config := range map[string]Config{
		"anonymous":       anonymous,
		"credentials":     credentials,
		"secret name":     secretName,
		"service account": serviceAccount,
	} {
		if err := config.validate(); err != nil {
			t.Errorf("expected a valid configuration with %s, got: %v", name, err)
		}
	}
}

func TestValidateAcceptsEachImageFormat(t *testing.T) {
	for _, format := range []string{"qcow2", "raw"} {
		config := newConfig()
		config.ImageFormat = format
		if err := config.validate(); err != nil {
			t.Errorf("expected image format '%s' to be valid, got: %v", format, err)
		}
	}
}

func TestValidateRejectsInvalidConfig(t *testing.T) {
	missingImage := newConfig()
	missingImage.Image = ""
	missingTag := newConfig()
	missingTag.Image = "registry:5000/pelotech/base-ubuntu"
	digest := newConfig()
	digest.Image = "ghcr.io/pelotech/base-ubuntu@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	imageFormat := newConfig()
	imageFormat.ImageFormat = "vmdk"
	missingPassword := newConfig()
	missingPassword.RegistryUsername = "packer"
	missingUsername := newConfig()
	missingUsername.RegistryPassword = "secret"
	secretNameWithCredentials := newConfig()
	secretNameWithCredentials.RegistrySecretName = "registry-credentials"
	secretNameWithCredentials.RegistryUsername, secretNameWithCredentials.RegistryPassword = "packer", "secret"

	for _, test := range []struct {
		config        Config
		expectedError string
	}{
		{missingImage, "'image' is required"},
		{missingTag, "'image' must carry a tag and no digest"},
		{digest, "'image' must carry a tag and no digest"},
		{imageFormat, "unsupported image format 'vmdk'"},
		{missingPassword, "'registry_username' and 'registry_password' must be provided together"},
		{missingUsername, "'registry_username' and 'registry_password' must be provided together"},
		{secretNameWithCredentials, "'registry_secret_name' cannot be used with 'registry_username' and 'registry_password'"},
	} {
		if err := test.config.validate(); err == nil || !strings.Contains(err.Error(), test.expectedError) {
			t.Errorf("expected error \"%s\", got: %v", test.expectedError, err)
		}
	}
}

func TestDefaultPreferenceFallsBackToTheBuilder(t *testing.T) {
	source := &buildercommon.KubevirtArtifact{
		StateData: map[string]interface{}{
			buildercommon.PreferenceArtifactKey: "ubuntu",
		},
	}

	processor := PostProcessor{config: newConfig()}
	if preference := processor.findDefaultPreference(source); preference != "ubuntu" {
		t.Errorf("expected the preference of the builder, got: '%s'", preference)
	}

	processor.config.DefaultPreference = "fedora"
	if preference := processor.findDefaultPreference(source); preference != "fedora" {
		t.Errorf("expected the configured preference, got: '%s'", preference)
	}

	source.StateData = map[string]interface{}{}
	processor.config.DefaultPreference = ""
	if preference := processor.findDefaultPreference(source); preference != "" {
		t.Errorf("expected no preference, got: '%s'", preference)
	}
}
