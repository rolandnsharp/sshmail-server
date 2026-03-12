package tui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var validName = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,19}$`)

// RegisterModel is the registration screen for new users.
type RegisterModel struct {
	input    textinput.Model
	err      string
	width    int
	height   int
	done     bool
	Name     string
	Quit     bool
}

// RegisterResult is sent when registration completes.
type RegisterResult struct {
	Name string
	Quit bool
}

func NewRegisterModel() RegisterModel {
	ti := textinput.New()
	ti.Placeholder = "pick a username"
	ti.Focus()
	ti.CharLimit = 20
	ti.Width = 24
	return RegisterModel{input: ti}
}

func (m RegisterModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m RegisterModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			m.Quit = true
			return m, tea.Quit
		case "enter":
			name := strings.TrimSpace(m.input.Value())
			if name == "" {
				return m, nil
			}
			if !validName.MatchString(name) {
				m.err = "lowercase letters, numbers, _ and - only (2-20 chars)"
				return m, nil
			}
			m.err = ""
			m.Name = name
			m.done = true
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.err = ""
	return m, cmd
}

func (m RegisterModel) View() string {
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B50FF")).
		Bold(true)

	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#858392"))

	errStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#EB4268"))

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3A3943")).
		Padding(1, 3).
		Width(40)

	var content strings.Builder
	content.WriteString(titleStyle.Render("ssh sshmail.dev"))
	content.WriteString("\n\n")
	content.WriteString(subtitleStyle.Render("welcome! claim your username:"))
	content.WriteString("\n\n")
	content.WriteString(m.input.View())
	if m.err != "" {
		content.WriteString("\n")
		content.WriteString(errStyle.Render(m.err))
	}
	content.WriteString("\n\n")
	content.WriteString(subtitleStyle.Render("enter to confirm · esc to quit"))

	box := boxStyle.Render(content.String())

	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box,
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#1F1C23")),
	)
}

// SetError sets an external error message (e.g. "name taken").
func (m *RegisterModel) SetError(msg string) {
	m.err = msg
}

// ValidateName checks if a name is syntactically valid.
func ValidateName(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("invalid name: lowercase letters, numbers, _ and - only (2-20 chars, starts with letter)")
	}
	return nil
}
