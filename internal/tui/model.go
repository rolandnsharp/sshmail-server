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

// CharmTone palette — matching Crush
var (
	bg          = lipgloss.Color("#000000") // Black — base background
	bgHighlight = lipgloss.Color("#3A3943") // Charcoal — selected items
	divider     = lipgloss.Color("#3A3943") // Charcoal — separator line
	textMuted   = lipgloss.Color("#858392") // Squid
	textNormal  = lipgloss.Color("#BFBCC8") // Smoke
	textBright  = lipgloss.Color("#DFDBDD") // Ash
	accent      = lipgloss.Color("#6B50FF") // Charple
	accentWarm  = lipgloss.Color("#FF60FF") // Dolly
	accentGreen = lipgloss.Color("#68FFD6") // Bok
	accentPink  = lipgloss.Color("#EB4268") // Sriracha
	accentCyan  = lipgloss.Color("#00A4FF") // Malibu

	// Name colors — CharmTone palette for per-user coloring
	nameColors = [][3]int{
		{107, 80, 255},  // Charple
		{255, 96, 255},  // Dolly
		{104, 255, 214}, // Bok
		{235, 66, 104},  // Sriracha
		{0, 164, 255},   // Malibu
		{255, 175, 100}, // warm orange
		{150, 206, 180}, // mint
		{255, 154, 162}, // salmon
		{184, 147, 255}, // lavender
	}

	fromStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accentGreen)

	timeStyle = lipgloss.NewStyle().
			Foreground(accent).
			Faint(true)

	unreadStyle = lipgloss.NewStyle().
			Foreground(accentPink).
			Bold(true)

	accentStyle = lipgloss.NewStyle().
			Foreground(textMuted).
			Bold(true).
			PaddingTop(1)

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
		Background(bg).
		Padding(0, 0, 0, 2)
	sidebar := list.New([]list.Item{}, delegate, 0, 0)
	sidebar.SetShowTitle(true)
	sidebar.Title = "sshmail.dev"
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
	input.SetHeight(2)
	input.CharLimit = 4096
	input.MaxHeight = 6
	// Shift+Enter inserts newline; Enter handled by us to send
	input.KeyMap.InsertNewline.SetKeys("shift+enter")
	// Style the input area with theme background
	inputBg := lipgloss.Color("#2D2C35") // BBQ — input area background
	input.FocusedStyle.Base = lipgloss.NewStyle().Background(inputBg)
	input.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(textMuted).Background(inputBg)
	input.FocusedStyle.Text = lipgloss.NewStyle().Foreground(textBright).Background(inputBg)
	input.FocusedStyle.CursorLine = lipgloss.NewStyle().Background(inputBg)
	input.BlurredStyle.Base = lipgloss.NewStyle().Background(inputBg)
	input.BlurredStyle.Placeholder = lipgloss.NewStyle().Foreground(textMuted).Background(inputBg)
	input.BlurredStyle.Text = lipgloss.NewStyle().Foreground(textNormal).Background(inputBg)

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
		// Scroll wheel: sidebar or chat depending on mouse position
		if msg.Button == tea.MouseButtonWheelUp {
			if msg.X <= sidebarWidth {
				m.sidebar.CursorUp()
				if cmd := m.syncSelection(); cmd != nil {
					return m, cmd
				}
				return m, nil
			} else {
				m.viewport.LineUp(3)
				return m, nil
			}
		}
		if msg.Button == tea.MouseButtonWheelDown {
			if msg.X <= sidebarWidth {
				m.sidebar.CursorDown()
				if cmd := m.syncSelection(); cmd != nil {
					return m, cmd
				}
				return m, nil
			} else {
				m.viewport.LineDown(3)
				return m, nil
			}
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
			topOffset := 5
			clickedItem := msg.Y - topOffset
			if clickedItem >= 0 && clickedItem < len(m.sidebar.Items()) {
				m.sidebar.Select(clickedItem)
				if cmd := m.syncSelection(); cmd != nil {
					return m, cmd
				}
			}
			m.status = ""
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
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
					m.input.SetHeight(2)
					m.updateLayout()
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
			prevHeight := m.input.Height()
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			cmds = append(cmds, cmd)
			if m.input.Height() != prevHeight {
				m.updateLayout()
			}
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

	line := lipgloss.NewStyle().Background(bg)
	sidebarWidth := m.width * 30 / 100
	if sidebarWidth < 20 {
		sidebarWidth = 20
	}
	if sidebarWidth > 35 {
		sidebarWidth = 35
	}
	chatWidth := m.width - sidebarWidth - 1 // 1 for divider

	sep := lipgloss.NewStyle().Foreground(divider).Background(bg).Render("│")
	channelName := m.channelTitle()
	if m.focus == focusInput {
		channelName += " ✎"
	}

	// Panel = everything except the status bar at the bottom
	panelHeight := m.height - 3 // just 1 row for status
	if panelHeight < 5 {
		panelHeight = 5
	}
	chatHeight := panelHeight - 2 // 1 for channel title, 1 for input

	// Sidebar: first line is branding
	sidebarLines := make([]string, 0, panelHeight)
	sidebarLines = append(sidebarLines,
		lipgloss.NewStyle().Bold(true).Foreground(accent).Background(bgHighlight).
			Width(sidebarWidth).MaxWidth(sidebarWidth).Render(" sshmail.dev"))
	// sbLine renders a single sidebar line, truncated to fit (never wraps)
	maxText := sidebarWidth - 2 // 2 for padding
	truncate := func(s string) string {
		if len(s) > maxText {
			return s[:maxText-1] + "…"
		}
		return s
	}
	sbLine := func(text string, style lipgloss.Style) string {
		return style.Width(sidebarWidth).MaxWidth(sidebarWidth).Render(text)
	}

	items := m.sidebar.Items()
	sel := m.sidebar.Index()
	for idx, item := range items {
		ci := item.(channelItem)
		if ci.kind == "header" || ci.kind == "hint" {
			style := lipgloss.NewStyle().Foreground(textMuted).Bold(true).Background(bg).Padding(0, 1)
			if ci.kind == "hint" {
				style = hintStyle.Background(bg).Padding(0, 1)
			}
			if idx > 0 {
				sidebarLines = append(sidebarLines, sbLine("", line))
			}
			sidebarLines = append(sidebarLines, sbLine(style.Render(truncate(ci.name)), line))
			continue
		}
		prefix := "  "
		if ci.kind == "group" {
			prefix = "# "
		} else if ci.kind == "board" {
			prefix = "@ "
		}
		label := truncate(prefix + ci.name)
		if ci.unread > 0 {
			label += unreadStyle.Render(fmt.Sprintf(" (%d)", ci.unread))
		}
		if idx == sel && m.focus == focusSidebar {
			sidebarLines = append(sidebarLines, sbLine(label,
				lipgloss.NewStyle().Background(bgHighlight).Foreground(accent).Bold(true).Padding(0, 1)))
		} else if idx == sel {
			sidebarLines = append(sidebarLines, sbLine(label,
				lipgloss.NewStyle().Background(bgHighlight).Foreground(textBright).Padding(0, 1)))
		} else {
			sidebarLines = append(sidebarLines, sbLine(label,
				line.Foreground(textNormal).Padding(0, 1)))
		}
	}
	emptyLine := line.Width(sidebarWidth).MaxWidth(sidebarWidth).Render("")
	for len(sidebarLines) < panelHeight {
		sidebarLines = append(sidebarLines, emptyLine)
	}
	if len(sidebarLines) > panelHeight {
		sidebarLines = sidebarLines[:panelHeight]
	}

	// Right side: channel title + chat lines + input line
	chatLineStyle := lipgloss.NewStyle().Background(bg).Width(chatWidth).MaxWidth(chatWidth).Padding(0, 1)
	rightLines := make([]string, 0, panelHeight)
	// Channel title as first right line (matches branding on left)
	rightLines = append(rightLines,
		lipgloss.NewStyle().Bold(true).Foreground(textBright).Background(bgHighlight).
			Width(chatWidth).MaxWidth(chatWidth).Render(" "+channelName))
	// Replace ANSI resets in viewport content to preserve background color
	bgAnsi := "\033[48;2;0;0;0m" // Black #000000
	vpContent := strings.ReplaceAll(m.viewport.View(), "\033[0m", "\033[0m"+bgAnsi)
	chatLines := strings.Split(strings.TrimRight(vpContent, "\n"), "\n")
	for i := 0; i < chatHeight && i < len(chatLines); i++ {
		rightLines = append(rightLines, chatLineStyle.Render(chatLines[i]))
	}
	for len(rightLines) < chatHeight { // fill up to input
		rightLines = append(rightLines, chatLineStyle.Render(""))
	}
	// Input area at bottom of right panel — render with raw ANSI for full background
	inputBgAnsi := "\033[48;2;45;44;53m" // BBQ #2D2C35
	val := m.input.Value()
	inputLines := strings.Split(val, "\n")
	inputHeight := m.input.Height()
	for i := 0; i < inputHeight; i++ {
		content := ""
		if i < len(inputLines) {
			content = inputLines[i]
		}
		if content == "" && i == 0 && val == "" {
			content = "\033[38;2;133;131;146mtype a message...\033[0m"
		}
		// Pad to full chat width with background
		pad := chatWidth - lipgloss.Width(content) - 2 // 2 for left padding
		if pad < 0 {
			pad = 0
		}
		line := inputBgAnsi + " " + content + strings.Repeat(" ", pad+1) + "\033[0m"
		rightLines = append(rightLines, line)
	}

	// Build all lines: panel rows + status
	allLines := make([]string, 0, m.height)
	for i := 0; i < panelHeight; i++ {
		allLines = append(allLines, sidebarLines[i]+sep+rightLines[i])
	}
	allLines = append(allLines, lipgloss.NewStyle().Foreground(textMuted).Background(bgHighlight).
		Width(m.width).
		Render(" "+m.status+"  "+m.helpText()))

	// Cap to terminal height
	if len(allLines) > m.height {
		allLines = allLines[:m.height]
	}
	// Wrap entire view in a full-screen background
	return lipgloss.NewStyle().
		Background(bg).
		Width(m.width).
		Height(m.height).
		Render(strings.Join(allLines, "\n"))
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
	key := lipgloss.NewStyle().Foreground(textBright)
	sep := " · "
	if m.focus == focusInput {
		return key.Render("enter") + " send" + sep +
			key.Render("shift+enter") + " newline" + sep +
			key.Render("tab") + " sidebar" + sep +
			key.Render("esc") + " escape"
	}
	return key.Render("↑↓") + " navigate" + sep +
		key.Render("enter") + " select" + sep +
		key.Render("tab") + " write" + sep +
		key.Render("esc") + " escape"
}

// --- Layout ---

func (m *Model) updateLayout() {
	sidebarWidth := m.width * 30 / 100
	if sidebarWidth < 20 {
		sidebarWidth = 20
	}
	if sidebarWidth > 35 {
		sidebarWidth = 35
	}

	chatWidth := m.width - sidebarWidth - 3 // 1 divider + 2 padding
	panelHeight := m.height - 3             // just status bar
	inputHeight := m.input.Height()
	chatHeight := panelHeight - 1 - inputHeight // branding row + input rows

	if chatHeight < 3 {
		chatHeight = 3
	}

	m.sidebar.SetSize(sidebarWidth, panelHeight)
	m.viewport.Width = chatWidth
	m.viewport.Height = chatHeight
	m.input.SetWidth(chatWidth)
	m.renderMessages()
}

// wrapText wraps s at word boundaries using lipgloss.
// nameColorFor returns an ANSI foreground escape for a username, consistent per name.
func nameColorFor(name string) string {
	h := 0
	for _, c := range name {
		h = h*31 + int(c)
	}
	if h < 0 {
		h = -h
	}
	c := nameColors[h%len(nameColors)]
	return fmt.Sprintf("\033[1;38;2;%d;%d;%dm", c[0], c[1], c[2])
}

func wrapText(s string, maxWidth int) string {
	return lipgloss.NewStyle().Width(maxWidth).Render(s)
}

// --- Rendering ---

func (m *Model) renderMessages() {
	fileStyle := lipgloss.NewStyle().Foreground(accentWarm)

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithWordWrap(m.viewport.Width-10),
		glamour.WithStylePath("dark"),
	)

	var sb strings.Builder
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := m.messages[i]
		ts := "\033[38;2;133;131;146m" + msg.At.Local().Format("15:04") + "\033[0m"
		from := nameColorFor(msg.From) + msg.From + ":\033[0m"
		header := ts + " " + from

		body := msg.Body
		isSimple := !strings.Contains(body, "\n") && !strings.Contains(body, "```")

		if !isSimple && renderer != nil {
			if rendered, err := renderer.Render(body); err == nil {
				body = strings.TrimSpace(rendered)
			}
		}

		if msg.File != nil {
			body += fileStyle.Render(fmt.Sprintf(" [%s]", *msg.File))
		}

		if isSimple {
			maxWidth := m.viewport.Width - 2
			line := header + " " + body
			if len(line) <= maxWidth {
				sb.WriteString(line + "\n")
			} else {
				sb.WriteString(header + "\n" + wrapText(body, maxWidth) + "\n")
			}
		} else {
			sb.WriteString(header + "\n" + body + "\n")
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
		case a.InvitedBy > 0:
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
		Background(bg).
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
		Background(bg).
		Padding(0, 1)
	return d
}
