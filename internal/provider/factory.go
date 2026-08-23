package provider

import (
	"errors"
	"os"
	"strings"

	"github.com/chinmay-sawant/lazykoder/internal/provider/openai"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/provider/subscription"
)

// NewClient creates a configured provider client. API-key clients report a
// missing key separately. Codex and Grok use their official CLI sessions,
// which keep subscription credentials outside lazykoder.
func NewClient(id string) (Client, error) {
	descriptor, ok := DescriptorFor(id)
	if !ok {
		return opencode.NewClient(""), errors.New("provider: unknown provider " + id)
	}
	switch descriptor.ID {
	case IDOpenCode:
		key, err := opencode.APIKeyFromEnv()
		return opencode.NewClient(key), err
	case IDOpenAI:
		key, err := openai.APIKeyFromEnv()
		return openai.NewClient(key, openai.WithBaseURL(descriptor.BaseURL), openai.WithModel(descriptor.Model), openai.WithProviderName(descriptor.ID)), err
	case IDGrok:
		return subscription.NewGrok(descriptor.Model), nil
	case IDCodex:
		return subscription.NewCodex(descriptor.Model), nil
	case IDXAI:
		key := strings.TrimSpace(os.Getenv(descriptor.EnvKey))
		if key == "" {
			return openai.NewClient("", openai.WithBaseURL(descriptor.BaseURL), openai.WithModel(descriptor.Model), openai.WithProviderName(descriptor.ID)), errors.New("provider: " + descriptor.EnvKey + " is not set")
		}
		return openai.NewClient(key, openai.WithBaseURL(descriptor.BaseURL), openai.WithModel(descriptor.Model), openai.WithProviderName(descriptor.ID)), nil
	default:
		return opencode.NewClient(""), errors.New("provider: " + descriptor.ID + " is not wired yet")
	}
}
