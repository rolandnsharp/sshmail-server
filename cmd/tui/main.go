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

// Dark theme — inspired by Charmbracelet Crush
// Deep background with soft purples, warm accents, muted text
var (
	// Base colors
	bgDark      = lipgloss.Color("#1a1b26") // deep navy/purple bg
	bgPanel     = lipgloss.Color("#24283b") // slightly lighter panel bg
	bgHighlight = lipgloss.Color("#2f3349") // selected/hover bg
	border      = lipgloss.Color("#414868") // muted border
	borderFocus = lipgloss.Color("#7aa2f7") // focused border — soft blue
	textMuted   = lipgloss.Color("#565f89") // dim text
	textNormal  = lipgloss.Color("#a9b1d6") // normal text
	textBright  = lipgloss.Color("#c0caf5") // bright text
	accent      = lipgloss.Color("#bb9af7") // purple accent
	accentWarm  = lipgloss.Color("#e0af68") // warm yellow accent
	accentGreen = lipgloss.Color("#9ece6a") // green for success
	accentPink  = lipgloss.Color("#f7768e") // pink for unread/alerts
	accentCyan  = lipgloss.Color("#7dcfff") // cyan for usernames

	sidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			BorderBackground(bgDark).
			Background(bgDark).
			Foreground(textNormal).
			Padding(0, 1)

	sidebarFocusStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(borderFocus).
				BorderBackground(bgDark).
				Background(bgDark).
				Foreground(textNormal).
				Padding(0, 1)

	chatStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			BorderBackground(bgDark).
			Background(bgDark).
			Padding(0, 1)

	inputStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			BorderBackground(bgDark).
			Background(bgPanel).
			Foreground(textNormal)

	inputFocusStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderFocus).
			BorderBackground(bgDark).
			Background(bgPanel).
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

	helpStyle = lipgloss.NewStyle().
			Foreground(textMuted).
			Background(bgDark).
			Padding(0, 1)

	sectionStyle = lipgloss.NewStyle().
			Foreground(accentWarm).
			Bold(true)
)

// --- Sidebar item ---

type channelItem struct {
	name    string
	kind    string // "group", "board", "dm", "inbox", "allmail"
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

type sectionItem struct {
	title string
}

func (i sectionItem) Title() string       { return sectionStyle.Render(i.title) }
func (i sectionItem) Description() string { return "" }
func (i sectionItem) FilterValue() string { return i.title }

// --- Messages ---

type pollMsg struct{ unread int }
type pollErrMsg struct{ err error }
type inboxMsg struct{ messages []Message }
type boardMsg struct{ messages []Message }
type agentsMsg struct{ agents []Agent }
type whoamiMsg struct{ agent *Agent }
type sentMsg struct {
	id     int64
	target string
	kind   string
}
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
	agents   []Agent
	selected string // currently selected channel/DM name
	selKind  string // "board", "group", "dm", "inbox"
	status   string
	unread   int
	unreadBy map[string]int
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
		status:   "connecting... enter opens Inbox, tab switches to compose",
		unreadBy: map[string]int{},
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.fetchWhoami,
		m.fetchAgents,
		m.fetchUnreadInbox,
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
				return m, tea.Quit
			case "i":
				if m.focus == focusSidebar {
					m.focus = focusInput
					m.input.Focus()
					m.sidebar.SetDelegate(dimDelegate())
					return m, nil
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
		case "up", "k":
			if m.focus == focusSidebar {
				var cmd tea.Cmd
				m.sidebar, cmd = m.sidebar.Update(msg)
				m.skipSectionSelection(-1)
				return m, cmd
			}
		case "down", "j":
			if m.focus == focusSidebar {
				var cmd tea.Cmd
				m.sidebar, cmd = m.sidebar.Update(msg)
				m.skipSectionSelection(1)
				return m, cmd
			}
			case "esc":
				if m.focus == focusInput {
					m.focus = focusSidebar
					m.input.Blur()
					m.sidebar.SetDelegate(activeDelegate())
					return m, nil
				}
			}

			if m.focus == focusSidebar && msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
				r := msg.Runes[0]
				if !strings.ContainsRune("\n\r\t", r) && r >= 32 {
					m.focus = focusInput
					m.input.Focus()
					m.sidebar.SetDelegate(dimDelegate())
					m.input.SetValue(string(r))
					m.input.SetCursor(len(m.input.Value()))
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
			if len(m.agents) > 0 {
				m.buildChannelList(m.agents)
				m.updateLayout()
			}

	case agentsMsg:
			m.agents = msg.agents
			m.buildChannelList(msg.agents)
			m.updateLayout()

		case inboxMsg:
				m.messages = msg.messages
				m.renderMessages()
				if m.selKind == "allmail" {
					m.status = fmt.Sprintf("all mail — %d messages", len(msg.messages))
				} else if m.selKind == "inbox" {
					if m.unread > 0 {
						m.status = fmt.Sprintf("inbox — %d unread messages", len(msg.messages))
					} else {
						m.status = fmt.Sprintf("inbox — %d messages", len(msg.messages))
					}
				} else {
					m.status = fmt.Sprintf("%s — %d messages", m.channelTitle(), len(msg.messages))
				}
				m.recomputeUnreadMap(msg.messages)
				m.updateUnreadBadge()

	case boardMsg:
			m.messages = msg.messages
			m.renderMessages()
		m.status = fmt.Sprintf("%s — %d messages", m.selected, len(msg.messages))

		case pollMsg:
				m.unread = msg.unread
				if m.selKind == "allmail" {
					cmds = append(cmds, m.fetchInbox)
				} else {
					cmds = append(cmds, m.fetchUnreadInbox)
				}
				if m.selKind == "inbox" {
					if msg.unread > 0 {
						m.status = fmt.Sprintf("%s — %d unread (Inbox is selected)", m.agentName(), msg.unread)
					} else {
						m.status = fmt.Sprintf("%s — inbox clear", m.agentName())
					}
				} else if m.selKind == "allmail" {
					m.status = fmt.Sprintf("%s — %d unread (All Mail stays open)", m.agentName(), msg.unread)
				} else {
					m.status = fmt.Sprintf("%s — %d unread (select Inbox and press Enter)", m.agentName(), msg.unread)
				}
			cmds = append(cmds, m.pollTick())

	case pollErrMsg:
		cmds = append(cmds, m.pollTick())

		case sentMsg:
			m.status = fmt.Sprintf("sent to %s as #%d", m.channelLabel(msg.target, msg.kind), msg.id)
			// Refresh current view
			if m.selKind == "inbox" {
				cmds = append(cmds, m.fetchUnreadInbox)
			} else if m.selKind == "allmail" {
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
	status := statusStyle.Width(m.width).Render(" " + m.status)
	help := helpStyle.Width(m.width).Render(" enter open/send  tab switch focus  esc sidebar  q quit ")

	// Sidebar
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

	// Chat area
	chatWidth := m.width - sidebarWidth - 2

	// Title
	title := titleStyle.Width(chatWidth - 2).Render(m.channelTitle())

	chatContent := chatStyle.Width(chatWidth - 4).Render(m.viewport.View())

	inStyle := inputStyle
	if m.focus == focusInput {
		inStyle = inputFocusStyle
	}
	inputContent := inStyle.Width(chatWidth - 2).Render(m.input.View())

	rightPanel := lipgloss.JoinVertical(lipgloss.Left, title, chatContent, inputContent)

	main := lipgloss.JoinHorizontal(lipgloss.Top, sidebarContent, rightPanel)

	content := lipgloss.JoinVertical(lipgloss.Left, main, help, status)

	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Left, lipgloss.Top,
		content,
		lipgloss.WithWhitespaceBackground(bgDark),
	)
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
	bodyStyle := lipgloss.NewStyle().Foreground(textNormal).Background(bgDark)
	fileStyle := lipgloss.NewStyle().Foreground(accentWarm).Background(bgDark)
	lineStyle := lipgloss.NewStyle().Background(bgDark).Width(m.viewport.Width)
	metaStyle := lipgloss.NewStyle().Background(bgDark)
	bodyWidth := m.viewport.Width - 2
	if bodyWidth < 20 {
		bodyWidth = 20
	}

	var sb strings.Builder
	// Messages are newest-first from the server, reverse for display
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := m.messages[i]
		ts := timeStyle.Background(bgDark).Render(msg.At.Local().Format("15:04"))
		from := fromStyle.Background(bgDark).Render(msg.From)
		header := metaStyle.Render(fmt.Sprintf("%s %s", ts, from))
		body := bodyStyle.Width(bodyWidth).Render(strings.TrimSpace(msg.Body))
		if msg.File != nil {
			body += "\n" + fileStyle.Render(fmt.Sprintf("attachment: %s", *msg.File))
		}
		sb.WriteString(lineStyle.Render(header) + "\n")
		sb.WriteString(lineStyle.Render(body) + "\n\n")
	}
	m.viewport.SetContent(sb.String())
	m.viewport.GotoBottom()
}

func (m model) channelTitle() string {
	if m.selKind == "inbox" {
		return "inbox"
	}
	if m.selKind == "allmail" {
		return "all mail"
	}
	return m.channelLabel(m.selected, m.selKind)
}

func (m model) channelLabel(name, kind string) string {
	prefix := ""
	switch kind {
	case "group":
		prefix = "# "
	case "board":
		prefix = "@ "
	case "dm":
		prefix = ""
	}
	return prefix + name
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

	// Inbox views first
	items = append(items, channelItem{name: "inbox", kind: "inbox", unread: m.unread})
	items = append(items, channelItem{name: "all mail", kind: "allmail"})

	// Public boards
	var boards []channelItem
	var groups []channelItem
	var dms []channelItem

	for _, a := range agents {
		if a.Name == "board" || a.Public {
			boards = append(boards, channelItem{name: a.Name, kind: "board", public: true, unread: m.unreadBy[a.Name]})
			continue
		}
		if strings.HasPrefix(a.Fingerprint, "group:") {
			groups = append(groups, channelItem{name: a.Name, kind: "group", unread: m.unreadBy[a.Name]})
		}
	}

	// DMs: real people excluding self and non-public groups.
	for _, a := range agents {
		if !a.Public && !strings.HasPrefix(a.Fingerprint, "group:") && a.InvitedBy > 0 && m.me != nil && a.Name != m.me.Name {
			dms = append(dms, channelItem{name: a.Name, kind: "dm", unread: m.unreadBy[a.Name]})
		}
	}

	if len(boards) > 0 {
		items = append(items, sectionItem{title: "Boards"})
		for _, b := range boards {
			items = append(items, b)
		}
	}
	if len(groups) > 0 {
		items = append(items, sectionItem{title: "Groups"})
		for _, g := range groups {
			items = append(items, g)
		}
	}
	if len(dms) > 0 {
		items = append(items, sectionItem{title: "Direct Messages"})
		for _, d := range dms {
			items = append(items, d)
		}
	}

	m.channels = append(append([]channelItem{}, boards...), append(groups, dms...)...)
	m.sidebar.SetItems(items)
}

func (m *model) updateUnreadBadge() {
	items := m.sidebar.Items()
	if len(items) == 0 {
		return
	}
	for idx, item := range items {
		ch, ok := item.(channelItem)
		if !ok {
			continue
		}
		if ch.kind == "inbox" {
			ch.unread = m.unread
		} else if ch.kind == "allmail" {
			ch.unread = 0
		} else {
			ch.unread = m.unreadBy[ch.name]
		}
		items[idx] = ch
	}
	m.sidebar.SetItems(items)
}

func (m *model) recomputeUnreadMap(msgs []Message) {
	next := map[string]int{}
	for _, msg := range msgs {
		target := msg.To
		if m.me != nil && msg.To == m.me.Name {
			target = msg.From
		}
		next[target]++
	}
	m.unreadBy = next
	if len(m.agents) > 0 {
		m.buildChannelList(m.agents)
	}
}

func (m *model) skipSectionSelection(direction int) {
	items := m.sidebar.Items()
	if len(items) == 0 {
		return
	}
	idx := m.sidebar.Index()
	for idx >= 0 && idx < len(items) {
		if _, ok := items[idx].(sectionItem); !ok {
			m.sidebar.Select(idx)
			return
		}
		idx += direction
	}
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

func (m model) fetchUnreadInbox() tea.Msg {
	msgs, err := m.client.Inbox(false)
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
		case "allmail":
			msgs, err := m.client.Inbox(true)
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
		if m.selKind == "inbox" || m.selKind == "allmail" {
			return errMsg{fmt.Errorf("select a channel or DM to send")}
		}
		result, err := m.client.Send(target, text)
		if err != nil {
			return errMsg{err}
		}
		return sentMsg{id: result.ID, target: target, kind: m.selKind}
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
