package chat

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func benchDragModel(items int) Model {
	m := New(Options{})
	m.width = 120
	m.height = 40
	for i := 0; i < items; i++ {
		text := "| A | B |\n| --- | --- |\n| v | w |\n"
		if i%2 == 0 {
			text = "some **bold** text with `inline` code and a table:\n\n" + text + "\nmore text"
		}
		m.items = append(m.items, transcriptItem{kind: itemAssistant, text: text, when: int64(i)})
	}
	m.syncTranscript()
	return m
}

func BenchmarkDragMotion(b *testing.B) {
	for _, items := range []int{20, 100, 300} {
		b.Run(fmt.Sprintf("items=%d", items), func(b *testing.B) {
			m := benchDragModel(items)
			top := m.transcriptTop()
			press, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: 0, Y: top, Button: tea.MouseLeft}))
			m = press.(Model)
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				y := top + (i % 10)
				motion, _ := m.Update(tea.MouseMotionMsg(tea.Mouse{X: 0, Y: y, Button: tea.MouseLeft}))
				m = motion.(Model)
			}
		})
	}
}

func BenchmarkDragView(b *testing.B) {
	m := benchDragModel(100)
	top := m.transcriptTop()
	press, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: 0, Y: top, Button: tea.MouseLeft}))
	m = press.(Model)
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		motion, _ := m.Update(tea.MouseMotionMsg(tea.Mouse{X: 0, Y: top + (i % 10), Button: tea.MouseLeft}))
		m = motion.(Model)
		_ = m.View()
	}
}
