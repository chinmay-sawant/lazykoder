// Package tips holds the rotating usage hints shown right-aligned on the
// alert row above the input box while the chat is idle. Every Rotation the
// chat model advances one step through All. The list mirrors docs/tips.md:
// keep the two in sync when adding or editing a tip.
package tips

import "time"

// Rotation is how often the chat UI advances to the next tip.
const Rotation = 15 * time.Second

// The tips, one short hint each. Keep entries short so they fit right-aligned
// on the alert row without covering the jump-to-latest icon.
const (
	EnterSend           = "enter sends the prompt"
	ShiftEnterNewline   = "shift+enter inserts a newline"
	EscCancelsTurn      = "esc cancels a running turn"
	EscTwiceClears      = "press esc twice to clear the prompt"
	SlashCommands       = "type / to list commands"
	NewSession          = "/new starts a fresh session"
	SessionsPicker      = "/resume or ctrl+s continues a past session"
	ModelPicker         = "/model switches the chat model"
	VariantPicker       = "/variant sets the reasoning effort"
	SlotSettings        = "/settings opens project defaults (model, agents, compaction, safety)"
	AgentsDrawer        = "/agents opens the sub-agent drawer and logs"
	ContinueSession     = "/continue resumes after a step limit"
	CompactNow          = "/compact summarizes older context now"
	AutoCompactLimit    = "auto-compact fires at 80% of the model window (set in /settings)"
	RefreshModels       = "/refresh reloads the model list"
	HelpOverlay         = "/help or ? shows every shortcut"
	MentionFile         = "type @ to mention a project file"
	ExpandThinking      = "ctrl+p expands thinking, ctrl+e expands tools"
	DragSelectCopy      = "drag across the transcript to select and copy"
	ToolCardClick       = "click a tool card to expand it"
	ModelStatusClick    = "click the model label to switch models"
	SelectAllInput      = "ctrl+a selects the whole input"
	CtrlCCopyQuit       = "ctrl+c clears input, ctrl+a then ctrl+c copies, twice quits"
	HistoryBrowse       = "up/down browse your previous prompts"
	SessionsResume      = "launch is fresh; /resume or ctrl+s loads a past run"
	ProjectInstructions = "AGENTS.md in this folder is sent as system context"
	ToolSuccessDiamond  = "a green diamond means the tool succeeded"
	ToolFailureDiamond  = "a red diamond means the tool failed"
	ScrollbarJump       = "click the scrollbar to jump the transcript"
	JumpToLatest        = "the ▼ above the box jumps to the latest output"
	DestructiveAskFirst = "destructive bash commands always ask first"
	FooterCosts         = "open /status for tokens, cache, cost and tps"
)

// All is the full rotation list shown while idle.
var All = []string{
	EnterSend,
	ShiftEnterNewline,
	EscCancelsTurn,
	EscTwiceClears,
	SlashCommands,
	NewSession,
	SessionsPicker,
	ModelPicker,
	VariantPicker,
	SlotSettings,
	AgentsDrawer,
	ContinueSession,
	CompactNow,
	AutoCompactLimit,
	RefreshModels,
	HelpOverlay,
	MentionFile,
	ExpandThinking,
	DragSelectCopy,
	ToolCardClick,
	ModelStatusClick,
	SelectAllInput,
	CtrlCCopyQuit,
	HistoryBrowse,
	SessionsResume,
	ProjectInstructions,
	ToolSuccessDiamond,
	ToolFailureDiamond,
	ScrollbarJump,
	JumpToLatest,
	DestructiveAskFirst,
	FooterCosts,
}

// At returns the tip at index i, cycling through the list.
func At(i int) string {
	if len(All) == 0 {
		return ""
	}
	i %= len(All)
	if i < 0 {
		i += len(All)
	}
	return All[i]
}
