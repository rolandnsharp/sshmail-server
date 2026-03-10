package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/glamour/v2"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- Styles ---

// Dark theme — inspired by Charmbracelet Crush
var (
	bgDark      = lipgloss.Color("#1a1b26")
	bgPanel     = lipgloss.Color("#24283b")
	bgHighlight = lipgloss.Color("#2f3349")
	border      = lipgloss.Color("#414868")
	borderFocus = lipgloss.Color("#7aa2f7")
	textMuted   = lipgloss.Color("#565f89")
	textNormal  = lipgloss.Color("#a9b1d6")
	textBright  = lipgloss.Color("#c0caf5")
	accent      = lipgloss.Color("#bb9af7")
	accentWarm  = lipgloss.Color("#e0af68")
	accentGreen = lipgloss.Color("#9ece6a")
	accentPink  = lipgloss.Color("#f7768e")
	accentCyan  = lipgloss.Color("#7dcfff")

	sidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Background(bgPanel).
			Padding(1, 1)

	sidebarFocusStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(borderFocus).
				Background(bgPanel).
				Padding(1, 1)

	chatStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Background(bgDark).
			Padding(0, 1)

	inputStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Padding(0, 1).
			Foreground(textBright)

	inputFocusStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderFocus).
			Padding(0, 1).
			Foreground(textBright)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accent).
			Background(bgDark).
			Padding(0, 1)

	fromStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accentCyan)

	timeStyle = lipgloss.NewStyle().
			Foreground(textMuted)

	unreadStyle = lipgloss.NewStyle().
			Foreground(accentPink).
			Bold(true)

	statusStyle = lipgloss.NewStyle().
			Foreground(textMuted).
			Background(bgDark).
			Padding(0, 1)

	accentStyle = lipgloss.NewStyle().
			Foreground(textMuted).
			Bold(true).
			PaddingTop(1)

	helpStyle = lipgloss.NewStyle().
			Foreground(textMuted).
			Background(bgDark).
			Padding(0, 1)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(textBright).
			Background(bgDark)

	hintStyle = lipgloss.NewStyle().
			Foreground(textMuted).
			Italic(true)
)

// --- Sidebar item ---

type channelItem struct {
	name   string
	kind   string // "group", "board", "dm", "header", "inbox"
	unread int
	public bool
}

func (i channelItem) Title() string {
	if i.kind == "header" {
		return accentStyle.Render(i.name)
	}
	if i.kind == "hint" {
		return hintStyle.Render(i.name)
	}
	prefix := "  "
	if i.kind == "group" {
		prefix = "# "
	} else if i.kind == "board" {
		prefix = "@ "
	} else if i.kind == "file" {
		prefix = "  "
	}
	title := prefix + i.name
	if i.unread > 0 {
		title += unreadStyle.Render(fmt.Sprintf(" (%d)", i.unread))
	}
	return title
}

func (i channelItem) Description() string { return "" }
func (i channelItem) FilterValue() string { return i.name }

// --- Messages ---

type pollMsg struct {
	unread int
	counts map[string]int
}
type pollErrMsg struct{ err error }
type inboxMsg struct{ messages []Message }
type boardMsg struct{ messages []Message }
type agentsMsg struct{ agents []Agent }
type whoamiMsg struct{ agent *Agent }
type sentMsg struct{ id int64 }
type errMsg struct{ err error }
type watchEventMsg struct{ event WatchEvent }
type repoFilesMsg struct{ files []string }

// --- Focus ---

type focus int

const (
	focusSidebar focus = iota
	focusInput
)

// --- Model ---

type Model struct {
	backend  Backend
	me       *Agent
	focus    focus
	width    int
	height   int
	sidebar  list.Model
	viewport viewport.Model
	input    textarea.Model
	channels []channelItem
	messages []Message
	selected string // currently selected channel/DM name
	selKind  string // "board", "group", "dm", "inbox"
	status   string
	err       error
	watchChan chan WatchEvent
	repoFiles []string
	agents    []Agent
}

func NewModel(backend Backend) Model {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetHeight(1)
	delegate.SetSpacing(0)
	delegate.Styles.SelectedTitle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(accent).
		Foreground(accent).
		Background(bgHighlight).
		Bold(true).
		Padding(0, 0, 0, 1)
	delegate.Styles.NormalTitle = lipgloss.NewStyle().
		Foreground(textBright).
		Padding(0, 0, 0, 2)
	sidebar := list.New([]list.Item{}, delegate, 0, 0)
	sidebar.SetShowTitle(true)
	sidebar.Title = "sshmail"
	sidebar.Styles.Title = lipgloss.NewStyle().
		Foreground(accent).
		Bold(true).
		Padding(0, 1)
	sidebar.SetShowStatusBar(false)
	sidebar.SetShowHelp(false)
	sidebar.SetFilteringEnabled(false)

	input := textarea.New()
	input.Placeholder = "type a message..."
	input.ShowLineNumbers = false
	input.SetHeight(3)
	input.CharLimit = 4096

	vp := viewport.New(0, 0)

	input.Focus()
	sidebar.SetDelegate(dimDelegate())

	return Model{
		backend:  backend,
		focus:    focusInput,
		sidebar:  sidebar,
		viewport: vp,
		input:    input,
		selected: "inbox",
		selKind:  "inbox",
		status:   "connecting...",
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.input.Focus(),
		m.fetchWhoami,
		m.fetchAgents,
		m.fetchInbox,
		m.startWatch(),
		m.fetchRepoFiles,
	)
}

func (m Model) startWatch() tea.Cmd {
	return func() tea.Msg {
		ch := make(chan WatchEvent, 16)
		if err := m.backend.Watch(ch); err != nil {
			return pollErrMsg{err}
		}
		return watchStartedMsg{ch: ch}
	}
}

type watchStartedMsg struct{ ch chan WatchEvent }

func waitForEvent(ch chan WatchEvent) tea.Cmd {
	return func() tea.Msg {
		evt, ok := <-ch
		if !ok {
			return pollErrMsg{fmt.Errorf("watch stream closed")}
		}
		return watchEventMsg{event: evt}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateLayout()
		return m, nil

	case tea.MouseMsg:
		if msg.Action != tea.MouseActionPress {
			break
		}
		sidebarWidth := m.width * 35 / 100
		if sidebarWidth < 20 {
			sidebarWidth = 20
		}
		if sidebarWidth > 40 {
			sidebarWidth = 40
		}
		if msg.X > sidebarWidth {
			m.focus = focusInput
			cmd := m.input.Focus()
			m.sidebar.SetDelegate(dimDelegate())
			return m, cmd
		} else {
			m.focus = focusSidebar
			m.input.Blur()
			m.sidebar.SetDelegate(activeDelegate())
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.focus == focusInput {
				m.focus = focusSidebar
				m.input.Blur()
				m.sidebar.SetDelegate(activeDelegate())
				return m, nil
			}
			return m, tea.Quit
		case "tab":
			if m.focus == focusSidebar {
				m.focus = focusInput
				cmd := m.input.Focus()
				m.sidebar.SetDelegate(dimDelegate())
				return m, cmd
			} else {
				m.focus = focusSidebar
				m.input.Blur()
				m.sidebar.SetDelegate(activeDelegate())
			}
			return m, nil
		case "enter":
			if m.focus == focusInput {
				text := strings.TrimSpace(m.input.Value())
				if text != "" {
					m.input.Reset()
					if m.me != nil {
						optimistic := Message{
							From: m.me.Name,
							To:   m.selected,
							Body: text,
							At:   time.Now(),
						}
						m.messages = append([]Message{optimistic}, m.messages...)
						m.renderMessages()
					}
					return m, m.sendMessage(text)
				}
				return m, nil
			}
			// Enter on sidebar switches to input for the selected channel
			if item, ok := m.sidebar.SelectedItem().(channelItem); ok && item.kind != "header" && item.kind != "file" && item.kind != "hint" {
				m.focus = focusInput
				cmd := m.input.Focus()
				m.sidebar.SetDelegate(dimDelegate())
				return m, cmd
			}
		}

		if m.focus == focusSidebar {
			var cmd tea.Cmd
			m.sidebar, cmd = m.sidebar.Update(msg)
			cmds = append(cmds, cmd)
			// Load channel as user navigates
			if fetchCmd := m.syncSelection(); fetchCmd != nil {
				cmds = append(cmds, fetchCmd)
			}
		} else {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case whoamiMsg:
		m.me = msg.agent
		m.status = fmt.Sprintf("logged in as %s", m.me.Name)

	case repoFilesMsg:
		m.repoFiles = msg.files
		m.rebuildSidebar()

	case agentsMsg:
		m.agents = msg.agents
		m.buildChannelList(msg.agents)
		m.updateLayout()

	case inboxMsg:
		m.messages = msg.messages
		m.renderMessages()
		if m.selKind == "inbox" {
			m.status = fmt.Sprintf("inbox — %d messages", len(msg.messages))
		}

	case boardMsg:
		m.messages = msg.messages
		m.renderMessages()
		m.status = fmt.Sprintf("%s — %d messages", m.selected, len(msg.messages))

	case watchStartedMsg:
		m.watchChan = msg.ch
		m.status = fmt.Sprintf("%s — connected", m.agentName())
		cmds = append(cmds, waitForEvent(msg.ch))

	case watchEventMsg:
		evt := msg.event
		if evt.Type == "message" {
			m.status = fmt.Sprintf("new message from %s", evt.From)
			if evt.To == m.selected || evt.From == m.selected ||
				m.selKind == "inbox" {
				cmds = append(cmds, m.fetchChannel(channelItem{name: m.selected, kind: m.selKind}))
			}
		}
		if m.watchChan != nil {
			cmds = append(cmds, waitForEvent(m.watchChan))
		}

	case pollMsg:
		m.status = fmt.Sprintf("%s — %d unread", m.agentName(), msg.unread)
		m.updateUnreadCounts(msg.counts)
		if msg.unread > 0 {
			cmds = append(cmds, m.fetchChannel(channelItem{name: m.selected, kind: m.selKind}))
		}
		cmds = append(cmds, m.pollTick())

	case pollErrMsg:
		cmds = append(cmds, m.pollTick())

	case sentMsg:
		m.status = fmt.Sprintf("sent #%d", msg.id)
		cmds = append(cmds, m.fetchChannel(channelItem{name: m.selected, kind: m.selKind}))

	case errMsg:
		m.err = msg.err
		m.status = fmt.Sprintf("error: %v", msg.err)
	}

	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	// Always update textarea so cursor blink ticks are processed
	if m.focus == focusInput {
		var inputCmd tea.Cmd
		m.input, inputCmd = m.input.Update(msg)
		cmds = append(cmds, inputCmd)
	}

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	if m.width == 0 {
		return "loading..."
	}

	status := statusStyle.Width(m.width).Render(" " + m.status)

	sidebarWidth := m.width * 35 / 100
	if sidebarWidth < 20 {
		sidebarWidth = 20
	}
	if sidebarWidth > 40 {
		sidebarWidth = 40
	}
	sbStyle := sidebarStyle
	if m.focus == focusSidebar {
		sbStyle = sidebarFocusStyle
	}
	sidebarContent := sbStyle.Width(sidebarWidth - 4).Render(m.sidebar.View())

	chatWidth := m.width - sidebarWidth - 2

	title := titleStyle.Width(chatWidth - 2).Render(m.channelTitle())
	chatContent := chatStyle.Width(chatWidth - 4).Render(m.viewport.View())

	inStyle := inputStyle
	if m.focus == focusInput {
		inStyle = inputFocusStyle
	}
	inputContent := inStyle.Width(chatWidth - 2).Render(m.input.View())

	rightPanel := lipgloss.JoinVertical(lipgloss.Left, title, chatContent, inputContent)
	main := lipgloss.JoinHorizontal(lipgloss.Top, sidebarContent, rightPanel)

	help := helpStyle.Width(m.width).Render(m.helpText())
	content := lipgloss.JoinVertical(lipgloss.Left, main, status, help)

	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Left, lipgloss.Top,
		content,
		lipgloss.WithWhitespaceBackground(bgDark),
	)
}

// syncSelection loads the channel matching the current sidebar highlight.
// Returns a fetch command if the selection changed, nil otherwise.
func (m *Model) syncSelection() tea.Cmd {
	item, ok := m.sidebar.SelectedItem().(channelItem)
	if !ok || item.kind == "header" || item.kind == "file" || item.kind == "hint" {
		return nil
	}
	if item.name == m.selected && item.kind == m.selKind {
		return nil
	}
	m.selected = item.name
	m.selKind = item.kind
	m.status = fmt.Sprintf("loading %s...", item.name)
	return m.fetchChannel(item)
}

func (m Model) helpText() string {
	sep := " · "
	if m.focus == focusInput {
		return helpKeyStyle.Render("enter") + " send" + sep +
			helpKeyStyle.Render("esc") + " sidebar" + sep +
			helpKeyStyle.Render("ctrl+c") + " quit"
	}
	return helpKeyStyle.Render("↑↓") + " navigate" + sep +
		helpKeyStyle.Render("enter") + " select" + sep +
		helpKeyStyle.Render("tab") + " write" + sep +
		helpKeyStyle.Render("esc") + " quit"
}

// --- Layout ---

func (m *Model) updateLayout() {
	sidebarWidth := m.width * 35 / 100
	if sidebarWidth < 20 {
		sidebarWidth = 20
	}
	if sidebarWidth > 40 {
		sidebarWidth = 40
	}

	chatWidth := m.width - sidebarWidth - 8
	// 2 status/help lines + 1 title + 3 input + 6 borders/padding
	chatHeight := m.height - 18

	if chatHeight < 5 {
		chatHeight = 5
	}

	// Sidebar list height so rendered sidebar (list + border/padding) matches right panel
	sidebarHeight := chatHeight + 4
	m.sidebar.SetSize(sidebarWidth-4, sidebarHeight)
	m.viewport.Width = chatWidth
	m.viewport.Height = chatHeight
	m.input.SetWidth(chatWidth)
	m.renderMessages()
}

// --- Rendering ---

func (m *Model) renderMessages() {
	fileStyle := lipgloss.NewStyle().Foreground(accentWarm).Background(bgDark)
	lineStyle := lipgloss.NewStyle().Background(bgDark).Width(m.viewport.Width)

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithWordWrap(m.viewport.Width-20),
		glamour.WithStylePath("dark"),
	)

	var sb strings.Builder
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := m.messages[i]
		ts := timeStyle.Background(bgDark).Render(msg.At.Local().Format("15:04"))
		from := fromStyle.Background(bgDark).Render(msg.From)

		body := msg.Body
		if renderer != nil {
			if rendered, err := renderer.Render(body); err == nil {
				body = strings.TrimSpace(rendered)
			}
		}

		if msg.File != nil {
			body += fileStyle.Render(fmt.Sprintf(" [%s]", *msg.File))
		}

		header := fmt.Sprintf("%s %s:", ts, from)
		if !strings.Contains(body, "\n") && len(body) < 80 {
			sb.WriteString(lineStyle.Render(header+" "+body) + "\n")
		} else {
			sb.WriteString(lineStyle.Render(header) + "\n" + body + "\n")
		}
	}
	m.viewport.SetContent(sb.String())
	m.viewport.GotoBottom()
}

func (m Model) channelTitle() string {
	if m.selKind == "inbox" {
		return "inbox"
	}
	prefix := ""
	switch m.selKind {
	case "group":
		prefix = "# "
	case "board":
		prefix = "@ "
	}
	return prefix + m.selected
}

func (m Model) agentName() string {
	if m.me != nil {
		return m.me.Name
	}
	return "sshmail"
}

// --- Channel list ---

func (m *Model) buildChannelList(agents []Agent) {
	var items []list.Item

	items = append(items, channelItem{name: "inbox", kind: "inbox"})

	var boards []channelItem
	var groups []channelItem
	var dms []channelItem

	for _, a := range agents {
		switch {
		case strings.HasPrefix(a.Fingerprint, "group:"):
			groups = append(groups, channelItem{name: a.Name, kind: "group"})
		case a.Public:
			boards = append(boards, channelItem{name: a.Name, kind: "board", public: true})
		case a.InvitedBy > 0 && m.me != nil && a.Name != m.me.Name:
			dms = append(dms, channelItem{name: a.Name, kind: "dm"})
		}
	}

	if len(boards) > 0 {
		items = append(items, channelItem{name: "Public Boards", kind: "header"})
		for _, b := range boards {
			items = append(items, b)
		}
	}
	if len(groups) > 0 {
		items = append(items, channelItem{name: "Private Groups", kind: "header"})
		for _, g := range groups {
			items = append(items, g)
		}
	}
	if len(dms) > 0 {
		items = append(items, channelItem{name: "Direct Messages", kind: "header"})
		for _, d := range dms {
			items = append(items, d)
		}
	}

	items = append(items, channelItem{name: "Git Repo", kind: "header"})
	if len(m.repoFiles) > 0 {
		for _, f := range m.repoFiles {
			items = append(items, channelItem{name: f, kind: "file"})
		}
	} else {
		items = append(items, channelItem{name: "(empty)", kind: "file"})
	}
	if m.me != nil {
		items = append(items, channelItem{name: "git clone ssh://ssh.sshmail.dev/" + m.me.Name, kind: "hint"})
	}

	m.channels = nil
	m.channels = append(m.channels, boards...)
	m.channels = append(m.channels, groups...)
	m.channels = append(m.channels, dms...)
	m.sidebar.SetItems(items)
}

func (m *Model) rebuildSidebar() {
	if m.agents != nil {
		m.buildChannelList(m.agents)
	}
}

// --- Commands ---

func (m Model) fetchWhoami() tea.Msg {
	agent, err := m.backend.Whoami()
	if err != nil {
		return errMsg{err}
	}
	return whoamiMsg{agent}
}

func (m Model) fetchRepoFiles() tea.Msg {
	files, err := m.backend.RepoFiles()
	if err != nil {
		return repoFilesMsg{nil}
	}
	return repoFilesMsg{files}
}

func (m Model) fetchAgents() tea.Msg {
	agents, err := m.backend.Agents()
	if err != nil {
		return errMsg{err}
	}
	return agentsMsg{agents}
}

func (m Model) fetchInbox() tea.Msg {
	msgs, err := m.backend.Inbox(true)
	if err != nil {
		return errMsg{err}
	}
	return inboxMsg{msgs}
}

func (m Model) fetchChannel(item channelItem) tea.Cmd {
	return func() tea.Msg {
		switch item.kind {
		case "board":
			msgs, err := m.backend.Board(item.name)
			if err != nil {
				return errMsg{err}
			}
			return boardMsg{msgs}
		case "inbox":
			msgs, err := m.backend.Inbox(false)
			if err != nil {
				return errMsg{err}
			}
			return inboxMsg{msgs}
		default:
			msgs, err := m.backend.Inbox(true)
			if err != nil {
				return errMsg{err}
			}
			var filtered []Message
			for _, msg := range msgs {
				if msg.From == item.name || msg.To == item.name {
					filtered = append(filtered, msg)
				}
			}
			return inboxMsg{filtered}
		}
	}
}

func (m Model) sendMessage(text string) tea.Cmd {
	return func() tea.Msg {
		target := m.selected
		if m.selKind == "inbox" {
			return errMsg{fmt.Errorf("select a channel or DM to send")}
		}
		result, err := m.backend.Send(target, text)
		if err != nil {
			return errMsg{err}
		}
		return sentMsg{result.ID}
	}
}

func (m Model) pollTick() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		result, err := m.backend.PollCounts()
		if err != nil {
			return pollErrMsg{err}
		}
		return pollMsg{unread: result.Unread, counts: result.Counts}
	})
}

func (m *Model) updateUnreadCounts(counts map[string]int) {
	if counts == nil {
		return
	}
	items := m.sidebar.Items()
	changed := false
	for i, item := range items {
		if ch, ok := item.(channelItem); ok {
			n := counts[ch.name]
			if ch.unread != n {
				ch.unread = n
				items[i] = ch
				changed = true
			}
		}
	}
	if changed {
		m.sidebar.SetItems(items)
	}
}

// --- Delegates ---

func activeDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.ShowDescription = false
	d.SetHeight(1)
	d.SetSpacing(0)
	d.Styles.SelectedTitle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(accent).
		Foreground(accent).
		Background(bgHighlight).
		Bold(true).
		Padding(0, 0, 0, 1)
	d.Styles.NormalTitle = lipgloss.NewStyle().
		Foreground(textBright).
		Padding(0, 0, 0, 2)
	return d
}

func dimDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.ShowDescription = false
	d.SetHeight(1)
	d.SetSpacing(0)
	d.Styles.SelectedTitle = lipgloss.NewStyle().
		Foreground(textMuted).
		Background(bgHighlight).
		Padding(0, 1)
	d.Styles.NormalTitle = lipgloss.NewStyle().
		Foreground(textMuted).
		Padding(0, 1)
	return d
}
