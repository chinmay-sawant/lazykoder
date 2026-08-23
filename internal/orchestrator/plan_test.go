package orchestrator

import "testing"

func TestParseBoundsAndNormalizesModelClass(t *testing.T) {
	plan, err := Parse(`{"goal":"audit packages","subtasks":[{"id":"1","name":"inspect","prompt":"Inspect the packages and report findings.","role":"explore"}]}`, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Subtasks) != 1 || plan.Subtasks[0].ModelClass != "flash" {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestParseRejectsMalformedPlan(t *testing.T) {
	for _, raw := range []string{
		`{"goal":"audit","subtasks":[{"id":"1","name":"x","prompt":"y","role":"unknown"}]}`,
		`{"goal":"audit","subtasks":[{"id":"1","name":"x","prompt":"y","role":"explore"},{"id":"1","name":"z","prompt":"q","role":"explore"}]}`,
	} {
		if _, err := Parse(raw, 4); err == nil {
			t.Fatalf("Parse(%s) succeeded", raw)
		}
	}
}

func TestLooksDecomposableAvoidsShortRequests(t *testing.T) {
	if LooksDecomposable("fix this") {
		t.Fatal("short request should not trigger planning")
	}
	if !LooksDecomposable("audit these packages and report the important findings") {
		t.Fatal("multi-part audit should trigger planning")
	}
}
