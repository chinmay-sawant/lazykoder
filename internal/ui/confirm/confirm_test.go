package confirm

import (
	"strings"
	"testing"

	"charm.land/bubbletea/v2"
)

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			for i += 2; i < len(s); i++ {
				if s[i] >= 0x40 && s[i] <= 0x7e {
					i++
					break
				}
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func TestViewSubAgent(t *testing.T) {
	m := New("lint-fix", "sub-agent")
	v := stripANSI(m.View())
	if !strings.Contains(v, "Delete lint-fix (sub-agent)?") {
		t.Errorf("View() = %q, want it to contain %q", v, "Delete lint-fix (sub-agent)?")
	}
	if !strings.Contains(v, "y confirm") {
		t.Errorf("View() = %q, want it to contain %q", v, "y confirm")
	}
	if !strings.Contains(v, "y confirm  •  n cancel") {
		t.Errorf("View() = %q, want it to contain the exact hint %q", v, "y confirm  •  n cancel")
	}
}

func TestViewRm(t *testing.T) {
	m := New("rm -rf /tmp/x", "rm -rf")
	v := stripANSI(m.View())
	if !strings.Contains(v, "Delete rm -rf /tmp/x (rm -rf)?") {
		t.Errorf("View() = %q, want it to contain %q", v, "Delete rm -rf /tmp/x (rm -rf)?")
	}
}

func TestYKeyAllows(t *testing.T) {
	for _, code := range []rune{'y', 'Y'} {
		m := New("lint-fix", "sub-agent")
		m, cmd := m.Update(tea.KeyPressMsg{Code: code})
		res := m.Result()
		if res == nil {
			t.Fatalf("Update(%q): Result() = nil, want non-nil", code)
		}
		if !res.Allow {
			t.Errorf("Update(%q): Result().Allow = false, want true", code)
		}
		msg := cmd()
		if r, ok := msg.(ResultMsg); !ok || !r.Allow {
			t.Errorf("Update(%q): cmd() = %#v, want ResultMsg{Allow:true}", code, msg)
		}
	}
}

func TestCancelKeysDeny(t *testing.T) {
	for _, tc := range []struct {
		name string
		code rune
	}{
		{"n", 'n'},
		{"N", 'N'},
		{"esc", tea.KeyEsc},
		{"q", 'q'},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New("lint-fix", "sub-agent")
			m, cmd := m.Update(tea.KeyPressMsg{Code: tc.code})
			res := m.Result()
			if res == nil {
				t.Fatalf("Update(%q): Result() = nil, want non-nil", tc.code)
			}
			if res.Allow {
				t.Errorf("Update(%q): Result().Allow = true, want false", tc.code)
			}
			msg := cmd()
			if r, ok := msg.(ResultMsg); !ok || r.Allow {
				t.Errorf("Update(%q): cmd() = %#v, want ResultMsg{Allow:false}", tc.code, msg)
			}
		})
	}
}

func TestIgnoredKeys(t *testing.T) {
	before := New("lint-fix", "sub-agent")
	beforeView := before.View()
	for _, tc := range []struct {
		name string
		code rune
	}{
		{"enter", tea.KeyEnter},
		{"j", 'j'},
		{"k", 'k'},
		{"up", tea.KeyUp},
		{"down", tea.KeyDown},
		{"space", tea.KeySpace},
		{"space rune", ' '},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, cmd := before.Update(tea.KeyPressMsg{Code: tc.code})
			if m.Result() != nil {
				t.Errorf("Update(%q): Result() = %v, want nil", tc.code, m.Result())
			}
			if cmd != nil {
				t.Errorf("Update(%q): cmd = %v, want nil", tc.code, cmd)
			}
			if v := m.View(); v != beforeView {
				t.Errorf("Update(%q): View changed:\nbefore: %q\nafter:  %q", tc.code, beforeView, v)
			}
		})
	}
}

func TestCtrlCQuits(t *testing.T) {
	m := New("lint-fix", "sub-agent")
	m, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if m.Result() != nil {
		t.Errorf("ctrl+c: Result() = %v, want nil (never confirm)", m.Result())
	}
	if cmd == nil {
		t.Fatal("ctrl+c: cmd = nil, want tea.Quit")
	}
	if msg := cmd(); msg != (tea.QuitMsg{}) {
		t.Errorf("ctrl+c: cmd() = %#v, want tea.QuitMsg", msg)
	}
}

func TestResolvedIgnoresFurtherKeys(t *testing.T) {
	m := New("lint-fix", "sub-agent")
	m, _ = m.Update(tea.KeyPressMsg{Code: 'y'})
	if m.Result() == nil || !m.Result().Allow {
		t.Fatal("y: Result() should be non-nil with Allow=true")
	}
	for _, code := range []rune{'y', 'n', 'q', tea.KeyEsc} {
		m, cmd := m.Update(tea.KeyPressMsg{Code: code})
		if cmd != nil {
			t.Errorf("Update(%q) after resolve: cmd = %v, want nil", code, cmd)
		}
		if res := m.Result(); res == nil || !res.Allow {
			t.Errorf("Update(%q) after resolve: Result() = %v, want still Allow=true", code, res)
		}
	}
}

func TestInitReturnsNilCmd(t *testing.T) {
	if cmd := New("x", "y").Init(); cmd != nil {
		t.Errorf("Init() = %v, want nil", cmd)
	}
}
