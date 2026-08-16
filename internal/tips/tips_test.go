package tips

import "testing"

func TestAtCycles(t *testing.T) {
	if len(All) == 0 {
		t.Fatal("tip list is empty")
	}
	for i := 0; i < len(All)*2; i++ {
		got := At(i)
		want := All[i%len(All)]
		if got != want {
			t.Errorf("At(%d) = %q, want %q", i, got, want)
		}
	}
	if At(-1) != All[len(All)-1] {
		t.Errorf("At(-1) = %q, want last tip %q", At(-1), All[len(All)-1])
	}
}

func TestTipsAreUnique(t *testing.T) {
	seen := make(map[string]bool, len(All))
	for _, tip := range All {
		if seen[tip] {
			t.Errorf("duplicate tip %q", tip)
		}
		seen[tip] = true
	}
}
