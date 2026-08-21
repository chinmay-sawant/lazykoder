package agent

import (
	"strings"
	"testing"
)

func TestNeedsCompactUnknownWindowSkips(t *testing.T) {
	if NeedsCompact(500_000, 0, DefaultCompactPercent) {
		t.Fatal("unknown window should not compact")
	}
}

func TestNeedsCompact256kTo1MDoesNotTrigger(t *testing.T) {
	estimate := int64(80_000)
	if NeedsCompact(estimate, 1_000_000, DefaultCompactPercent) {
		t.Fatal("80k on a 1M window should not compact")
	}
}

func TestNeedsCompact1MFillOn256kTriggers(t *testing.T) {
	estimate := int64(400_000)
	if !NeedsCompact(estimate, 256_000, DefaultCompactPercent) {
		t.Fatal("400k on a 256k window should compact")
	}
}

func TestNeedsCompactAt80Percent(t *testing.T) {
	window := int64(1_000_000)
	if NeedsCompact(800_000, window, 80) {
		t.Fatal("exactly 80% should not compact (trigger is strictly greater)")
	}
	if !NeedsCompact(800_001, window, 80) {
		t.Fatal("just over 80% of 1M should compact")
	}
	if NeedsCompact(145_000, window, 80) {
		t.Fatal("145k on 1M is 14.5%, should not compact")
	}
	if !NeedsCompact(210_000, 256_000, 80) {
		t.Fatal("210k on 256k is above 80% (204800)")
	}
}

func TestPickSummarizer1MTo256kUsesOutgoing(t *testing.T) {
	outgoing := ModelRef{ID: "deepseek-large", Context: 1_000_000}
	incoming := ModelRef{ID: "small-model", Context: 256_000}
	got := PickSummarizer(outgoing, incoming, 400_000, DefaultSummarizerReserve)
	if got.ID != outgoing.ID {
		t.Fatalf("summarizer = %q, want outgoing %q", got.ID, outgoing.ID)
	}
}

func TestPickSummarizerFitsIncomingKeepsIncoming(t *testing.T) {
	outgoing := ModelRef{ID: "old", Context: 1_000_000}
	incoming := ModelRef{ID: "new", Context: 256_000}
	got := PickSummarizer(outgoing, incoming, 80_000, DefaultSummarizerReserve)
	if got.ID != incoming.ID {
		t.Fatalf("summarizer = %q, want incoming %q", got.ID, incoming.ID)
	}
}

func TestPruneEnoughToSkipLLM(t *testing.T) {
	oldBody := strings.Repeat("x", 80_000)
	tailBody := "recent-tool-output"
	msgs := []ChatMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "ok"},
		{Role: "tool", ToolCallID: "t1", Content: oldBody},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "still going"},
		{Role: "tool", ToolCallID: "t2", Content: oldBody},
		{Role: "user", Content: "third"},
		{Role: "assistant", Content: "tail"},
		{Role: "tool", ToolCallID: "t3", Content: tailBody},
	}
	pruned := PruneToolOutputs(msgs, 200)
	if pruned[2].Content != PrunedToolPlaceholder {
		t.Fatalf("old tool[2] = %q, want placeholder", prunePreview(pruned[2].Content))
	}
	// Last two user turns start at "second", so tool[5] stays.
	if pruned[5].Content == PrunedToolPlaceholder {
		t.Fatal("tool after the protected user turns should stay")
	}
	if pruned[8].Content != tailBody {
		t.Fatalf("tail tool cleared: %q", prunePreview(pruned[8].Content))
	}
	if EstimateMessages(pruned) >= EstimateMessages(msgs) {
		t.Fatalf("prune did not shrink estimate: %d vs %d", EstimateMessages(pruned), EstimateMessages(msgs))
	}
	if NeedsCompact(EstimateMessages(pruned), 256_000, DefaultCompactPercent) {
		t.Fatal("pruned estimate should fit a 256k window")
	}
}

func TestEncodeParseCompactRoundTrip(t *testing.T) {
	env := CompactEnvelope{
		Summary:            "handoff",
		TailStartMessageID: "msg_1",
		FromModel:          "big",
		ToModel:            "small",
		FromWindow:         1_000_000,
		ToWindow:           256_000,
		Reason:             CompactReasonShrink,
		TokensAfter:        21_000,
	}
	got := ParseCompactText(EncodeCompactText(env))
	if got.Summary != env.Summary || got.TailStartMessageID != env.TailStartMessageID {
		t.Fatalf("got %+v", got)
	}
	if got.FromWindow != env.FromWindow || got.Reason != env.Reason || got.TokensAfter != env.TokensAfter {
		t.Fatalf("got %+v", got)
	}
	plain := ParseCompactText("just a summary")
	if plain.Summary != "just a summary" {
		t.Fatalf("plain = %+v", plain)
	}
}

func TestEstimateTokensEmpty(t *testing.T) {
	if EstimateTokens("") != 0 {
		t.Fatal("empty text should estimate 0")
	}
}

func prunePreview(s string) string {
	if len(s) > 40 {
		return s[:40] + "…"
	}
	return s
}
