package provider

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCatalog(t *testing.T) {
	wantIDs := []string{IDOpenCode, IDOpenAI, IDGrok, IDCodex, IDXAI}
	if got := IDs(); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("provider IDs = %v, want %v", got, wantIDs)
	}

	for _, id := range wantIDs {
		if _, ok := DescriptorFor(id); !ok {
			t.Fatalf("DescriptorFor(%q) did not find catalog entry", id)
		}
	}
	if got := Normalize(" OPENEI "); got != IDOpenAI {
		t.Fatalf("Normalize(OPENEI) = %q, want %q", got, IDOpenAI)
	}
	if got := Normalize("opencode-go"); got != IDOpenCode {
		t.Fatalf("Normalize(opencode-go) = %q, want %q", got, IDOpenCode)
	}
	if got := Normalize("unknown"); got != IDOpenCode {
		t.Fatalf("Normalize(unknown) = %q, want %q", got, IDOpenCode)
	}
	if _, ok := DescriptorFor("unknown"); ok {
		t.Fatal("DescriptorFor(unknown) unexpectedly returned a provider")
	}
}

func TestCatalogReturnsCopy(t *testing.T) {
	descriptors := Descriptors()
	descriptors[0].Label = "changed"
	if got, _ := DescriptorFor(IDOpenCode); got.Label == "changed" {
		t.Fatal("Descriptors exposed the internal catalog")
	}
}

func TestNewClientUsesProviderEndpoints(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-test")
	t.Setenv("OPENCODE_API_KEY", "opencode-test")
	t.Setenv("XAI_API_KEY", "xai-test")

	openCode, err := NewClient(IDOpenCode)
	if err != nil {
		t.Fatal(err)
	}
	if openCode.BaseURL() != "https://opencode.ai/zen/go/v1" {
		t.Fatalf("OpenCode base URL = %q", openCode.BaseURL())
	}
	if openCode.Model() != "deepseek-v4-flash" {
		t.Fatalf("OpenCode model = %q", openCode.Model())
	}

	openAI, err := NewClient(IDOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	if openAI.BaseURL() != "https://api.openai.com/v1" || openAI.Model() != "gpt-4.1-mini" {
		t.Fatalf("OpenAI client = base %q model %q", openAI.BaseURL(), openAI.Model())
	}

	codex, err := NewClient(IDCodex)
	if err != nil {
		t.Fatal(err)
	}
	if codex.BaseURL() != "cli://codex" || codex.Model() != "" {
		t.Fatalf("Codex client = base %q model %q", codex.BaseURL(), codex.Model())
	}
	if got := DefaultModel(IDCodex); got != "" {
		t.Fatalf("Codex default model = %q, want provider-managed default", got)
	}

	grok, err := NewClient(IDGrok)
	if err != nil {
		t.Fatal(err)
	}
	if grok.BaseURL() != "cli://grok" || grok.Model() != "grok-4.6" {
		t.Fatalf("Grok client = base %q model %q", grok.BaseURL(), grok.Model())
	}

	xai, err := NewClient(IDXAI)
	if err != nil {
		t.Fatal(err)
	}
	if xai.BaseURL() != "https://api.x.ai/v1" || xai.Model() != "grok-4.6" {
		t.Fatalf("xAI client = base %q model %q", xai.BaseURL(), xai.Model())
	}
}

func TestCredentialSource(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("OPENCODE_ZEN_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("XAI_API_KEY", "")

	if name, configured := CredentialSource(IDOpenCode); configured || name != "OPENCODE_API_KEY" {
		t.Fatalf("OpenCode credential = %q, %v", name, configured)
	}
	if name, configured := CredentialSource(IDCodex); configured || name != "" {
		t.Fatalf("Codex credential = %q, %v", name, configured)
	}
	if name, configured := CredentialSource(IDGrok); configured || name != "" {
		t.Fatalf("Grok credential = %q, %v", name, configured)
	}

	t.Setenv("OPENCODE_ZEN_API_KEY", "zen-test")
	t.Setenv("OPENAI_API_KEY", "openai-test")
	t.Setenv("XAI_API_KEY", "xai-test")
	if name, configured := CredentialSource(IDOpenCode); !configured || name != "OPENCODE_ZEN_API_KEY" {
		t.Fatalf("OpenCode credential = %q, %v", name, configured)
	}
	if _, configured := CredentialSource(IDCodex); configured {
		t.Fatal("Codex must not reuse OPENAI_API_KEY")
	}
	if _, configured := CredentialSource(IDGrok); configured {
		t.Fatal("Grok must not reuse XAI_API_KEY")
	}
}

func TestCatalogAuthMethods(t *testing.T) {
	codex, _ := DescriptorFor(IDCodex)
	if codex.AuthMethod != AuthMethodCodex || codex.CLI != "codex" {
		t.Fatalf("Codex auth = %+v", codex)
	}
	grok, _ := DescriptorFor(IDGrok)
	if grok.AuthMethod != AuthMethodGrok || grok.CLI != "grok" {
		t.Fatalf("Grok auth = %+v", grok)
	}
	xai, _ := DescriptorFor(IDXAI)
	if xai.AuthMethod != AuthMethodAPIKey || xai.EnvKey != "XAI_API_KEY" {
		t.Fatalf("xAI auth = %+v", xai)
	}
}

func TestLoginCommandUsesProviderOwnedFlows(t *testing.T) {
	binDir := t.TempDir()
	for _, name := range []string{"codex", "grok"} {
		path := filepath.Join(binDir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			t.Fatalf("make %s executable: %v", name, err)
		}
	}
	t.Setenv("PATH", binDir)
	t.Setenv("OPENAI_API_KEY", "api-key-must-not-reach-codex")
	t.Setenv("XAI_API_KEY", "api-key-must-not-reach-grok")

	codex, err := LoginCommand(IDCodex)
	if err != nil {
		t.Fatal(err)
	}
	if got := codex.Args[1:]; !reflect.DeepEqual(got, []string{"login"}) {
		t.Fatalf("Codex login args = %v", got)
	}
	if hasEnv(codex.Env, "OPENAI_API_KEY") {
		t.Fatal("Codex login inherited OPENAI_API_KEY")
	}
	grok, err := LoginCommand(IDGrok)
	if err != nil {
		t.Fatal(err)
	}
	if got := grok.Args[1:]; !reflect.DeepEqual(got, []string{"login", "--device-auth"}) {
		t.Fatalf("Grok login args = %v", got)
	}
	if hasEnv(grok.Env, "XAI_API_KEY") {
		t.Fatal("Grok login inherited XAI_API_KEY")
	}
}

func TestCheckAuthDoesNotUseDirectAPIKeysForSubscriptionProviders(t *testing.T) {
	binDir := t.TempDir()
	for name, envName := range map[string]string{"codex": "OPENAI_API_KEY", "grok": "XAI_API_KEY"} {
		path := filepath.Join(binDir, name)
		body := "#!/bin/sh\n" +
			"if [ -n \"$" + envName + "\" ]; then exit 1; fi\n" +
			"exit 0\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			t.Fatalf("make %s executable: %v", name, err)
		}
	}
	t.Setenv("PATH", binDir)
	t.Setenv("OPENAI_API_KEY", "must-not-be-used")
	t.Setenv("XAI_API_KEY", "must-not-be-used")

	for _, id := range []string{IDCodex, IDGrok} {
		if status := CheckAuth(context.Background(), id); status.State != AuthStateReady {
			t.Fatalf("%s auth status = %+v", id, status)
		}
	}
}

func hasEnv(env []string, name string) bool {
	prefix := name + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}
	return false
}
