package chat

import (
	"fmt"
	"strings"
	"testing"

	"github.com/chinmay-sawant/lazykoder/internal/db"
)

func renderBenchmarkModel(assistant, collapsedTools, expandedTools int) Model {
	m := New(Options{})
	m.width = 120
	m.height = 40
	for i := 0; i < assistant; i++ {
		m.items = append(m.items, transcriptItem{kind: itemAssistant, text: "historical assistant response", when: int64(i)})
	}
	toolOutput := strings.Repeat("tool output line\n", 600)
	for i := 0; i < collapsedTools; i++ {
		m.items = append(m.items, transcriptItem{kind: itemTool, collapsed: true, tool: db.ToolCall{
			Tool: "bash", Status: "completed", Output: &toolOutput,
		}})
	}
	for i := 0; i < expandedTools; i++ {
		m.items = append(m.items, transcriptItem{kind: itemTool, collapsed: false, tool: db.ToolCall{
			Tool: "bash", Status: "completed", Output: &toolOutput,
		}})
	}
	m.items = append(m.items, transcriptItem{kind: itemAssistant, text: "live tail", when: 9999})
	m.syncTranscript()
	return m
}

func BenchmarkBuildRenderedItems(b *testing.B) {
	cases := map[string]struct {
		assistant, collapsedTools, expandedTools int
	}{
		"assistant-100":            {assistant: 100},
		"collapsed-tools-100x8k":   {collapsedTools: 100},
		"expanded-tools-20-capped": {expandedTools: 20},
	}
	for name, tc := range cases {
		b.Run(name+"/baseline-full-rebuild", func(b *testing.B) {
			m := renderBenchmarkModel(tc.assistant, tc.collapsedTools, tc.expandedTools)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.items[len(m.items)-1].text = fmt.Sprintf("live tail %08d", i)
				m.renderCache.itemKeys = nil
				m.renderCache.itemRows = nil
				m.renderCache.rows = nil
				m.renderCache.plain = nil
				m.renderCache.fp = 0
				_ = m.renderedItems()
			}
		})

		b.Run(name+"/memo-tail-update", func(b *testing.B) {
			m := renderBenchmarkModel(tc.assistant, tc.collapsedTools, tc.expandedTools)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.items[len(m.items)-1].text = fmt.Sprintf("live tail %08d", i)
				_ = m.renderedItems()
			}
		})
	}
}
