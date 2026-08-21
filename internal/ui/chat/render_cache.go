package chat

import (
	"encoding/binary"
	"hash"
	"hash/fnv"
	"strconv"
	"strings"

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

	itemKeys    []uint64
	itemRows    []string
	itemRenders int

	textDigests     []contentDigest
	inputDigests    []contentDigest
	outputDigests   []contentDigest
	metadataDigests []contentDigest
}

type contentDigest struct {
	present bool
	value   string
	hash    uint64
}

const (
	hashTrue  uint64 = 1
	hashFalse uint64 = 0
)

type fpWriter struct {
	h   hash.Hash64
	buf [8]byte
}

func newFPWriter() *fpWriter {
	return &fpWriter{h: fnv.New64a()}
}

func (w *fpWriter) u64(v uint64) {
	binary.LittleEndian.PutUint64(w.buf[:], v)
	w.h.Write(w.buf[:])
}

func (w *fpWriter) i64(v int64) { w.str(strconv.FormatInt(v, 10)) }

func (w *fpWriter) i(v int) { w.str(strconv.Itoa(v)) }

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

func hashString64(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

func cachedContentDigest(slots *[]contentDigest, index int, value *string) (bool, uint64, int) {
	for len(*slots) <= index {
		*slots = append(*slots, contentDigest{})
	}
	slot := &(*slots)[index]
	if value == nil {
		slot.present = false
		slot.value = ""
		slot.hash = 0
		return false, 0, 0
	}
	if slot.present && slot.value == *value {
		return true, slot.hash, len(*value)
	}
	slot.present = true
	slot.value = *value
	slot.hash = hashString64(*value)
	return true, slot.hash, len(*value)
}

func (m Model) itemContentFingerprint(index int, it transcriptItem) uint64 {
	w := newFPWriter()
	w.i(index)
	w.i(int(it.kind))
	w.b(it.collapsed)
	w.i64(it.when)
	_, textHash, textLen := cachedContentDigest(&m.renderCache.textDigests, index, &it.text)
	w.i(textLen)
	w.u64(textHash)
	w.str(it.tool.Tool)
	w.str(it.tool.CallID)
	w.str(it.tool.Status)
	w.strPtr(it.tool.Title)
	w.i64Ptr(it.tool.TimeStart)
	w.i64Ptr(it.tool.TimeEnd)
	w.intPtr(it.tool.ExitCode)
	_, inputHash, inputLen := cachedContentDigest(&m.renderCache.inputDigests, index, stringPtr(it.tool.InputJSON))
	w.i(inputLen)
	w.u64(inputHash)
	_, outputHash, outputLen := cachedContentDigest(&m.renderCache.outputDigests, index, it.tool.Output)
	w.i(outputLen)
	w.u64(outputHash)
	_, metadataHash, metadataLen := cachedContentDigest(&m.renderCache.metadataDigests, index, it.tool.MetadataJSON)
	w.i(metadataLen)
	w.u64(metadataHash)
	w.strPtr(it.part.ToolName)
	w.strPtr(it.part.ToolStatus)
	w.str(it.part.ID)
	w.strPtr(it.part.ToolCallID)
	w.i(m.turnOwner(index))
	return w.h.Sum64()
}

func stringPtr(s string) *string {
	return &s
}

func (m Model) itemRenderKey(index int, it transcriptItem) uint64 {
	w := newFPWriter()
	w.u64(m.itemContentFingerprint(index, it))
	w.b(index == m.selectedItem)
	streaming := m.busy && it.kind == itemReasoning && !it.collapsed && index == m.lastReasoningIndex()
	w.b(streaming)
	w.i(m.width)
	w.i(m.transcript.Width())
	w.i(m.railInset)
	usesRail := m.itemUsesWorkRail(index)
	w.b(usesRail)
	liveRail := usesRail && m.itemInLiveTurn(index)
	w.b(liveRail)
	if liveRail {
		w.b(m.busy && m.pulseOn)
		w.i(m.pulse)
	}
	// In-flight tool cards repaint their diamond every pulse tick.
	if it.kind == itemTool && m.busy && m.pulseOn && toolInFlight(toolItemStatus(it)) {
		w.i(m.pulse)
	}
	return w.h.Sum64()
}

// renderFingerprint hashes every input that can change how the transcript
// renders. Large text and tool bodies contribute cached content digests and
// lengths, rather than being written into the hash on every stream delta.
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
	for i, it := range m.items {
		w.u64(m.itemContentFingerprint(i, it))
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
