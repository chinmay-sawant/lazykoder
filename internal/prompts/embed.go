// Package prompts holds embedded system prompts shipped with the binary.
package prompts

import (
	"embed"
	"fmt"
	"sync"
)

//go:embed *.md
var files embed.FS

var cache sync.Map

// Must returns the named embedded prompt. A missing name panics so a
// typo fails at first use instead of sending an empty instruction.
func Must(name string) string {
	if v, ok := cache.Load(name); ok {
		return v.(string)
	}
	raw, err := files.ReadFile(name)
	if err != nil {
		panic(fmt.Sprintf("prompts: missing %q: %v", name, err))
	}
	text := string(raw)
	cache.Store(name, text)
	return text
}
