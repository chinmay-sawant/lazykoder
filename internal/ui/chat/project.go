package chat

import (
	"context"
	"fmt"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/db"
)

// projectOpts controls Session -> transcriptItem projection knobs shared by
// main replay and the sub-agent log viewer.
type projectOpts struct {
	// CollapseReasoning starts reasoning cards collapsed (main chat).
	CollapseReasoning bool
	// SkipEmptyText drops blank text parts (sub-agent log).
	SkipEmptyText bool
	// SeedInputHistory collects visible user texts for up-arrow history.
	SeedInputHistory bool
	// IncludeCompactNotes paints compaction checkpoints as note rows.
	IncludeCompactNotes bool
	// CollectUsage returns step-finish parts for the fill/cost meter.
	CollectUsage bool
}

type usageApply struct {
	part       db.Part
	modelID    string
	providerID string
}

type projectResult struct {
	items   []transcriptItem
	history []inputHistoryItem
	usage   []usageApply
	// compactMeter parts update tokensUsed / cache on main replay.
	compactMeter []db.Part
}

// projectSession loads a SessionGraph and projects it into transcript items.
// Callers apply usage/compact meter side effects from the returned slices.
func projectSession(store *db.Store, sessionID string, opts projectOpts) (projectResult, error) {
	var out projectResult
	if store == nil || sessionID == "" {
		return out, nil
	}
	graph, err := store.LoadSessionGraph(context.Background(), sessionID)
	if err != nil {
		return out, err
	}
	for _, entry := range graph.Entries {
		msg := entry.Message
		if !msg.Visible {
			continue
		}
		for _, p := range entry.Parts {
			switch p.Type {
			case "text":
				if p.Text == nil {
					continue
				}
				if opts.SkipEmptyText && *p.Text == "" {
					continue
				}
				if msg.Role == "user" {
					if opts.SeedInputHistory {
						out.history = append(out.history, inputHistoryItem{messageID: msg.ID, text: *p.Text})
					}
					out.items = append(out.items, transcriptItem{
						kind: itemUser, text: *p.Text,
						when: itemTime(msg.TimeCreated, p.TimeCreated), part: p,
					})
				} else {
					out.items = append(out.items, transcriptItem{
						kind: itemAssistant, text: *p.Text,
						when: itemTime(msg.TimeCreated, p.TimeCreated), part: p,
					})
				}
			case "reasoning":
				if p.Text == nil {
					continue
				}
				if opts.SkipEmptyText && *p.Text == "" {
					continue
				}
				out.items = append(out.items, transcriptItem{
					kind: itemReasoning, text: *p.Text, collapsed: opts.CollapseReasoning,
					when: itemTime(msg.TimeCreated, p.TimeCreated), part: p,
				})
			case "plan":
				if p.Text == nil {
					continue
				}
				out.items = append(out.items, transcriptItem{
					kind: itemNote, text: "orchestration plan\n" + *p.Text,
					when: itemTime(msg.TimeCreated, p.TimeCreated), part: p,
				})
			case "tool":
				tool := db.ToolCall{PartID: p.ID}
				if stored, ok := graph.ToolCallsByPart[p.ID]; ok {
					tool = stored
				} else {
					tool.Tool = "tool"
					if p.ToolName != nil {
						tool.Tool = *p.ToolName
					}
					if p.ToolStatus != nil {
						tool.Status = *p.ToolStatus
					}
				}
				when := itemTime(msg.TimeCreated, p.TimeCreated)
				if tool.TimeStart != nil {
					when = *tool.TimeStart
				}
				// Edit cards stay open so the diff is always visible.
				collapsed := tool.Tool != "edit"
				out.items = append(out.items, transcriptItem{
					kind: itemTool, collapsed: collapsed, when: when, tool: tool, part: p,
				})
			case "step-finish":
				if opts.CollectUsage {
					out.usage = append(out.usage, usageApply{
						part:       p,
						modelID:    msg.ModelID,
						providerID: msg.ProviderID,
					})
				}
			case agent.CompactPartType:
				if opts.IncludeCompactNotes {
					out.items = append(out.items, compactNoticeItem(p))
					out.compactMeter = append(out.compactMeter, p)
				}
			}
		}
	}
	return out, nil
}

func compactNoticeItem(p db.Part) transcriptItem {
	text := "context compacted"
	if p.Text != nil {
		env := agent.ParseCompactText(*p.Text)
		if env.FromWindow > 0 && env.ToWindow > 0 && env.FromWindow != env.ToWindow {
			text = fmt.Sprintf("context compacted (%s -> %s)", formatTokens(env.FromWindow), formatTokens(env.ToWindow))
		} else if env.FromModel != "" && env.ToModel != "" && env.FromModel != env.ToModel {
			text = fmt.Sprintf("context compacted (%s -> %s)", env.FromModel, env.ToModel)
		}
	}
	return transcriptItem{kind: itemNote, text: text, part: p}
}
