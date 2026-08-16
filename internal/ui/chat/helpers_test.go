package chat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

func viewText(m Model) string {
	return m.View().Content
}

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

func newTestStore(t *testing.T) *db.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := db.Open(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return st
}

func deadClient() *opencode.Client {
	return opencode.NewClient("test-key", opencode.WithBaseURL("http://127.0.0.1:1"))
}

type fakeToolCall struct {
	ID   string
	Name string
	Args string
}

func respBody(content, finishReason string, toolCalls []fakeToolCall) string {
	msg := map[string]any{"content": content, "finish_reason": finishReason}
	if len(toolCalls) > 0 {
		var wc []map[string]any
		for _, tc := range toolCalls {
			wc = append(wc, map[string]any{
				"id":   tc.ID,
				"type": "function",
				"function": map[string]any{
					"name":      tc.Name,
					"arguments": tc.Args,
				},
			})
		}
		msg["tool_calls"] = wc
	}
	body := map[string]any{"choices": []any{map[string]any{"message": msg}}}
	raw, _ := json.Marshal(body)
	return string(raw)
}

type fakeProvider struct {
	mu        sync.Mutex
	responses []string
	requests  int
	modelList []string
	delay     time.Duration
	srv       *httptest.Server
}

func newFakeProvider(t *testing.T, status int, responses ...string) *fakeProvider {
	t.Helper()
	f := &fakeProvider{responses: responses}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			raw, _ := json.Marshal(map[string]any{"data": []any{
				map[string]any{"id": "deepseek-v4-flash"},
				map[string]any{"id": "claude-4"},
			}})
			_, _ = w.Write(raw)
			return
		}
		f.mu.Lock()
		idx := f.requests
		f.requests++
		var req struct {
			Model string `json:"model"`
		}
		if raw, err := io.ReadAll(r.Body); err == nil {
			_ = json.Unmarshal(raw, &req)
		}
		f.modelList = append(f.modelList, req.Model)
		resp := responses[len(responses)-1]
		if idx < len(responses) {
			resp = responses[idx]
		}
		delay := f.delay
		f.mu.Unlock()
		if delay > 0 {
			time.Sleep(delay)
		}
		w.Header().Set("Content-Type", "application/json")
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	f.srv = srv
	return f
}

func (f *fakeProvider) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func (f *fakeProvider) requestModel(idx int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if idx < 0 || idx >= len(f.modelList) {
		return ""
	}
	return f.modelList[idx]
}

func newClient(srv *httptest.Server) *opencode.Client {
	return opencode.NewClient("test-key", opencode.WithBaseURL(srv.URL))
}

type pump struct {
	t  *testing.T
	ch chan tea.Msg
}

func newPump(t *testing.T) *pump {
	return &pump{t: t, ch: make(chan tea.Msg, 512)}
}

func (p *pump) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				p.run(c)
			}
			return
		}
		p.ch <- msg
	}()
}

func (p *pump) next() tea.Msg {
	select {
	case m := <-p.ch:
		return m
	case <-time.After(15 * time.Second):
		p.t.Fatal("timed out waiting for message")
		return nil
	}
}

func (p *pump) runStep(m Model, msg tea.Msg) Model {
	mm, cmd := m.Update(msg)
	p.run(cmd)
	return mm.(Model)
}

func (p *pump) apply(m Model, msg tea.Msg) Model {
	mm, cmd := m.Update(msg)
	p.run(cmd)
	return mm.(Model)
}

func (p *pump) drainUntil(m Model, want string) Model {
	for i := 0; i < 300; i++ {
		if strings.Contains(stripANSI(viewText(m)), want) {
			return m
		}
		m = p.apply(m, p.next())
	}
	p.t.Fatalf("never saw %q in view", want)
	return m
}

func (p *pump) drainIdle(m Model) Model {
	for i := 0; i < 2000; i++ {
		if !m.busy && !strings.Contains(stripANSI(viewText(m)), "sending...") {
			return m
		}
		m = p.apply(m, p.next())
	}
	p.t.Fatalf("model still busy after draining")
	return m
}

func upd(m Model, msg tea.Msg) Model {
	nm, _ := m.Update(msg)
	return nm.(Model)
}

func updCmd(m Model, msg tea.Msg) (Model, tea.Cmd) {
	nm, cmd := m.Update(msg)
	return nm.(Model), cmd
}

func typeText(m Model, s string) Model {
	for _, r := range s {
		m = upd(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func lastToolStatus(m Model) string {
	for i := len(m.items) - 1; i >= 0; i-- {
		if m.items[i].kind == itemTool {
			return m.items[i].tool.Status
		}
	}
	return ""
}

func viewLineIndex(m Model, needle string) int {
	for i, line := range strings.Split(stripANSI(viewText(m)), "\n") {
		if strings.Contains(line, needle) {
			return i
		}
	}
	return -1
}

func stampOnRight(view, left, stamp string) bool {
	for _, line := range strings.Split(view, "\n") {
		if !strings.Contains(line, left) || !strings.Contains(line, stamp) {
			continue
		}
		trimmed := strings.TrimRight(line, " ")
		idxLeft := strings.Index(trimmed, left)
		idxStamp := strings.LastIndex(trimmed, stamp)
		if idxLeft < 0 || idxStamp <= idxLeft {
			return false
		}
		return strings.HasSuffix(trimmed, stamp)
	}
	return false
}

func clickModelStatus(t *testing.T, m Model) Model {
	t.Helper()
	left, top, right, bottom, ok := m.modelStatusRect()
	if !ok || right <= left || bottom <= top {
		t.Fatal("model status click target not found")
	}
	mm, _ := m.Update(tea.MouseClickMsg(tea.Mouse{
		X:      right - 1,
		Y:      top,
		Button: tea.MouseLeft,
	}))
	return mm.(Model)
}

func enter() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyEnter}
}
