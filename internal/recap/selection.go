package recap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

const (
	maxSelectionCandidates = 12
	maxSelectionFileBytes  = 4 * 1024
	maxSelectionPrompt     = 12 * 1024
	maxSelectionOutput     = 4 * 1024
	maxSelectionQuery      = 600
	maxSelectionSummary    = 800
	maxSelectionTokens     = 300
	maxSummaryLines        = 4
)

// RecapCandidate is the bounded title and summary shown to the selector.
type RecapCandidate struct {
	Path    string
	Summary string
	ModTime int64
}

// RecentRecapCandidates returns the newest recap summaries without exposing
// the full artifact set to the model.
func RecentRecapCandidates(workdir string) ([]RecapCandidate, error) {
	root, err := filepath.Abs(workdir)
	if err != nil {
		return nil, fmt.Errorf("recap: resolve workdir: %w", err)
	}
	recapRoot := filepath.Join(root, "knowledge-base", "recaps")
	candidates := make([]RecapCandidate, 0, maxSelectionCandidates)
	err = filepath.WalkDir(recapRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				return filepath.SkipDir
			}
			return filepath.SkipDir
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return filepath.SkipDir
		}
		file, err := os.Open(path)
		if err != nil {
			return filepath.SkipDir
		}
		raw, readErr := io.ReadAll(io.LimitReader(file, maxSelectionFileBytes))
		_ = file.Close()
		if readErr != nil {
			return filepath.SkipDir
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return filepath.SkipDir
		}
		candidates = append(candidates, RecapCandidate{
			Path:    filepath.ToSlash(relative),
			Summary: recapSummary(string(raw)),
			ModTime: info.ModTime().UnixNano(),
		})
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("recap: list candidates: %w", err)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].ModTime > candidates[j].ModTime
	})
	if len(candidates) > maxSelectionCandidates {
		candidates = candidates[:maxSelectionCandidates]
	}
	return candidates, nil
}

// SelectRecapContext performs one no-tools selection call after local grep
// finds nothing. It returns only candidates whose paths the model selected.
func SelectRecapContext(
	ctx context.Context,
	client Client,
	model string,
	info modelscache.Info,
	query string,
	workdir string,
) (string, error) {
	if client == nil || strings.TrimSpace(model) == "" {
		return "", nil
	}
	candidates, err := RecentRecapCandidates(workdir)
	if err != nil || len(candidates) == 0 {
		return "", err
	}
	var prompt strings.Builder
	prompt.WriteString("Select relevant recap paths for this request. Return only exact paths, one per line, or NONE.\n")
	prompt.WriteString("Request: ")
	prompt.WriteString(truncateString(query, maxSelectionQuery))
	prompt.WriteString("\nCandidates:\n")
	for _, candidate := range candidates {
		fmt.Fprintf(&prompt, "PATH %s\n%s\n", candidate.Path, truncateString(candidate.Summary, maxSelectionSummary))
	}
	if len([]rune(prompt.String())) > maxSelectionPrompt {
		return "", nil
	}
	response, err := client.Chat(ctx, opencode.ChatRequest{
		Model:     model,
		Endpoint:  info.Endpoint,
		Messages:  []opencode.Message{{Role: "user", Content: prompt.String()}},
		MaxTokens: maxSelectionTokens,
	})
	if err != nil {
		return "", err
	}
	if response == nil {
		return "", errors.New("recap: selector returned no response")
	}
	selected := strings.ToLower(response.Content)
	var out strings.Builder
	for _, candidate := range candidates {
		if !strings.Contains(selected, strings.ToLower(candidate.Path)) {
			continue
		}
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		fmt.Fprintf(&out, "%s: %s", candidate.Path, singleLine(candidate.Summary))
	}
	return truncateString(out.String(), maxSelectionOutput), nil
}

func recapSummary(raw string) string {
	lines := strings.Split(raw, "\n")
	parts := make([]string, 0, maxSummaryLines)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "```") {
			continue
		}
		parts = append(parts, line)
		if len(parts) == maxSummaryLines {
			break
		}
	}
	return strings.Join(parts, " ")
}

func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
