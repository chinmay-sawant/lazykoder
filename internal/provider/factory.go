package provider

import (
	"errors"
	"fmt"

	"github.com/chinmay-sawant/lazykoder/internal/provider/openai"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/provider/subscription"
)

var builtinFactories = map[string]DescriptorFactory{
	IDOpenCode: func(descriptor Descriptor) (Client, error) {
		key, err := credentialValue(descriptor)
		return opencode.NewClient(key), err
	},
	IDOpenAI: openAICompatibleFactory,
	IDXAI:    openAICompatibleFactory,
	IDCodex: func(descriptor Descriptor) (Client, error) {
		return subscription.NewCodex(descriptor.Model), nil
	},
	IDGrok: func(descriptor Descriptor) (Client, error) {
		return subscription.NewGrok(descriptor.Model), nil
	},
}

func init() {
	for _, descriptor := range builtinProviders {
		descriptor.Factory = builtinFactories[descriptor.ID]
		if descriptor.AuthMethod != AuthMethodAPIKey {
			descriptor.AuthChecker = cliAuthChecker
			descriptor.LoginCommand = cliLoginCommand
		}
		if err := Register(descriptor); err != nil {
			panic(err)
		}
	}
}

// NewClient creates a client through the descriptor registry. Unknown IDs are
// errors and never silently route to OpenCode.
func NewClient(id string) (Client, error) {
	descriptor, ok := DescriptorFor(id)
	if !ok {
		return nil, fmt.Errorf("provider: unknown provider %q", id)
	}
	if descriptor.Factory != nil {
		return descriptor.Factory(descriptor)
	}
	if descriptor.AuthMethod != AuthMethodAPIKey {
		return nil, fmt.Errorf("provider: %s requires a registered client factory", descriptor.ID)
	}
	return openAICompatibleFactory(descriptor)
}

func openAICompatibleFactory(descriptor Descriptor) (Client, error) {
	if descriptor.BaseURL == "" {
		return nil, errors.New("provider: compatible provider requires base_url")
	}
	key, err := credentialValue(descriptor)
	client := openai.NewClient(key,
		openai.WithBaseURL(descriptor.BaseURL),
		openai.WithModel(descriptor.Model),
		openai.WithProviderName(descriptor.ID),
	)
	return client, err
}
