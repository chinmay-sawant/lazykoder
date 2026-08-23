package provider

import (
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
	if descriptor.ID == IDOpenCode {
		for _, name := range []string{"OPENCODE_API_KEY", "OPENCODE_ZEN_API_KEY"} {
			if hasCredential(name) {
				return name, true
			}
		}
		return descriptor.EnvKey, false
	}
	return descriptor.EnvKey, hasCredential(descriptor.EnvKey)
}

func hasCredential(name string) bool {
	return strings.TrimSpace(os.Getenv(name)) != ""
}
