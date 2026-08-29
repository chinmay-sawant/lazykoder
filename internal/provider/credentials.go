package provider

import (
	"fmt"
	"os"
	"strings"
)

// CredentialSource reports the environment variable that supplies a
// configured credential for id. The key value never leaves this package.
// The returned name is the required variable when no credential is present.
func CredentialSource(id string) (string, bool) {
	descriptor, ok := DescriptorFor(id)
	if !ok || descriptor.AuthMethod != AuthMethodAPIKey {
		return "", false
	}
	for _, name := range descriptor.EnvKeys {
		if hasCredential(name) {
			return name, true
		}
	}
	return descriptor.EnvKey, false
}

func credentialValue(descriptor Descriptor) (string, error) {
	for _, name := range descriptor.EnvKeys {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value, nil
		}
	}
	if descriptor.EnvKey == "" {
		return "", nil
	}
	return "", fmt.Errorf("provider: %s is not set", descriptor.EnvKey)
}

func hasCredential(name string) bool {
	return strings.TrimSpace(os.Getenv(name)) != ""
}
