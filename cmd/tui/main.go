package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- Config ---

func getConfig() (host string, port int, keyPath string) {
	host = os.Getenv("SSHMAIL_HOST")
	if host == "" {
		host = "ssh.sshmail.dev"
	}
	port = 2233
	if p := os.Getenv("SSHMAIL_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}
	keyPath = os.Getenv("SSHMAIL_KEY")
	if keyPath == "" {
		home, _ := os.UserHomeDir()
		keyPath = filepath.Join(home, ".ssh", "id_ed25519")
	}
	return
}

// --- Styles ---

var (
	sidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1)

	chatStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1)

	inputStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62"))

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("170"))

	fromStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39"))

	timeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	unreadStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
)

// --- Sidebar item ---

type channelItem struct {
	name    string
	kind    string // "group", "board", "dm"
	unread  int
	public  bool
}

func (i channelItem) Title() string {
	prefix := "  "
	if i.kind == "group" {
		prefix = "# "
	} else if i.kind == "board" {
		prefix = "@ "
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

type pollMsg struct{ unread int }
type pollErrMsg struct{ err error }
type inboxMsg struct{ messages []Message }
type boardMsg struct{ messages []Message }
type agentsMsg struct{ agents []Agent }
type whoamiMsg struct{ agent *Agent }
type sentMsg struct{ id int64 }
type errMsg struct{ err error }

// --- Focus ---

type focus int

const (
	focusSidebar focus = iota
	focusInput
)

// --- Model ---

type model struct {
	client   *Client
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
	err      error
}

func initialModel(client *Client) model {
	// Sidebar
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetHeight(1)
	delegate.SetSpacing(0)
	sidebar := list.New([]list.Item{}, delegate, 0, 0)
	sidebar.SetShowTitle(true)
	sidebar.Title = "sshmail"
	sidebar.SetShowStatusBar(false)
	sidebar.SetShowHelp(false)
	sidebar.SetFilteringEnabled(false)

	// Input
	input := textarea.New()
	input.Placeholder = "type a message..."
	input.ShowLineNumbers = false
	input.SetHeight(3)
	input.CharLimit = 4096

	// Viewport
	vp := viewport.New(0, 0)

	return model{
		client:   client,
		focus:    focusSidebar,
		sidebar:  sidebar,
		viewport: vp,
		input:    input,
		selected: "inbox",
		selKind:  "inbox",
		status:   "connecting...",
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.fetchWhoami,
		m.fetchAgents,
		m.fetchInbox,
		m.pollTick(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateLayout()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.focus == focusSidebar {
				return m, tea.Quit
			}
		case "tab":
			if m.focus == focusSidebar {
				m.focus = focusInput
				m.input.Focus()
				m.sidebar.SetDelegate(dimDelegate())
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
					return m, m.sendMessage(text)
				}
				return m, nil
			}
			// Sidebar: select channel
			if item, ok := m.sidebar.SelectedItem().(channelItem); ok {
				m.selected = item.name
				m.selKind = item.kind
				m.status = fmt.Sprintf("loading %s...", item.name)
				return m, m.fetchChannel(item)
			}
		case "esc":
			if m.focus == focusInput {
				m.focus = focusSidebar
				m.input.Blur()
				m.sidebar.SetDelegate(activeDelegate())
				return m, nil
			}
		}

		// Route keyboard to focused component
		if m.focus == focusSidebar {
			var cmd tea.Cmd
			m.sidebar, cmd = m.sidebar.Update(msg)
			cmds = append(cmds, cmd)
		} else {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case whoamiMsg:
		m.me = msg.agent
		m.status = fmt.Sprintf("logged in as %s", m.me.Name)

	case agentsMsg:
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

	case pollMsg:
		m.status = fmt.Sprintf("%s — %d unread", m.agentName(), msg.unread)
		cmds = append(cmds, m.pollTick())

	case pollErrMsg:
		cmds = append(cmds, m.pollTick())

	case sentMsg:
		m.status = fmt.Sprintf("sent #%d", msg.id)
		// Refresh current view
		if m.selKind == "inbox" || m.selKind == "dm" {
			cmds = append(cmds, m.fetchInbox)
		} else {
			cmds = append(cmds, m.fetchChannel(channelItem{name: m.selected, kind: m.selKind}))
		}

	case errMsg:
		m.err = msg.err
		m.status = fmt.Sprintf("error: %v", msg.err)
	}

	// Update viewport for scroll
	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if m.width == 0 {
		return "loading..."
	}

	// Status bar
	status := statusStyle.Render(" " + m.status)

	// Sidebar
	sidebarWidth := m.width * 35 / 100
	if sidebarWidth < 20 {
		sidebarWidth = 20
	}
	if sidebarWidth > 40 {
		sidebarWidth = 40
	}
	sidebarContent := sidebarStyle.Width(sidebarWidth - 4).Render(m.sidebar.View())

	// Chat area
	chatWidth := m.width - sidebarWidth - 2
	inputHeight := 5

	// Title
	title := titleStyle.Render(" " + m.channelTitle())

	chatContent := chatStyle.Width(chatWidth - 4).Render(m.viewport.View())
	inputContent := inputStyle.Width(chatWidth - 2).Render(m.input.View())

	rightPanel := lipgloss.JoinVertical(lipgloss.Left, title, chatContent, inputContent)

	main := lipgloss.JoinHorizontal(lipgloss.Top, sidebarContent, rightPanel)

	_ = inputHeight
	return lipgloss.JoinVertical(lipgloss.Left, main, status)
}

// --- Layout ---

func (m *model) updateLayout() {
	sidebarWidth := m.width * 35 / 100
	if sidebarWidth < 20 {
		sidebarWidth = 20
	}
	if sidebarWidth > 40 {
		sidebarWidth = 40
	}

	chatWidth := m.width - sidebarWidth - 8
	chatHeight := m.height - 10

	if chatHeight < 5 {
		chatHeight = 5
	}

	m.sidebar.SetSize(sidebarWidth-4, m.height-4)
	m.viewport.Width = chatWidth
	m.viewport.Height = chatHeight
	m.input.SetWidth(chatWidth)
	m.renderMessages()
}

// --- Rendering ---

func (m *model) renderMessages() {
	var sb strings.Builder
	// Messages are newest-first from the server, reverse for display
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := m.messages[i]
		ts := timeStyle.Render(msg.At.Local().Format("15:04"))
		from := fromStyle.Render(msg.From)
		body := msg.Body
		if msg.File != nil {
			body += fmt.Sprintf(" [file: %s]", *msg.File)
		}
		sb.WriteString(fmt.Sprintf("%s %s: %s\n", ts, from, body))
	}
	m.viewport.SetContent(sb.String())
	m.viewport.GotoBottom()
}

func (m model) channelTitle() string {
	if m.selKind == "inbox" {
		return "inbox"
	}
	prefix := ""
	switch m.selKind {
	case "group":
		prefix = "# "
	case "board":
		prefix = "@ "
	case "dm":
		prefix = ""
	}
	return prefix + m.selected
}

func (m model) agentName() string {
	if m.me != nil {
		return m.me.Name
	}
	return "sshmail"
}

// --- Channel list ---

func (m *model) buildChannelList(agents []Agent) {
	var items []list.Item

	// Inbox first
	items = append(items, channelItem{name: "inbox", kind: "inbox"})

	// Groups and public channels
	var boards []channelItem
	var dms []channelItem

	for _, a := range agents {
		if a.Name == "board" || a.Public {
			boards = append(boards, channelItem{name: a.Name, kind: "board", public: true})
		}
	}

	// DMs: agents that are real people (have invitedBy or are not public/groups)
	for _, a := range agents {
		if !a.Public && a.InvitedBy > 0 && m.me != nil && a.Name != m.me.Name {
			dms = append(dms, channelItem{name: a.Name, kind: "dm"})
		}
	}

	for _, b := range boards {
		items = append(items, b)
	}
	for _, d := range dms {
		items = append(items, d)
	}

	m.channels = append(boards, dms...)
	m.sidebar.SetItems(items)
}

// --- Commands ---

func (m model) fetchWhoami() tea.Msg {
	agent, err := m.client.Whoami()
	if err != nil {
		return errMsg{err}
	}
	return whoamiMsg{agent}
}

func (m model) fetchAgents() tea.Msg {
	agents, err := m.client.Agents()
	if err != nil {
		return errMsg{err}
	}
	return agentsMsg{agents}
}

func (m model) fetchInbox() tea.Msg {
	msgs, err := m.client.Inbox(true)
	if err != nil {
		return errMsg{err}
	}
	return inboxMsg{msgs}
}

func (m model) fetchChannel(item channelItem) tea.Cmd {
	return func() tea.Msg {
		switch item.kind {
		case "board":
			msgs, err := m.client.Board(item.name)
			if err != nil {
				return errMsg{err}
			}
			return boardMsg{msgs}
		case "inbox":
			msgs, err := m.client.Inbox(false)
			if err != nil {
				return errMsg{err}
			}
			return inboxMsg{msgs}
		default:
			// DMs and groups show up in inbox
			msgs, err := m.client.Inbox(true)
			if err != nil {
				return errMsg{err}
			}
			// Filter to messages involving this agent/group
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

func (m model) sendMessage(text string) tea.Cmd {
	return func() tea.Msg {
		target := m.selected
		if m.selKind == "inbox" {
			return errMsg{fmt.Errorf("select a channel or DM to send")}
		}
		result, err := m.client.Send(target, text)
		if err != nil {
			return errMsg{err}
		}
		return sentMsg{result.ID}
	}
}

func (m model) pollTick() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		unread, err := m.client.Poll()
		if err != nil {
			return pollErrMsg{err}
		}
		return pollMsg{unread}
	})
}

// --- Delegates ---

func activeDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.ShowDescription = false
	d.SetHeight(1)
	d.SetSpacing(0)
	return d
}

func dimDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.ShowDescription = false
	d.SetHeight(1)
	d.SetSpacing(0)
	d.Styles.SelectedTitle = d.Styles.NormalTitle
	return d
}

// --- Main ---

func main() {
	host, port, keyPath := getConfig()

	client, err := NewClient(host, port, keyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(
		initialModel(client),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
