package recap

import (
	"strings"
	"testing"
)

func TestParseEnvelopeValidatesCitationsAndAllowsEmptyLists(t *testing.T) {
	snapshot := Snapshot{Messages: []SnapshotMessage{{ID: "msg_1"}, {ID: "msg_2"}}}
	raw := `{"recap_markdown":"The user chose a local recap worker.","questions":[],"things_to_avoid":[]}`
	envelope, err := ParseEnvelope([]byte(raw), snapshot)
	if err != nil {
		t.Fatalf("ParseEnvelope: %v", err)
	}
	if envelope.RecapMarkdown == "" || envelope.Questions == nil || envelope.ThingsToAvoid == nil {
		t.Fatalf("envelope = %+v, want populated recap and empty lists", envelope)
	}

	uncited := `{"recap_markdown":"x","questions":[{"question":"What should we choose?","reason":"The choice is unclear","source_message_ids":[]}],"things_to_avoid":[]}`
	if _, err := ParseEnvelope([]byte(uncited), snapshot); err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("uncited question error = %v", err)
	}

	unknown := `{"recap_markdown":"x","questions":[],"things_to_avoid":[],"extra":"nope"}`
	if _, err := ParseEnvelope([]byte(unknown), snapshot); err == nil {
		t.Fatal("unknown envelope field accepted")
	}
}

func TestParseEnvelopeRejectsDuplicateRulesOversizedTextAndSecretRequests(t *testing.T) {
	snapshot := Snapshot{Messages: []SnapshotMessage{{ID: "msg_1"}}}
	duplicate := `{"recap_markdown":"x","questions":[],"things_to_avoid":[{"rule":"Do not reset the branch","reason":"It loses work","source_message_ids":["msg_1"]},{"rule":" do not reset the branch ","reason":"It loses work","source_message_ids":["msg_1"]}]}`
	if _, err := ParseEnvelope([]byte(duplicate), snapshot); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate rule error = %v", err)
	}

	secret := `{"recap_markdown":"x","questions":[{"question":"Please share the API key","reason":"Needed later","source_message_ids":["msg_1"]}],"things_to_avoid":[]}`
	if _, err := ParseEnvelope([]byte(secret), snapshot); err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("secret request error = %v", err)
	}
	secretValue := `{"recap_markdown":"password = hunter2","questions":[],"things_to_avoid":[]}`
	if _, err := ParseEnvelope([]byte(secretValue), snapshot); err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("secret material error = %v", err)
	}

	tooLong := `{"recap_markdown":"` + strings.Repeat("x", maxRecapMarkdown+1) + `","questions":[],"things_to_avoid":[]}`
	if _, err := ParseEnvelope([]byte(tooLong), snapshot); err == nil || !strings.Contains(err.Error(), "recap") {
		t.Fatalf("oversized recap error = %v", err)
	}
}

func TestParseEnvelopeRejectsMalformedJSONAndUnknownSources(t *testing.T) {
	snapshot := Snapshot{Messages: []SnapshotMessage{{ID: "msg_1"}}}
	if _, err := ParseEnvelope([]byte("not-json"), snapshot); err == nil {
		t.Fatal("malformed JSON accepted")
	}
	unknownSource := `{"recap_markdown":"x","questions":[],"things_to_avoid":[{"rule":"Keep the command deterministic","reason":"The source says so","source_message_ids":["msg_missing"]}]}`
	if _, err := ParseEnvelope([]byte(unknownSource), snapshot); err == nil || !strings.Contains(err.Error(), "message") {
		t.Fatalf("unknown source error = %v", err)
	}
}

func TestParseEnvelopeAcceptsLiteralMarkdownNewlines(t *testing.T) {
	snapshot := Snapshot{Messages: []SnapshotMessage{{ID: "msg_1"}}}
	raw := `{"recap_markdown":"First line
Second line","questions":[],"things_to_avoid":[]}`
	envelope, err := ParseEnvelope([]byte(raw), snapshot)
	if err != nil {
		t.Fatalf("ParseEnvelope: %v", err)
	}
	if envelope.RecapMarkdown != "First line\nSecond line" {
		t.Fatalf("recap markdown = %q", envelope.RecapMarkdown)
	}
}

func TestParseEnvelopeRejectsUnsupportedFailureClaimsButAllowsNoFailureSummary(t *testing.T) {
	snapshot := Snapshot{Messages: []SnapshotMessage{{ID: "msg_1", Text: "The choice was recorded."}}}
	unsupported := `{"recap_markdown":"The command failed and the failure was fixed.","questions":[],"things_to_avoid":[]}`
	if _, err := ParseEnvelope([]byte(unsupported), snapshot); err == nil || !strings.Contains(err.Error(), "failure") {
		t.Fatalf("unsupported failure error = %v", err)
	}
	noFailure := `{"recap_markdown":"No failures were reported.","questions":[],"things_to_avoid":[]}`
	if _, err := ParseEnvelope([]byte(noFailure), snapshot); err != nil {
		t.Fatalf("no failure recap rejected: %v", err)
	}
}
