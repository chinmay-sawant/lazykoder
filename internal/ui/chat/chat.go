// Package chat implements the chat TUI model: transcript, prompt, status and confirm flow.
package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/policy"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/tools/question"
	"github.com/chinmay-sawant/lazykoder/internal/ui/confirm"
)

const (
	idleHint = "enter to send  •  q to quit"
	busyHint = "sending..."
)

var (
	errStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	busyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	hintStyle = lipgloss.NewStyle().Faint(true)
)

// Options configures the chat model.
type Options struct {
	Store      *db.Store
	Client     *opencode.Client
	Workdir    string
	Session    *db.Session
	MaxSteps   int
	InitialErr string
}

// Model is the chat screen: transcript, prompt, status and confirm view.
type Model struct {
	store    *db.Store
	client   *opencode.Client
	workdir  string
	session  *db.Session
	maxSteps int

	lines       []string
	prompt      textinput.Model
	busy        bool
	err         string
	pendingUser string
	lastTool    int

	model     string // current model; "" = provider default
	models    []string
	modelsErr string

	confirmCh   chan confirmRequest
	askCh       chan askRequest
	doneCh      chan struct{}
	doneClosed  bool
	pending     *confirmRequest
	pendingAsk  *askRequest
	confirm     confirm.Model
	confirmMode bool

	picker      list.Model
	pickerMode  bool
	pickerBuilt bool
}

type modelItem string

func (i modelItem) Title() string       { return string(i) }
func (i modelItem) Description() string { return "" }
func (i modelItem) FilterValue() string { return string(i) }

type modelsMsg struct {
	list []string
	err  error
}

type confirmRequest struct {
	dec     policy.Decision
	subject string
	resp    chan bool
}

type confirmRequestMsg struct {
	req confirmRequest
}

type askRequest struct {
	q    question.Question
	resp chan int
}

type askRequestMsg struct {
	req askRequest
}

type eventBatchMsg struct {
	events []agent.Event
	err    error
}

// New returns a chat model for the given options.
func New(opts Options) Model {
	m := Model{
		store:     opts.Store,
		client:    opts.Client,
		workdir:   opts.Workdir,
		session:   opts.Session,
		maxSteps:  opts.MaxSteps,
		err:       opts.InitialErr,
		confirmCh: make(chan confirmRequest, 1),
		askCh:     make(chan askRequest, 1),
		doneCh:    make(chan struct{}),
		lastTool:  -1,
		prompt:    textinput.New(),
	}
	m.prompt.Placeholder = "ask lazykoder..."
	m.prompt.Focus()
	if m.session != nil && m.store != nil {
		m.model = m.session.Model
		m.replay(m.session.ID)
	}
	return m
}

// Init starts the fetch and watcher commands.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.confirmWatch(), m.askWatch(), m.fetchModels)
}

func (m Model) fetchModels() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	list, err := m.client.Models(ctx)
	return modelsMsg{list: list, err: err}
}

// Update routes keys and streamed events through the model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case confirmRequestMsg:
		m.pending = &msg.req
		qualifier := "rm"
		if msg.req.dec.Destructive {
			qualifier = "rm -rf"
		}
		m.confirm = confirm.New(msg.req.subject, qualifier)
		m.confirmMode = true
		return m, m.confirmWatch()
	case askRequestMsg:
		m.pendingAsk = &msg.req
		qualifier := msg.req.q.Header
		if qualifier == "" {
			qualifier = "question"
		}
		m.confirm = confirm.New(msg.req.q.Question, qualifier)
		m.confirmMode = true
		return m, m.askWatch()
	case eventBatchMsg:
		for _, ev := range msg.events {
			switch ev.Kind {
			case agent.EventPart:
				m.applyPart(ev.Part)
			case agent.EventTool:
				m.applyTool(ev)
			case agent.EventError:
				if ev.Err != nil {
					m.err = ev.Err.Error()
				}
			}
		}
		if m.err == "" && msg.err != nil {
			m.err = msg.err.Error()
		}
		m.busy = false
		m.pendingUser = ""
		m.pending = nil
		m.pendingAsk = nil
		m.confirmMode = false
		return m, nil
	case modelsMsg:
		m.models = msg.list
		if msg.err != nil {
			m.modelsErr = msg.err.Error()
		} else {
			m.modelsErr = ""
		}
		return m, nil
	case errMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
		}
		return m, nil
	case tea.KeyPressMsg:
		if m.confirmMode {
			return m.updateConfirmKey(msg)
		}
		if m.pickerMode {
			return m.updatePickerKey(msg)
		}
		return m.updateKey(msg)
	}
	return m, nil
}

// View renders the picker, confirm view, or the transcript, prompt and status line.
func (m Model) View() tea.View {
	if m.confirmMode {
		v := tea.NewView(m.confirm.View())
		v.AltScreen = true
		return v
	}
	if m.pickerMode {
		v := tea.NewView(m.pickerView())
		v.AltScreen = true
		return v
	}
	var b strings.Builder
	if len(m.lines) > 0 {
		b.WriteString(strings.Join(m.lines, "\n"))
		b.WriteString("\n\n")
	}
	b.WriteString(m.prompt.View())
	b.WriteString("\n\n")
	b.WriteString(m.statusLine())
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func (m Model) pickerView() string {
	var b strings.Builder
	if m.modelsErr != "" {
		b.WriteString(errStyle.Render("models unavailable: " + m.modelsErr))
		b.WriteString("\n\n")
	}
	if len(m.models) == 0 {
		b.WriteString(hintStyle.Render("no models loaded"))
	} else {
		b.WriteString(m.picker.View())
	}
	b.WriteString("\n\n")
	b.WriteString(hintStyle.Render("↑/↓ navigate  •  enter select  •  esc cancel"))
	return b.String()
}

func (m Model) updatePickerKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	if key.Mod.Contains(tea.ModCtrl) && key.Code == 'c' {
		return m.closeDone(), tea.Quit
	}
	switch key.Code {
	case 'q', 'Q':
		m.pickerMode = false
		return m, nil
	case tea.KeyEscape:
		m.pickerMode = false
		return m, nil
	case tea.KeyEnter:
		if m.pickerBuilt && len(m.models) > 0 {
			if it, ok := m.picker.SelectedItem().(modelItem); ok {
				m.model = string(it)
				m.pickerMode = false
				return m, m.persistModel()
			}
		}
		return m, nil
	case 'j', tea.KeyDown:
		m.picker.CursorDown()
		return m, nil
	case 'k', tea.KeyUp:
		m.picker.CursorUp()
		return m, nil
	}
	return m, nil
}

func (m Model) openPicker() Model {
	if !m.pickerBuilt {
		items := make([]list.Item, 0, len(m.models))
		for _, id := range m.models {
			items = append(items, modelItem(id))
		}
		m.picker = list.New(items, list.NewDefaultDelegate(), 60, 10)
		m.picker.Title = "Models"
		m.pickerBuilt = true
	}
	m.pickerMode = true
	return m
}

func (m Model) persistModel() tea.Cmd {
	if m.session == nil || m.store == nil {
		return nil
	}
	sid, model := m.session.ID, m.model
	return func() tea.Msg {
		if err := m.store.UpdateSessionModel(context.Background(), sid, model); err != nil {
			return errMsg{err: err}
		}
		return nil
	}
}

type errMsg struct {
	err error
}

func (m Model) updateKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	if key.Mod.Contains(tea.ModCtrl) {
		if key.Code == 'c' {
			return m.closeDone(), tea.Quit
		}
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(key)
		return m, cmd
	}
	switch key.Code {
	case 'q', 'Q':
		return m.closeDone(), tea.Quit
	case 'm', 'M':
		if m.busy {
			return m, nil
		}
		return m.openPicker(), nil
	case tea.KeyEnter:
		if m.busy {
			return m, nil
		}
		text := m.prompt.Value()
		if strings.TrimSpace(text) == "" {
			return m, nil
		}
		return m.submit(text)
	}
	var cmd tea.Cmd
	m.prompt, cmd = m.prompt.Update(key)
	return m, cmd
}

func (m Model) updateConfirmKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	cm, cmd := m.confirm.Update(key)
	m.confirm = cm
	if res := m.confirm.Result(); res != nil {
		if m.pendingAsk != nil {
			return m.resolveAsk(res.Allow), nil
		}
		return m.resolveConfirm(res.Allow), nil
	}
	if cmd != nil {
		return m.closeDone(), tea.Quit
	}
	return m, nil
}

func (m Model) submit(text string) (Model, tea.Cmd) {
	m.prompt.SetValue("")
	m.busy = true
	m.err = ""
	m.pendingUser = text
	m.lines = append(m.lines, "user: "+text)
	ch := make(chan agent.Event, 64)
	errCh := make(chan error, 1)
	ag := agent.New(m.store, m.client, m.workdir, agent.Options{
		Session:  m.session,
		MaxSteps: m.maxSteps,
		Model:    m.model,
		Confirm:  m.confirmHook,
		Ask:      m.askHook,
	})
	sendCmd := func() tea.Msg {
		go func() { errCh <- ag.Send(context.Background(), text, ch) }()
		return nil
	}
	watchCmd := func() tea.Msg {
		var evs []agent.Event
		for ev := range ch {
			evs = append(evs, ev)
		}
		return eventBatchMsg{events: evs, err: <-errCh}
	}
	return m, tea.Batch(sendCmd, watchCmd)
}

func (m Model) confirmWatch() tea.Cmd {
	return func() tea.Msg {
		req := <-m.confirmCh
		return confirmRequestMsg{req: req}
	}
}

func (m Model) askWatch() tea.Cmd {
	return func() tea.Msg {
		req := <-m.askCh
		return askRequestMsg{req: req}
	}
}

func (m Model) confirmHook(dec policy.Decision, subject string) (bool, error) {
	resp := make(chan bool, 1)
	req := confirmRequest{dec: dec, subject: subject, resp: resp}
	select {
	case m.confirmCh <- req:
	default:
		return false, nil
	}
	select {
	case ok := <-resp:
		return ok, nil
	case <-m.doneCh:
		return false, nil
	}
}

func (m Model) askHook(q question.Question) (int, error) {
	resp := make(chan int, 1)
	req := askRequest{q: q, resp: resp}
	select {
	case m.askCh <- req:
	default:
		return 0, errors.New("chat: ask channel busy")
	}
	select {
	case idx := <-resp:
		return idx, nil
	case <-m.doneCh:
		return 0, errors.New("chat: cancelled")
	}
}

func (m Model) resolveConfirm(allow bool) Model {
	if m.pending != nil {
		select {
		case m.pending.resp <- allow:
		default:
		}
	}
	m.pending = nil
	m.confirmMode = false
	return m
}

func (m Model) resolveAsk(allow bool) Model {
	idx := 0
	if !allow {
		idx = 1
	}
	if m.pendingAsk != nil {
		select {
		case m.pendingAsk.resp <- idx:
		default:
		}
	}
	m.pendingAsk = nil
	m.confirmMode = false
	return m
}

func (m Model) closeDone() Model {
	if m.doneClosed {
		return m
	}
	m.doneClosed = true
	close(m.doneCh)
	return m
}

func (m *Model) replay(sessionID string) {
	ctx := context.Background()
	msgs, err := m.store.ListMessages(ctx, sessionID)
	if err != nil {
		m.err = "chat: " + err.Error()
		return
	}
	for _, msg := range msgs {
		parts, err := m.store.ListParts(ctx, msg.ID)
		if err != nil {
			m.err = "chat: " + err.Error()
			return
		}
		for _, p := range parts {
			switch p.Type {
			case "text":
				if p.Text != nil {
					m.lines = append(m.lines, msg.Role+": "+*p.Text)
				}
			case "reasoning":
				m.lines = append(m.lines, "reasoning: (collapsed)")
			case "tool":
				name := "tool"
				status := ""
				if p.ToolName != nil {
					name = *p.ToolName
				}
				if p.ToolStatus != nil {
					status = *p.ToolStatus
				}
				m.lines = append(m.lines, name+": "+status)
			}
		}
	}
}

func (m *Model) applyPart(p db.Part) {
	switch p.Type {
	case "text":
		if p.Text == nil {
			return
		}
		if m.pendingUser != "" && *p.Text == m.pendingUser {
			m.pendingUser = ""
			return
		}
		m.lines = append(m.lines, "assistant: "+*p.Text)
	case "reasoning":
		m.lines = append(m.lines, "reasoning: (collapsed)")
	}
}

func (m *Model) applyTool(ev agent.Event) {
	name := ev.Tool.Tool
	if name == "" && ev.Part.ToolName != nil {
		name = *ev.Part.ToolName
	}
	if name == "" {
		return
	}
	status := ev.Tool.Status
	if status == "" || status == "pending" {
		m.lines = append(m.lines, name+": pending")
		m.lastTool = len(m.lines) - 1
		return
	}
	if m.lastTool >= 0 && m.lastTool < len(m.lines) {
		m.lines[m.lastTool] = name + ": " + status
	}
}

func (m Model) statusLine() string {
	switch {
	case m.err != "":
		return errStyle.Render(m.err)
	case m.busy:
		return busyStyle.Render(busyHint)
	default:
		label := "default"
		if m.model != "" {
			label = m.model
		}
		var b strings.Builder
		b.WriteString(hintStyle.Render(strings.Join([]string{
			"model " + label,
			"m switch",
			"enter to send",
			"q to quit",
		}, "  •  ")))
		if m.modelsErr != "" {
			b.WriteString("\n")
			b.WriteString(errStyle.Render("models: " + m.modelsErr))
		} else if len(m.models) > 0 {
			b.WriteString("\n")
			b.WriteString(hintStyle.Render(fmt.Sprintf("models: %d available", len(m.models))))
		}
		return b.String()
	}
}
