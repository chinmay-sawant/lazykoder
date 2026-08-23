package provider

import "strings"

const (
	// IDOpenCode is the default provider shipped with lazykoder.
	IDOpenCode = "opencode"
	// IDOpenAI identifies the OpenAI chat-completions provider.
	IDOpenAI = "openai"
	// IDGrok identifies the xAI Grok provider.
	IDGrok = "grok"
	// IDCodex identifies the OpenAI Codex model provider.
	IDCodex = "codex"
	// IDXAI identifies the xAI provider.
	IDXAI = "xai"
)

// Descriptor is the user-facing metadata for one selectable provider.
type Descriptor struct {
	ID         string
	Label      string
	AuthMethod AuthMethod
	EnvKey     string
	CLI        string
	BaseURL    string
	Model      string
	Supported  bool
}

var catalog = []Descriptor{
	{ID: IDOpenCode, Label: "OpenCode", AuthMethod: AuthMethodAPIKey, EnvKey: "OPENCODE_API_KEY", Model: "deepseek-v4-flash", Supported: true},
	{ID: IDOpenAI, Label: "OpenAI", AuthMethod: AuthMethodAPIKey, EnvKey: "OPENAI_API_KEY", BaseURL: "https://api.openai.com/v1", Model: "gpt-4.1-mini", Supported: true},
	{ID: IDGrok, Label: "Grok", AuthMethod: AuthMethodGrok, CLI: "grok", Model: "grok-4.6", Supported: true},
	{ID: IDCodex, Label: "Codex", AuthMethod: AuthMethodCodex, CLI: "codex", Supported: true},
	{ID: IDXAI, Label: "xAI", AuthMethod: AuthMethodAPIKey, EnvKey: "XAI_API_KEY", BaseURL: "https://api.x.ai/v1", Model: "grok-4.6", Supported: true},
}

// Descriptors returns the selectable provider catalog in display order.
func Descriptors() []Descriptor {
	return append([]Descriptor(nil), catalog...)
}

// IDs returns the canonical provider IDs in display order.
func IDs() []string {
	ids := make([]string, 0, len(catalog))
	for _, descriptor := range catalog {
		ids = append(ids, descriptor.ID)
	}
	return ids
}

// DescriptorFor finds a provider by its canonical ID.
func DescriptorFor(id string) (Descriptor, bool) {
	id, ok := canonicalID(id)
	if !ok {
		return Descriptor{}, false
	}
	for _, descriptor := range catalog {
		if descriptor.ID == id {
			return descriptor, true
		}
	}
	return Descriptor{}, false
}

// DefaultModel returns the catalog default used when a child model override
// is empty. An empty value delegates to a provider-owned live default.
func DefaultModel(id string) string {
	descriptor, ok := DescriptorFor(id)
	if !ok {
		return catalog[0].Model
	}
	return descriptor.Model
}

// Normalize returns a canonical known provider ID. Unknown values fall back
// to the OpenCode default so malformed settings cannot select a random route.
func Normalize(id string) string {
	if id, ok := canonicalID(id); ok {
		return id
	}
	return IDOpenCode
}

func canonicalID(id string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case IDOpenCode, "opencode-go", "opencode-zen":
		return IDOpenCode, true
	case IDOpenAI, "openei", "open-ai":
		return IDOpenAI, true
	case IDGrok:
		return IDGrok, true
	case IDCodex:
		return IDCodex, true
	case IDXAI, "x-ai":
		return IDXAI, true
	default:
		return "", false
	}
}
