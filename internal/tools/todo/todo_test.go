package todo

import (
	"strings"
	"testing"
)

func TestRunReplaceAll(t *testing.T) {
	raw := `{"todos":[
		{"content":"scaffold","status":"completed"},
		{"content":"wire tools","status":"in_progress"},
		{"content":"tests","status":"pending"}
	]}`
	res, err := Run(raw)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(res.Items))
	}
	if res.Items[0].Status != "completed" || res.Items[1].Status != "in_progress" {
		t.Fatalf("statuses = %+v", res.Items)
	}
	if !strings.Contains(res.Output, "3") {
		t.Fatalf("output = %q", res.Output)
	}
}

func TestUnknownStatusMapsToPending(t *testing.T) {
	res, err := Run(`{"todos":[{"content":"x","status":"weird"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Items[0].Status != "pending" {
		t.Fatalf("status = %q, want pending", res.Items[0].Status)
	}
}

func TestEmptyContentSkipped(t *testing.T) {
	res, err := Run(`{"todos":[{"content":"  ","status":"pending"},{"content":"keep","status":"done"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 || res.Items[0].Content != "keep" || res.Items[0].Status != "completed" {
		t.Fatalf("items = %+v", res.Items)
	}
}
