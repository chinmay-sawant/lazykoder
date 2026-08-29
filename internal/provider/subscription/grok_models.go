package subscription

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

func grokModelCatalog(ctx context.Context) (ModelCatalog, error) {
	cmd := exec.CommandContext(ctx, "grok", "models")
	cmd.Env = withoutEnv(os.Environ(), "XAI_API_KEY")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return ModelCatalog{}, fmt.Errorf("Grok model catalog: %w", commandError(err, stderr.String()))
	}
	return parseGrokModelCatalog(string(output))
}

func parseGrokModelCatalog(raw string) (ModelCatalog, error) {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	models := make([]opencode.ModelInfo, 0)
	seen := make(map[string]struct{})
	defaultModel := ""
	inAvailableModels := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "default model:") {
			defaultModel = strings.TrimSpace(line[len("Default model:"):])
			continue
		}
		if strings.HasPrefix(lower, "available models") {
			inAvailableModels = true
			continue
		}
		if !inAvailableModels {
			continue
		}
		id, markedDefault := parseGrokModelLine(line)
		if id == "" {
			continue
		}
		if markedDefault && defaultModel == "" {
			defaultModel = id
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, opencode.ModelInfo{
			ID:       id,
			Provider: providerGrok,
			Endpoint: opencode.ChatURL("cli://" + providerGrok),
			Variants: grokReasoningEfforts(),
		})
	}
	if err := scanner.Err(); err != nil {
		return ModelCatalog{}, fmt.Errorf("read Grok model catalog: %w", err)
	}
	if len(models) == 0 {
		return ModelCatalog{}, fmt.Errorf("Grok model catalog is empty")
	}
	if defaultModel == "" {
		defaultModel = models[0].ID
	}
	return ModelCatalog{Models: models, Default: defaultModel}, nil
}

func grokReasoningEfforts() []string {
	return []string{"low", "medium", "high", "xhigh"}
}

func parseGrokModelLine(line string) (string, bool) {
	if len(line) < 2 || (line[0] != '-' && line[0] != '*') {
		return "", false
	}
	markedDefault := line[0] == '*'
	model := strings.TrimSpace(line[1:])
	const defaultSuffix = "(default)"
	if strings.HasSuffix(strings.ToLower(model), defaultSuffix) {
		model = strings.TrimSpace(model[:len(model)-len(defaultSuffix)])
		markedDefault = true
	}
	return model, markedDefault
}
