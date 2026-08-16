package policy

import (
	"bytes"
	"fmt"
	"net"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Action is the gate decision for a command.
type Action int

const (
	ActionDeny Action = iota
	ActionAllow
	ActionAsk
)

type Decision struct {
	Action      Action
	Destructive bool
	Reason      string
}

var deleteBinaries = map[string]bool{"rm": true, "rmdir": true, "unlink": true, "shred": true}
var shellBinaries = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true}

// Classify preserves the historical default: commands are allowed except
// destructive commands, which require confirmation.
func Classify(command string) Decision { return ClassifyWithAllowlist(command, nil, false) }

// ClassifyWithAllowlist parses shell syntax structurally. If enabled, every
// executable must be present in allowlist; malformed or unknown syntax fails
// closed into confirmation. The parser never executes the command.
func ClassifyWithAllowlist(command string, allowlist []string, enabled bool) Decision {
	if strings.Contains(command, "\\\n") {
		return Decision{Action: ActionAsk, Reason: "ambiguous shell syntax"}
	}
	if strings.TrimSpace(command) == "" {
		return Decision{Action: ActionAsk, Reason: "empty command"}
	}
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "command")
	if err != nil {
		return Decision{Action: ActionAsk, Reason: fmt.Sprintf("invalid shell syntax: %v", err)}
	}
	allowed := make(map[string]bool)
	for _, name := range allowlist {
		allowed[filepath.Base(strings.TrimSpace(name))] = true
	}
	foundDelete := false
	recursive := false
	unknown := false
	syntax.Walk(file, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		args := make([]string, 0, len(call.Args))
		for _, word := range call.Args {
			args = append(args, wordValue(word))
		}
		program := filepath.Base(args[0])
		checkDelete := func(i int) {
			if i < 0 || i >= len(args) {
				return
			}
			name := filepath.Base(args[i])
			if deleteBinaries[name] {
				foundDelete = true
				for _, arg := range args[i+1:] {
					if strings.HasPrefix(arg, "-") && strings.Contains(strings.ToLower(arg), "r") {
						recursive = true
					}
				}
			}
		}
		if deleteBinaries[program] {
			checkDelete(0)
		}
		if program == "git" {
			for i := 1; i < len(args); i++ {
				if args[i] == "rm" {
					checkDelete(i)
					break
				}
			}
		}
		if program == "find" {
			execSeen := false
			for _, arg := range args[1:] {
				if arg == "-delete" || arg == "delete" {
					foundDelete = true
				}
				if arg == "-exec" || arg == "-execdir" {
					execSeen = true
					continue
				}
				if execSeen && deleteBinaries[filepath.Base(arg)] {
					checkDelete(indexOf(args, arg))
					execSeen = false
				}
			}
		}
		if program != "echo" && program != "printf" && program != "ls" {
			for i := 1; i < len(args); i++ {
				if deleteBinaries[filepath.Base(args[i])] {
					checkDelete(i)
				}
			}
		}
		if program == "sudo" || program == "env" || program == "command" || program == "xargs" {
			for i := 1; i < len(args); i++ {
				if deleteBinaries[filepath.Base(args[i])] {
					checkDelete(i)
					break
				}
			}
		}
		if enabled && program != "" && !allowed[program] && !shellBinaries[program] {
			unknown = true
		}
		for i := 1; i+1 < len(args); i++ {
			if shellBinaries[program] && args[i] == "-c" {
				d := ClassifyWithAllowlist(args[i+1], allowlist, enabled)
				if d.Action == ActionAsk || d.Destructive {
					foundDelete = foundDelete || d.Action == ActionAsk || d.Destructive
					recursive = recursive || d.Destructive
					if d.Action == ActionAsk {
						unknown = true
					}
				}
			}
		}

		return true
	})
	if foundDelete {
		reason := "delete command detected"
		if recursive {
			reason = "recursive delete detected"
		}
		return Decision{Action: ActionAsk, Destructive: recursive, Reason: reason}
	}
	if unknown {
		return Decision{Action: ActionAsk, Reason: "command is not in the configured allowlist"}
	}
	return Decision{Action: ActionAllow}
}

func indexOf(args []string, value string) int {
	for i, v := range args {
		if v == value {
			return i
		}
	}
	return -1
}

func wordValue(w *syntax.Word) string {
	if v := w.Lit(); v != "" {
		return v
	}
	var b bytes.Buffer
	for _, p := range w.Parts {
		switch x := p.(type) {
		case *syntax.SglQuoted:
			b.WriteString(x.Value)
		case *syntax.DblQuoted:
			for _, q := range x.Parts {
				if l, ok := q.(*syntax.Lit); ok {
					b.WriteString(l.Value)
				}
			}
		case *syntax.Lit:
			b.WriteString(x.Value)
		}
	}
	return b.String()
}

// PrivateOrLoopback rejects local, private, link-local, multicast and metadata hosts.
func PrivateOrLoopback(host string) bool {
	h := host
	if strings.Contains(h, ":") {
		if x, _, e := net.SplitHostPort(h); e == nil {
			h = x
		}
	}
	if ip := net.ParseIP(strings.Trim(h, "[]")); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
	}
	return strings.EqualFold(h, "localhost") || strings.HasSuffix(strings.ToLower(h), ".localhost") || strings.EqualFold(h, "metadata.google.internal")
}
