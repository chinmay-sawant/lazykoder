// Package todo implements the model-driven todowrite tool (full-list replace).
package todo

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Item is one checklist row in a todowrite payload.
type Item struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}

// Args is the todowrite tool argument object.
type Args struct {
	Todos []Item `json:"todos"`
}

// Result is the tool outcome.
type Result struct {
	Output string
	Items  []Item
}

// NormalizeStatus maps free-form status strings to the four canonical values.
// Unknown values become pending (never fails the turn).
func NormalizeStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "pending", "todo", "open", "":
		return "pending"
	case "in_progress", "in-progress", "active", "doing", "running":
		return "in_progress"
	case "completed", "done", "complete", "finished", "success":
		return "completed"
	case "cancelled", "canceled", "skipped", "wontfix", "won'tfix":
		return "cancelled"
	default:
		return "pending"
	}
}

// ParseArgs unmarshals and normalizes todowrite arguments.
func ParseArgs(argsJSON string) (Args, error) {
	var a Args
	if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
		return Args{}, fmt.Errorf("todo: invalid arguments: %w", err)
	}
	out := make([]Item, 0, len(a.Todos))
	for _, it := range a.Todos {
		content := strings.TrimSpace(it.Content)
		if content == "" {
			continue
		}
		out = append(out, Item{
			Content: content,
			Status:  NormalizeStatus(it.Status),
		})
	}
	a.Todos = out
	return a, nil
}

// Run validates the list and returns a summary (persist is the caller's job).
func Run(argsJSON string) (Result, error) {
	a, err := ParseArgs(argsJSON)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Output: fmt.Sprintf("todos updated: %d", len(a.Todos)),
		Items:  a.Todos,
	}, nil
}

// SpecName is the tool name advertised to the model.
const SpecName = "todowrite"
