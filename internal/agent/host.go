package agent

import (
	"context"

	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

// SubagentHost is the small control-plane surface the parent loop needs for
// task tools. When nil, task tools are not advertised and execute as denied.
type SubagentHost interface {
	// Specs returns the task-family tool definitions to advertise.
	Specs() []opencode.ToolSpec
	// Execute runs one management tool. parentSessionID is the parent agent
	// session. status is completed|denied|error. result is JSON for the model;
	// metaJSON is optional metadata for the tool_calls row.
	Execute(ctx context.Context, parentSessionID, name, argsJSON, partID string) (result, metaJSON, status string, err error)
}
