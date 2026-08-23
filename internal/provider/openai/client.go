// Package openai implements the OpenAI chat-completions provider.
package openai

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

const (
	DefaultBaseURL = "https://api.openai.com/v1"
	DefaultModelID = "gpt-4.1-mini"
	ProviderName   = "openai"
)

// ErrMissingAPIKey reports that the OpenAI credential is unavailable.
var ErrMissingAPIKey = errors.New("openai: OPENAI_API_KEY is not set")

// APIKeyFromEnv reads the key loaded from the process environment. The
// application loads .env before calling this function, so both sources use
// the same resolution path.
func APIKeyFromEnv() (string, error) {
	value := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if value == "" {
		return "", ErrMissingAPIKey
	}
	return value, nil
}

// Option configures an OpenAI client.
type Option func(*Client)

// WithBaseURL points requests at a test server or compatible endpoint.
func WithBaseURL(value string) Option {
	return func(client *Client) {
		client.inner = opencode.NewClient(client.apiKey, opencode.WithBaseURL(value))
		client.inner.SetRetryPolicy(client.retryPolicy)
	}
}

// WithModel sets the default model.
func WithModel(value string) Option {
	return func(client *Client) {
		client.model = strings.TrimSpace(value)
	}
}

// WithHTTPClient sets the HTTP transport.
func WithHTTPClient(value *http.Client) Option {
	return func(client *Client) {
		client.inner = opencode.NewClient(client.apiKey,
			opencode.WithBaseURL(client.inner.BaseURL()),
			opencode.WithHTTPClient(value),
		)
		client.inner.SetRetryPolicy(client.retryPolicy)
	}
}

// WithRetryPolicy configures retries for transient responses.
func WithRetryPolicy(value opencode.RetryPolicy) Option {
	return func(client *Client) {
		client.retryPolicy = value
		client.inner.SetRetryPolicy(value)
	}
}

// Client talks to the OpenAI chat-completions endpoint. The OpenCode client
// supplies the common JSON and streaming parser, while this wrapper owns the
// provider identity and defaults.
type Client struct {
	apiKey      string
	model       string
	inner       *opencode.Client
	retryPolicy opencode.RetryPolicy
}

// NewClient returns an OpenAI client using the standard v1 endpoint.
func NewClient(apiKey string, opts ...Option) *Client {
	policy := opencode.DefaultRetryPolicy()
	client := &Client{
		apiKey:      apiKey,
		model:       DefaultModelID,
		retryPolicy: policy,
		inner: opencode.NewClient(apiKey,
			opencode.WithBaseURL(DefaultBaseURL),
			opencode.WithModel(DefaultModelID),
		),
	}
	for _, opt := range opts {
		opt(client)
	}
	client.inner.SetRetryPolicy(client.retryPolicy)
	return client
}

// Chat sends one chat-completions request.
func (client *Client) Chat(ctx context.Context, request opencode.ChatRequest) (*opencode.ChatResponse, error) {
	request = client.withDefaultModel(request)
	return client.inner.Chat(ctx, request)
}

// ChatStream sends one streaming chat-completions request.
func (client *Client) ChatStream(
	ctx context.Context,
	request opencode.ChatRequest,
	onDelta func(opencode.Delta) error,
) (*opencode.ChatResponse, error) {
	request = client.withDefaultModel(request)
	return client.inner.ChatStream(ctx, request, onDelta)
}

func (client *Client) withDefaultModel(request opencode.ChatRequest) opencode.ChatRequest {
	if strings.TrimSpace(request.Model) == "" {
		request.Model = client.model
	}
	return request
}

// Model returns the configured default model.
func (client *Client) Model() string { return client.model }

// BaseURL returns the configured API base.
func (client *Client) BaseURL() string { return client.inner.BaseURL() }

// HTTP returns the transport used by the client.
func (client *Client) HTTP() *http.Client { return client.inner.HTTP() }

// RetryPolicy returns the active retry policy.
func (client *Client) RetryPolicy() opencode.RetryPolicy { return client.retryPolicy }

// SetRetryPolicy changes the active retry policy.
func (client *Client) SetRetryPolicy(policy opencode.RetryPolicy) {
	client.retryPolicy = policy
	client.inner.SetRetryPolicy(policy)
}

// ModelInfos reads the OpenAI model catalog and stamps OpenAI routes.
func (client *Client) ModelInfos(ctx context.Context) ([]opencode.ModelInfo, error) {
	infos, err := client.inner.ModelInfos(ctx)
	if err != nil {
		return nil, err
	}
	for index := range infos {
		infos[index].Provider = ProviderName
		infos[index].Endpoint = opencode.ChatURL(client.BaseURL())
	}
	return infos, nil
}

// FreeModelInfos has no OpenAI-specific free-model route.
func (client *Client) FreeModelInfos(context.Context) ([]opencode.ModelInfo, error) {
	return nil, nil
}

// Usage is not exposed by the OpenAI models endpoint.
func (client *Client) Usage(context.Context) (opencode.BillingUsage, error) {
	return opencode.BillingUsage{}, nil
}
