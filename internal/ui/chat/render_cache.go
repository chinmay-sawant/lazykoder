package chat

import (
	"encoding/binary"
	"hash"
	"hash/fnv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// renderCache memoizes the rendered transcript so drag motion events do not
// re-run markdown and lipgloss styling for every item. It lives behind a
// pointer on Model so every value copy shares the same memo across bubbletea
// updates; the fingerprint gates correctness.
type renderCache struct {
	fp      uint64
	rows    []string
	plain   []string
	content string
}

const (
	hashTrue  uint64 = 1
	hashFalse uint64 = 0
)

type fpWriter struct {
	h    hash.Hash64
	buf  [8]byte
	keys []byte
}

func newFPWriter() *fpWriter {
	return &fpWriter{h: fnv.New64a()}
}

func (w *fpWriter) u64(v uint64) {
	binary.LittleEndian.PutUint64(w.buf[:], v)
	w.h.Write(w.buf[:])
}

func (w *fpWriter) i64(v int64) { w.u64(uint64(v)) }

func (w *fpWriter) i(v int) { w.u64(uint64(v)) }

func (w *fpWriter) b(v bool) {
	if v {
		w.u64(hashTrue)
		return
	}
	w.u64(hashFalse)
}

func (w *fpWriter) str(s string) {
	w.u64(uint64(len(s)))
	w.h.Write([]byte(s))
}

func (w *fpWriter) strPtr(s *string) {
	if s == nil {
		w.b(false)
		return
	}
	w.b(true)
	w.str(*s)
}

func (w *fpWriter) i64Ptr(p *int64) {
	if p == nil {
		w.b(false)
		return
	}
	w.b(true)
	w.i64(*p)
}

func (w *fpWriter) intPtr(p *int) {
	if p == nil {
		w.b(false)
		return
	}
	w.b(true)
	w.i(*p)
}

// renderFingerprint hashes every input that can change how the transcript
// renders: item contents and collapse state, tool/part fields, the selected
// item, the work-rail throb, and the layout width.
func (m Model) renderFingerprint() uint64 {
	w := newFPWriter()
	w.i(m.selectedItem)
	w.b(m.busy)
	w.b(m.pulseOn)
	w.i(m.pulse)
	w.i(m.width)
	w.i(m.railInset)
	w.i(m.turnItemFrom)
	w.i(len(m.items))
	for _, it := range m.items {
		w.i(int(it.kind))
		w.b(it.collapsed)
		w.i64(it.when)
		w.str(it.text)
		w.str(it.tool.Tool)
		w.str(it.tool.CallID)
		w.str(it.tool.Status)
		w.strPtr(it.tool.Title)
		w.i64Ptr(it.tool.TimeStart)
		w.i64Ptr(it.tool.TimeEnd)
		w.intPtr(it.tool.ExitCode)
		w.str(it.tool.InputJSON)
		w.strPtr(it.tool.Output)
		w.strPtr(it.tool.MetadataJSON)
		w.strPtr(it.part.ToolName)
		w.strPtr(it.part.ToolStatus)
	}
	return w.h.Sum64()
}

// plainTranscriptRowsMemo builds the ANSI-stripped rows of the rendered
// transcript once per render fingerprint.
func (m Model) plainTranscriptRowsMemo() []string {
	rows := m.renderedItems()
	c := m.renderCache
	if c == nil {
		return strings.Split(ansi.Strip(strings.Join(rows, "\n")), "\n")
	}
	if c.plain == nil {
		c.plain = strings.Split(ansi.Strip(strings.Join(rows, "\n")), "\n")
	}
	return c.plain
}

func (m Model) plainRowsFrom(rows []string) []string {
	return strings.Split(ansi.Strip(strings.Join(rows, "\n")), "\n")
}

// ensureRenderedRows returns the memoized rendered items, rebuilding them
// only when the fingerprint changes.
func (m Model) ensureRenderedRows() []string {
	c := m.renderCache
	if c == nil {
		return m.buildRenderedItems()
	}
	fp := m.renderFingerprint()
	if c.fp == fp {
		return c.rows
	}
	c.fp = fp
	c.rows = m.buildRenderedItems()
	c.plain = nil
	return c.rows
}

// renderedItemsMemo is the memoized entry point for renderedItems.
func (m Model) renderedItemsMemo() []string {
	return m.ensureRenderedRows()
}

// plainRowsMemo is the memoized entry point for plainTranscriptRows.
func (m Model) plainRowsMemo() []string {
	return m.plainTranscriptRowsMemo()
}

// lipgloss width helper kept next to the cache so the memo callers do not
// reach into styling details.
func rowWidth(s string) int {
	return lipgloss.Width(s)
}
