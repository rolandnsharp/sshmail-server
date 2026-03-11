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
	bg          = lipgloss.Color("#1F1C23") // Crush background
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
		prefix = "@ "
	} else if i.kind == "board" {
		prefix = "# "
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
type fileContentMsg struct{ name, content string }
type onlineMsg struct{ online map[string]bool }

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
	online    map[string]bool
}

func NewModel(backend Backend) Model {
	sidebar := list.New([]list.Item{}, activeDelegate(), 0, 0)
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
	input.SetHeight(3)
	input.CharLimit = 4096
	input.MaxHeight = 6
	// Alt+Enter inserts newline; Enter handled by us to send
	// (shift+enter doesn't work over SSH — terminals send the same escape code as enter)
	input.KeyMap.InsertNewline.SetKeys("alt+enter")
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

	input.Blur()

	return Model{
		backend:  backend,
		focus:    focusSidebar,
		sidebar:  sidebar,
		viewport: vp,
		input:    input,
		selected: "readme",
		selKind:  "readme",
		status:   "connecting...",
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.fetchWhoami,
		m.fetchAgents,
		m.startWatch(),
		m.fetchRepoFiles,
		m.fetchOnline,
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
		sidebarWidth := m.sidebarWidth()
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
					m.input.SetHeight(3)
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
			if item, ok := m.sidebar.SelectedItem().(channelItem); ok && item.kind != "header" && item.kind != "file" && item.kind != "hint" && item.kind != "spacer" {
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
		if m.selKind == "readme" {
			m.sidebar.Select(1) // index 1 = readme (after spacer)
			m.showReadme()
		}

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
		cmds = append(cmds, m.fetchOnline)

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
		cmds = append(cmds, m.fetchChannel(channelItem{name: m.selected, kind: m.selKind}))
		cmds = append(cmds, m.pollTick())
		cmds = append(cmds, m.fetchOnline)

	case pollErrMsg:
		cmds = append(cmds, m.pollTick())

	case sentMsg:
		m.status = fmt.Sprintf("sent #%d", msg.id)
		cmds = append(cmds, m.fetchChannel(channelItem{name: m.selected, kind: m.selKind}))

	case fileContentMsg:
		content := msg.content
		if strings.HasSuffix(msg.name, ".md") {
			renderer, _ := glamour.NewTermRenderer(
				glamour.WithWordWrap(m.viewport.Width-4),
				glamour.WithStylePath("dark"),
			)
			if renderer != nil {
				if rendered, err := renderer.Render(content); err == nil {
					content = rendered
				}
			}
		}
		m.viewport.SetContent(content)
		m.viewport.GotoTop()
		m.status = fmt.Sprintf("file: %s", msg.name)

	case onlineMsg:
		m.online = msg.online

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

// ANSI escape constants used across rendering functions.
const (
	topBarBgAnsi = "\033[48;2;107;80;255m" // Charple #6B50FF
	topBarFgAnsi = "\033[1;38;2;0;0;0m"    // Black bold
	bgAnsi       = "\033[48;2;31;28;35m"   // Crush background #1F1C23
)

func (m Model) View() string {
	if m.width == 0 {
		return "loading..."
	}

	sidebarWidth := m.sidebarWidth()
	chatWidth := m.width - sidebarWidth - 1 // 1 for divider
	panelHeight := m.panelHeight()
	inputHeight := m.input.Height()
	chatHeight := panelHeight - 1 - inputHeight // 1 for channel title, rest for input

	sidebarLines := m.renderSidebar(sidebarWidth, panelHeight)
	rightLines := m.renderRightPanel(chatWidth, chatHeight, panelHeight, inputHeight)

	// Build all lines: panel rows + status
	sep := lipgloss.NewStyle().Foreground(divider).Background(bg).Render("│")
	allLines := make([]string, 0, m.height)
	for i := 0; i < panelHeight; i++ {
		allLines = append(allLines, sidebarLines[i]+sep+rightLines[i])
	}
	allLines = append(allLines, m.renderStatusBar())

	// Pad each line to full terminal width and fill to full height
	for i, l := range allLines {
		w := lipgloss.Width(l)
		if w < m.width {
			allLines[i] = l + bgAnsi + strings.Repeat(" ", m.width-w) + "\033[0m"
		}
	}
	for len(allLines) < m.height {
		allLines = append(allLines, bgAnsi+strings.Repeat(" ", m.width)+"\033[0m")
	}
	if len(allLines) > m.height {
		allLines = allLines[:m.height]
	}
	return strings.Join(allLines, "\n")
}

func (m Model) sidebarWidth() int {
	w := m.width * 30 / 100
	if w < 20 {
		w = 20
	}
	if w > 35 {
		w = 35
	}
	return w
}

func (m Model) panelHeight() int {
	h := m.height - 1 // 1 row for status bar
	if h < 5 {
		h = 5
	}
	return h
}

func (m Model) renderSidebar(sidebarWidth, panelHeight int) []string {
	line := lipgloss.NewStyle().Background(bg)
	lines := make([]string, 0, panelHeight)

	// Top bar — branding
	brandText := topBarBgAnsi + topBarFgAnsi + " sshmail.dev"
	if pad := sidebarWidth - 12; pad > 0 {
		brandText += strings.Repeat(" ", pad)
	}
	brandText += "\033[0m"
	lines = append(lines, brandText)

	maxText := sidebarWidth - 4 // padding + prefix
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
		if ci.kind == "spacer" {
			lines = append(lines, sbLine("", line))
			continue
		}
		if ci.kind == "header" || ci.kind == "hint" {
			style := lipgloss.NewStyle().Foreground(textMuted).Bold(true).Background(bg).Padding(0, 1)
			if ci.kind == "hint" {
				style = hintStyle.Background(bg).Padding(0, 1)
			}
			if idx > 0 {
				lines = append(lines, sbLine("", line))
			}
			lines = append(lines, sbLine(style.Render(truncate(ci.name)), line))
			continue
		}
		prefix := "  "
		if ci.kind == "group" {
			prefix = "@ "
		} else if ci.kind == "board" {
			prefix = "# "
		}
		isOnline := ci.kind == "dm" && m.online[ci.name]
		name := truncate(ci.name)
		if ci.unread > 0 {
			name += unreadStyle.Render(fmt.Sprintf(" (%d)", ci.unread))
		}
		var itemStyle lipgloss.Style
		if idx == sel && m.focus == focusSidebar {
			itemStyle = lipgloss.NewStyle().Background(bgHighlight).Foreground(accent).Bold(true).Padding(0, 1)
		} else if idx == sel {
			itemStyle = lipgloss.NewStyle().Background(bgHighlight).Foreground(textBright).Padding(0, 1)
		} else {
			itemStyle = line.Foreground(textNormal).Padding(0, 1)
		}
		// Render the full line first, then inject the green dot via ANSI
		// without any resets that would break the background
		rendered := sbLine(prefix+name, itemStyle)
		if isOnline {
			// Replace the leading "  " with "● " using ANSI fg-only color change
			// \033[38;2;r;g;bm sets foreground without resetting background
			greenFg := "\033[38;2;104;255;214m"
			restoreFg := "\033[38;2;191;188;200m" // textNormal
			if idx == sel && m.focus == focusSidebar {
				restoreFg = "\033[1;38;2;107;80;255m" // accent bold
			} else if idx == sel {
				restoreFg = "\033[38;2;223;219;221m" // textBright
			}
			rendered = strings.Replace(rendered, "  "+name, greenFg+"●"+restoreFg+" "+name, 1)
		}
		lines = append(lines, rendered)
	}

	emptyLine := line.Width(sidebarWidth).MaxWidth(sidebarWidth).Render("")
	for len(lines) < panelHeight {
		lines = append(lines, emptyLine)
	}
	if len(lines) > panelHeight {
		lines = lines[:panelHeight]
	}
	return lines
}

func (m Model) renderRightPanel(chatWidth, chatHeight, panelHeight, inputHeight int) []string {
	lines := make([]string, 0, panelHeight)

	// Channel title bar
	channelName := m.channelTitle()
	if m.focus == focusInput {
		channelName += " ✎"
	}
	chanText := topBarBgAnsi + topBarFgAnsi + " " + channelName
	if pad := chatWidth - lipgloss.Width(" "+channelName); pad > 0 {
		chanText += strings.Repeat(" ", pad)
	}
	chanText += "\033[0m"
	lines = append(lines, chanText)

	// Viewport — re-inject bg after every ANSI reset
	// Glamour uses \033[m (short form), lipgloss uses \033[0m — normalize then replace
	vpContent := m.viewport.View()
	vpContent = strings.ReplaceAll(vpContent, "\033[0m", "\033[m")
	vpContent = strings.ReplaceAll(vpContent, "\033[m", "\033[m"+bgAnsi)
	vpLines := strings.Split(vpContent, "\n")
	for i := 0; i < chatHeight; i++ {
		if i < len(vpLines) {
			w := lipgloss.Width(vpLines[i])
			pad := chatWidth - w
			if pad < 0 {
				pad = 0
			}
			lines = append(lines, bgAnsi+vpLines[i]+bgAnsi+strings.Repeat(" ", pad)+"\033[0m")
		} else {
			lines = append(lines, bgAnsi+strings.Repeat(" ", chatWidth)+"\033[0m")
		}
	}

	// Input area
	inputBgColor := lipgloss.Color("#2D2C35") // BBQ
	inputBgAnsi := "\033[48;2;45;44;53m"      // BBQ #2D2C35
	inputView := m.input.View()
	inputView = strings.ReplaceAll(inputView, "\033[0m", "\033[0m"+inputBgAnsi)
	inputViewLines := strings.Split(inputView, "\n")
	if len(inputViewLines) > inputHeight {
		inputViewLines = inputViewLines[:inputHeight]
	}
	for _, il := range inputViewLines {
		rendered := inputBgAnsi + lipgloss.NewStyle().Background(inputBgColor).Width(chatWidth).Padding(0, 1).Render(il) + "\033[0m"
		lines = append(lines, rendered)
	}

	return lines
}

func (m Model) renderStatusBar() string {
	statusBgAnsi := "\033[48;2;107;80;255m"  // Charple #6B50FF
	statusFgAnsi := "\033[38;2;223;219;221m" // textBright
	content := " " + m.status + "  " + m.helpText()
	pad := m.width - lipgloss.Width(content)
	if pad < 0 {
		pad = 0
	}
	return statusBgAnsi + statusFgAnsi + content + strings.Repeat(" ", pad) + "\033[0m"
}

// syncSelection loads the channel matching the current sidebar highlight.
// Returns a fetch command if the selection changed, nil otherwise.
func (m *Model) syncSelection() tea.Cmd {
	item, ok := m.sidebar.SelectedItem().(channelItem)
	if !ok || item.kind == "header" || item.kind == "hint" || item.kind == "spacer" {
		return nil
	}
	if item.name == m.selected && item.kind == m.selKind {
		return nil
	}
	m.selected = item.name
	m.selKind = item.kind
	m.status = fmt.Sprintf("loading %s...", item.name)
	if item.kind == "readme" {
		m.showReadme()
		return nil
	}
	if item.kind == "file" {
		return m.fetchFile(item.name)
	}
	return m.fetchChannel(item)
}

func (m Model) helpText() string {
	// Use raw ANSI to avoid resets that break status bar background
	statusBgAnsi := "\033[48;2;107;80;255m" // Charple #6B50FF
	keyStart := "\033[1;38;2;0;0;0m"        // Black bold
	keyEnd := "\033[0m" + statusBgAnsi + "\033[38;2;223;219;221m" // reset, restore bg + textBright
	key := func(s string) string { return keyStart + s + keyEnd }
	sep := " · "
	if m.focus == focusInput {
		return key("enter") + " send" + sep +
			key("alt+enter") + " newline" + sep +
			key("tab") + " sidebar" + sep +
			key("esc") + " escape"
	}
	return key("↑↓") + " navigate" + sep +
		key("enter") + " select" + sep +
		key("tab") + " write" + sep +
		key("esc") + " escape"
}

// --- Layout ---

func (m *Model) updateLayout() {
	sidebarWidth := m.sidebarWidth()
	chatWidth := m.width - sidebarWidth - 3 // 1 divider + 2 padding
	panelHeight := m.panelHeight()
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

// --- Rendering ---

func (m *Model) renderMessages() {
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithWordWrap(m.viewport.Width-10),
		glamour.WithStylePath("dark"),
	)

	var sb strings.Builder
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := m.messages[i]
		ts := "\033[38;2;133;131;146m" + msg.At.Local().Format("15:04") + "\033[0m"
		from := nameColorFor(msg.From) + msg.From + ":\033[0m"
		header := "  " + ts + " " + from

		body := msg.Body
		if msg.File != nil {
			body += "\n\n📎 " + *msg.File
		}

		if renderer != nil {
			if rendered, err := renderer.Render(body); err == nil {
				body = strings.Trim(rendered, "\n")
			}
		}

		sb.WriteString(header + "\n" + body + "\n")
	}
	m.viewport.SetContent(sb.String())
	m.viewport.GotoBottom()
}

func (m *Model) showReadme() {
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithWordWrap(m.viewport.Width-4),
		glamour.WithStylePath("dark"),
	)
	content := Readme
	if renderer != nil {
		if rendered, err := renderer.Render(content); err == nil {
			content = rendered
		}
	}
	m.viewport.SetContent(content)
	m.viewport.GotoTop()
	m.status = "readme"
}

func (m Model) channelTitle() string {
	if m.selKind == "readme" {
		return "readme"
	}
	if m.selKind == "inbox" {
		return "inbox"
	}
	prefix := ""
	switch m.selKind {
	case "group":
		prefix = "@ "
	case "board":
		prefix = "# "
	case "file":
		return "📄 " + m.selected
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

	items = append(items, channelItem{name: "", kind: "spacer"})
	items = append(items, channelItem{name: "readme", kind: "readme"})
	items = append(items, channelItem{name: "", kind: "spacer"})
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
		case a.InvitedBy > 0 || (m.me != nil && a.Name == m.me.Name):
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
		items = append(items, channelItem{name: "git clone", kind: "hint"})
		items = append(items, channelItem{name: "ssh.sshmail.dev:" + m.me.Name, kind: "hint"})
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

func (m Model) fetchOnline() tea.Msg {
	online, err := m.backend.Online()
	if err != nil {
		return onlineMsg{nil}
	}
	return onlineMsg{online}
}

func (m Model) fetchRepoFiles() tea.Msg {
	files, err := m.backend.RepoFiles()
	if err != nil {
		return repoFilesMsg{nil}
	}
	return repoFilesMsg{files}
}

func (m Model) fetchFile(name string) tea.Cmd {
	return func() tea.Msg {
		content, err := m.backend.ReadFile(name)
		if err != nil {
			return errMsg{err}
		}
		return fileContentMsg{name: name, content: content}
	}
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
