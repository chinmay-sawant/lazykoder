// Package provider contains the shared chat-client contract used by the
// agent, UI, and background workers.
package provider

import (
	"context"
	"net/http"

	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

// Client is the provider contract required by the application.
//
// The wire types remain owned by the OpenAI-compatible package because both
// supported providers use the same chat-completions shape. Provider-specific
// clients translate their own HTTP behavior behind this contract.
type Client interface {
	Chat(context.Context, opencode.ChatRequest) (*opencode.ChatResponse, error)
	ChatStream(context.Context, opencode.ChatRequest, func(opencode.Delta) error) (*opencode.ChatResponse, error)
	Model() string
	BaseURL() string
	HTTP() *http.Client
	RetryPolicy() opencode.RetryPolicy
	SetRetryPolicy(opencode.RetryPolicy)
	ModelInfos(context.Context) ([]opencode.ModelInfo, error)
}

// FreeModelCatalog is implemented by providers that expose an additional
// catalog outside their standard models endpoint.
type FreeModelCatalog interface {
	FreeModelInfos(context.Context) ([]opencode.ModelInfo, error)
}

// UsageProvider is implemented by providers that expose account usage.
type UsageProvider interface {
	Usage(context.Context) (opencode.BillingUsage, error)
}

// ChatClient is the smaller contract used by no-tools background workers.
type ChatClient interface {
	Chat(context.Context, opencode.ChatRequest) (*opencode.ChatResponse, error)
}
