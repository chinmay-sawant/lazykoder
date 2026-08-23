package subscription

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

const (
	codexInitializeRequestID = 1
	codexModelListRequestID  = 2
	codexModelListLimit      = 100
	codexPreferredModel      = "gpt-5.6-luna"
	codexPreferredVariant    = "low"
)

type codexRPCClient struct {
	input  io.Writer
	output *bufio.Reader
}

type codexRPCResponse struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *codexRPCError  `json:"error"`
}

type codexRPCError struct {
	Message string `json:"message"`
}

type codexModelList struct {
	Data []codexModel `json:"data"`
}

type codexModel struct {
	ID                        string                       `json:"id"`
	Model                     string                       `json:"model"`
	Hidden                    bool                         `json:"hidden"`
	IsDefault                 bool                         `json:"isDefault"`
	SupportedReasoningEfforts []codexReasoningEffortOption `json:"supportedReasoningEfforts"`
}

type codexReasoningEffortOption struct {
	ReasoningEffort string `json:"reasoningEffort"`
}

func codexModelCatalog(ctx context.Context) (ModelCatalog, error) {
	cmd := exec.CommandContext(ctx, "codex", "app-server", "--stdio")
	cmd.Env = withoutEnv(os.Environ(), "OPENAI_API_KEY")
	input, err := cmd.StdinPipe()
	if err != nil {
		return ModelCatalog{}, fmt.Errorf("Codex model catalog input: %w", err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return ModelCatalog{}, fmt.Errorf("Codex model catalog output: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return ModelCatalog{}, fmt.Errorf("start Codex model catalog: %w", err)
	}
	defer func() {
		_ = input.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	client := codexRPCClient{input: input, output: bufio.NewReader(output)}
	if _, err := client.call(codexInitializeRequestID, "initialize", map[string]any{
		"clientInfo": map[string]string{"name": "lazykoder", "version": "dev"},
	}); err != nil {
		return ModelCatalog{}, codexCatalogError(err, stderr.String())
	}
	if err := client.notify("initialized", nil); err != nil {
		return ModelCatalog{}, codexCatalogError(err, stderr.String())
	}
	result, err := client.call(codexModelListRequestID, "model/list", map[string]any{
		"limit":         codexModelListLimit,
		"includeHidden": false,
	})
	if err != nil {
		return ModelCatalog{}, codexCatalogError(err, stderr.String())
	}
	return parseCodexModelCatalog(result)
}

func (client codexRPCClient) call(id int, method string, params any) (json.RawMessage, error) {
	if err := json.NewEncoder(client.input).Encode(map[string]any{
		"id":     id,
		"method": method,
		"params": params,
	}); err != nil {
		return nil, fmt.Errorf("write %s request: %w", method, err)
	}
	wantID := []byte(strconv.Itoa(id))
	for {
		line, err := client.output.ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("read %s response: %w", method, err)
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var response codexRPCResponse
		if err := json.Unmarshal(line, &response); err != nil {
			return nil, fmt.Errorf("decode %s response: %w", method, err)
		}
		if !bytes.Equal(bytes.TrimSpace(response.ID), wantID) {
			continue
		}
		if response.Error != nil {
			return nil, fmt.Errorf("%s failed: %s", method, response.Error.Message)
		}
		if len(response.Result) == 0 {
			return nil, fmt.Errorf("%s response is missing result", method)
		}
		return response.Result, nil
	}
}

func (client codexRPCClient) notify(method string, params any) error {
	if err := json.NewEncoder(client.input).Encode(map[string]any{
		"method": method,
		"params": params,
	}); err != nil {
		return fmt.Errorf("write %s notification: %w", method, err)
	}
	return nil
}

func parseCodexModelCatalog(raw json.RawMessage) (ModelCatalog, error) {
	var payload codexModelList
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ModelCatalog{}, fmt.Errorf("decode Codex model catalog: %w", err)
	}
	models := make([]opencode.ModelInfo, 0, len(payload.Data))
	seen := make(map[string]struct{}, len(payload.Data))
	accountDefault := ""
	preferredVisible := false
	for _, model := range payload.Data {
		if model.Hidden {
			continue
		}
		id := strings.TrimSpace(model.Model)
		if id == "" {
			id = strings.TrimSpace(model.ID)
		}
		if id == "" {
			continue
		}
		if model.IsDefault {
			accountDefault = id
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		variants := codexReasoningVariants(model.SupportedReasoningEfforts)
		if id == codexPreferredModel && hasVariant(variants, codexPreferredVariant) {
			preferredVisible = true
		}
		models = append(models, opencode.ModelInfo{
			ID:       id,
			Provider: providerCodex,
			Endpoint: opencode.ChatURL("cli://" + providerCodex),
			Variants: variants,
		})
	}
	if len(models) == 0 {
		return ModelCatalog{}, fmt.Errorf("Codex model catalog is empty")
	}
	if accountDefault == "" {
		return ModelCatalog{}, fmt.Errorf("Codex model catalog has no visible default")
	}
	if preferredVisible {
		return ModelCatalog{
			Models:         models,
			Default:        codexPreferredModel,
			DefaultVariant: codexPreferredVariant,
		}, nil
	}
	return ModelCatalog{Models: models, Default: accountDefault}, nil
}

func codexReasoningVariants(options []codexReasoningEffortOption) []string {
	variants := make([]string, 0, len(options))
	seen := make(map[string]struct{}, len(options))
	for _, option := range options {
		variant := strings.TrimSpace(option.ReasoningEffort)
		if variant == "" {
			continue
		}
		if _, ok := seen[variant]; ok {
			continue
		}
		seen[variant] = struct{}{}
		variants = append(variants, variant)
	}
	return variants
}

func hasVariant(variants []string, want string) bool {
	for _, variant := range variants {
		if variant == want {
			return true
		}
	}
	return false
}

func codexCatalogError(err error, stderr string) error {
	return fmt.Errorf("Codex model catalog: %w", commandError(err, stderr))
}
