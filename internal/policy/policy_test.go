package policy

import "testing"

func TestClassify(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		action      Action
		destructive bool
	}{
		{"rm file", "rm file", ActionAsk, false},
		{"rm -f x", "rm -f x", ActionAsk, false},
		{"rm -rf /tmp/x", "rm -rf /tmp/x", ActionAsk, true},
		{"rm -fr /x", "rm -fr /x", ActionAsk, true},
		{"rm -Rf x", "rm -Rf x", ActionAsk, true},
		{"rm --recursive x", "rm --recursive x", ActionAsk, true},
		{"/bin/rm ./a", "/bin/rm ./a", ActionAsk, false},
		{"sudo rm -rf .", "sudo rm -rf .", ActionAsk, true},
		{"sudo option rm", "sudo -u root rm x", ActionAsk, false},
		{"sudo separator rm", "sudo -- rm x", ActionAsk, false},
		{"env assignment rm", "env FOO=bar rm x", ActionAsk, false},
		{"env option rm", "env -- rm x", ActionAsk, false},
		{"command separator rm", "command -- rm x", ActionAsk, false},
		{"xargs option rm", "xargs -n1 rm", ActionAsk, false},
		{"env rm x", "env rm x", ActionAsk, false},
		{"command rm x", "command rm x", ActionAsk, false},
		{"xargs rm x", "xargs rm x", ActionAsk, false},
		{"find . -exec rm {} +", "find . -exec rm {} +", ActionAsk, false},
		{"find . -exec rm -rf {} +", "find . -exec rm -rf {} +", ActionAsk, true},
		{"find . -execdir rm {} +", "find . -execdir rm {} +", ActionAsk, false},
		{"find . -delete", "find . -delete", ActionAsk, false},
		{"git rm x", "git rm x", ActionAsk, false},
		{"git rm -rf x", "git rm -rf x", ActionAsk, true},
		{"rmdir /tmp/old", "rmdir /tmp/old", ActionAsk, false},
		{"unlink f", "unlink f", ActionAsk, false},
		{"shred f", "shred f", ActionAsk, false},
		{"ls", "ls", ActionAllow, false},
		{"echo room", "echo room", ActionAllow, false},
		{"echo rm", "echo rm", ActionAllow, false},
		{"chmod +x f", "chmod +x f", ActionAllow, false},
		{"go test ./...", "go test ./...", ActionAllow, false},
		{"go version", "go version", ActionAllow, false},
		{"empty", "", ActionAsk, false},
		{"whitespace", "   ", ActionAsk, false},
		{"ls | rm x", "ls | rm x", ActionAsk, false},
		{"go run . && rm x", "go run . && rm x", ActionAsk, false},
		{"backslash continuation", "ls \\\nrm x", ActionAsk, false},
		{"rm after semicolon", "echo hi; rm x", ActionAsk, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dec := Classify(tt.command)
			if dec.Action != tt.action {
				t.Errorf("Classify(%q) Action = %v, want %v", tt.command, dec.Action, tt.action)
			}
			if dec.Destructive != tt.destructive {
				t.Errorf("Classify(%q) Destructive = %v, want %v", tt.command, dec.Destructive, tt.destructive)
			}
			if dec.Action == ActionAsk && dec.Reason == "" {
				t.Errorf("Classify(%q) Reason = \"\", want non-empty for Ask", tt.command)
			}
			if dec.Action == ActionAllow && dec.Reason != "" {
				t.Errorf("Classify(%q) Reason = %q, want empty for Allow", tt.command, dec.Reason)
			}
		})
	}
}
