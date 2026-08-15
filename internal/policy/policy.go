package policy

import (
	"path/filepath"
	"strings"
)

// Action is the gate decision for a command.
type Action int

const (
	// ActionDeny refuses execution outright.
	ActionDeny Action = iota
	// ActionAllow runs the command without confirmation.
	ActionAllow
	// ActionAsk requires a human confirm before the command runs.
	ActionAsk
)

// Decision is the outcome of Classify.
type Decision struct {
	Action      Action
	Destructive bool
	Reason      string
}

var deleteBinaries = []string{"rm", "rmdir", "unlink", "shred"}

var prefixWrappers = []string{"sudo", "env", "command"}

var deleteWrappers = []string{"git", "xargs", "sudo", "env", "command"}

// Classify scans the whole command string, token by token (shell-ish whitespace split),
// including wrapped forms: sudo rm, env rm, command rm, /bin/rm, xargs rm,
// find ... -exec rm {} +, find . -delete.
func Classify(command string) Decision {
	segs := segments(command)
	if len(segs) == 0 {
		return Decision{Action: ActionAsk, Reason: "empty command"}
	}
	name := ""
	for _, seg := range segs {
		if n := segmentDeleteName(seg); n != "" {
			name = n
			break
		}
	}
	if name == "" {
		return Decision{Action: ActionAllow}
	}
	destructive := hasRecursiveFlag(segs)
	reason := name + " detected"
	if destructive {
		reason = "recursive delete detected"
	}
	return Decision{Action: ActionAsk, Destructive: destructive, Reason: reason}
}

func segments(command string) [][]string {
	s := strings.ReplaceAll(command, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\\\n", "\n")
	s = strings.ReplaceAll(s, "|", "\n")
	s = strings.ReplaceAll(s, "&", "\n")
	s = strings.ReplaceAll(s, ";", "\n")
	var segs [][]string
	for _, line := range strings.Split(s, "\n") {
		if toks := strings.Fields(line); len(toks) > 0 {
			segs = append(segs, toks)
		}
	}
	return segs
}

func segmentDeleteName(seg []string) string {
	if prog := programToken(seg); prog != "" {
		if name := deleteName(prog); name != "" {
			return name
		}
	}
	for i := 1; i < len(seg); i++ {
		tok := seg[i]
		prev := seg[i-1]
		if isOneOf(filepath.Base(prev), deleteWrappers) || prev == "-exec" || prev == "-execdir" {
			if name := deleteName(tok); name != "" {
				return name
			}
		}
		if tok == "-delete" || tok == "delete" {
			for j := 0; j < i; j++ {
				if filepath.Base(seg[j]) == "find" {
					return "find -delete"
				}
			}
		}
	}
	return ""
}

func programToken(seg []string) string {
	for _, tok := range seg {
		base := filepath.Base(tok)
		if isOneOf(base, prefixWrappers) {
			continue
		}
		return tok
	}
	return ""
}

func deleteName(tok string) string {
	base := filepath.Base(tok)
	if isOneOf(base, deleteBinaries) {
		return base
	}
	return ""
}

func hasRecursiveFlag(segs [][]string) bool {
	for _, seg := range segs {
		for _, tok := range seg {
			if tok == "-exec" || tok == "-execdir" {
				continue
			}
			if strings.HasPrefix(tok, "-") && strings.Contains(strings.ToLower(tok), "r") {
				return true
			}
		}
	}
	return false
}

func isOneOf(s string, set []string) bool {
	for _, e := range set {
		if s == e {
			return true
		}
	}
	return false
}
