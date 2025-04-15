package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// Define styles
var (
	// Colors
	userInputColor      = lipgloss.Color("#2a9d8f")
	assistantColor      = lipgloss.Color("#264653")
	errorColor          = lipgloss.Color("#e76f51")
	warningColor        = lipgloss.Color("#f4a261")
	toolOutputColor     = lipgloss.Color("#e9c46a")
	highlightColor      = lipgloss.Color("#0096cf")
	completionMenuColor = lipgloss.Color("#8ecae6")

	// Styles
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(highlightColor).
			MarginLeft(2)

	userInputStyle = lipgloss.NewStyle().
			Foreground(userInputColor)

	assistantOutputStyle = lipgloss.NewStyle().
				Foreground(assistantColor)

	errorStyle = lipgloss.NewStyle().
			Foreground(errorColor)

	warningStyle = lipgloss.NewStyle().
			Foreground(warningColor)

	toolOutputStyle = lipgloss.NewStyle().
			Foreground(toolOutputColor)

	fileListStyle = lipgloss.NewStyle().
			MarginLeft(2).
			MarginBottom(1)

	promptStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(userInputColor)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262")).
			MarginLeft(2)
)

// Define keymap
type keyMap struct {
	Enter     key.Binding
	AltEnter  key.Binding
	CtrlX     key.Binding
	CtrlE     key.Binding
	CtrlZ     key.Binding
	Up        key.Binding
	Down      key.Binding
	ToggleHelp key.Binding
	Quit      key.Binding
}

// ShortHelp returns keybindings to be shown in the mini help view.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.ToggleHelp, k.Quit}
}

// FullHelp returns keybindings for the expanded help view.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Enter, k.AltEnter, k.CtrlX, k.CtrlE},
		{k.CtrlZ, k.Up, k.Down, k.Quit},
	}
}

var keys = keyMap{
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "submit"),
	),
	AltEnter: key.NewBinding(
		key.WithKeys("alt+enter"),
		key.WithHelp("alt+enter", "insert newline/submit"),
	),
	CtrlX: key.NewBinding(
		key.WithKeys("ctrl+x"),
		key.WithHelp("ctrl+x ctrl+e", "edit in external editor"),
	),
	CtrlE: key.NewBinding(
		key.WithKeys("ctrl+e"),
		key.WithHelp("", ""),
	),
	CtrlZ: key.NewBinding(
		key.WithKeys("ctrl+z"),
		key.WithHelp("ctrl+z", "suspend"),
	),
	Up: key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
		key.WithHelp("↑", "previous history item"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "ctrl+n"),
		key.WithHelp("↓", "next history item"),
	),
	ToggleHelp: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "toggle help"),
	),
	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
}

// Message types
type errorMsg struct{ err error }
type historyMsg struct{ content string }
type assistantResponseMsg struct{ content string }

func (e errorMsg) Error() string { return e.err.Error() }

// Model represents the application state
type Model struct {
	textInput  textinput.Model
	viewport   viewport.Model
	history    []string
	markdown   *glamour.TermRenderer
	help       help.Model
	multiline  bool
	showHelp   bool
	width      int
	height     int
	fileList   []string
	historyIdx int
	content    string
	ready      bool
	err        error
}

func initialModel() Model {
	ti := textinput.New()
	ti.Placeholder = "Ask or type a command..."
	ti.Focus()
	ti.CharLimit = 0
	ti.Width = 80

	h := help.New()
	h.Width = 80

	return Model{
		textInput:  ti,
		viewport:   viewport.Model{},
		history:    []string{},
		help:       h,
		multiline:  false,
		showHelp:   false,
		fileList:   []string{"main.go", "go.mod", "go.sum"},
		historyIdx: -1,
		content:    "",
		ready:      false,
	}
}

func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, keys.ToggleHelp):
			m.showHelp = !m.showHelp
			return m, nil

		case key.Matches(msg, keys.Enter):
			if !m.multiline {
				content := m.textInput.Value()
				if content == "/multi" {
					m.multiline = true
					m.textInput.SetValue("")
					return m, nil
				}
				return m, func() tea.Msg {
					return historyMsg{content: content}
				}
			} else {
				m.textInput.SetValue(m.textInput.Value() + "\n")
				return m, nil
			}

		case key.Matches(msg, keys.AltEnter):
			if m.multiline {
				content := m.textInput.Value()
				return m, func() tea.Msg {
					return historyMsg{content: content}
				}
			} else {
				m.textInput.SetValue(m.textInput.Value() + "\n")
				return m, nil
			}

		case key.Matches(msg, keys.Up):
			if m.historyIdx < len(m.history)-1 {
				m.historyIdx++
				m.textInput.SetValue(m.history[len(m.history)-1-m.historyIdx])
			}
			return m, nil

		case key.Matches(msg, keys.Down):
			if m.historyIdx > 0 {
				m.historyIdx--
				m.textInput.SetValue(m.history[len(m.history)-1-m.historyIdx])
			} else if m.historyIdx == 0 {
				m.historyIdx = -1
				m.textInput.SetValue("")
			}
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		if !m.ready {
			var err error
			m.markdown, err = glamour.NewTermRenderer(
				glamour.WithAutoStyle(),
				glamour.WithWordWrap(msg.Width-4),
			)
			if err != nil {
				return m, tea.Quit
			}

			headerHeight := 6 // Adjust based on your header
			footerHeight := 3 // Adjust based on your footer
			verticalMarginHeight := headerHeight + footerHeight

			m.viewport = viewport.New(msg.Width, msg.Height-verticalMarginHeight)
			m.viewport.SetContent(m.content)
			m.viewport.YPosition = headerHeight

			m.help.Width = msg.Width

			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - 6 - 3 // header + footer
			m.help.Width = msg.Width
		}

	case historyMsg:
		// Add to history
		if msg.content != "" {
			m.history = append(m.history, msg.content)
			m.historyIdx = -1
		}

		// Format user input
		userInput := userInputStyle.Render(msg.content)
		m.viewport.SetContent(m.viewport.View() + "\n\n" + userInput)
		m.viewport.GotoBottom()
		m.textInput.SetValue("")

		// Simulate AI processing
		return m, func() tea.Msg {
			// Mocked AI response
			time.Sleep(1 * time.Second)
			response := "This is a simulated AI assistant response in markdown format.\n\n"
			response += "```go\nfunc Example() string {\n    return \"This is a code example\"\n}\n```\n\n"
			response += "- It can handle **markdown** formatting\n- Including lists\n- And *styled* text"
			return assistantResponseMsg{content: response}
		}

	case assistantResponseMsg:
		// Format AI response with markdown rendering
		rendered, err := m.markdown.Render(msg.content)
		if err != nil {
			return m, func() tea.Msg { return errorMsg{err} }
		}
		m.viewport.SetContent(m.viewport.View() + "\n\n" + rendered)
		m.viewport.GotoBottom()

	case errorMsg:
		m.err = msg.err
		return m, nil
	}

	m.textInput, cmd = m.textInput.Update(msg)
	cmds = append(cmds, cmd)

	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	// Header with file list
	header := titleStyle.Render("Aider TUI")
	if len(m.fileList) > 0 {
		fileList := "Files: " + strings.Join(m.fileList, ", ")
		header += "\n" + fileListStyle.Render(fileList)
	}
	header += "\n\n"

	// Main viewport with chat history
	main := m.viewport.View()

	// Input prompt and textinput
	var prompt string
	if m.multiline {
		prompt = promptStyle.Render("multi> ")
	} else {
		prompt = promptStyle.Render("> ")
	}
	input := prompt + m.textInput.View()

	// Error display
	var errorView string
	if m.err != nil {
		errorView = "\n" + errorStyle.Render(m.err.Error())
	}

	// Help
	var helpView string
	if m.showHelp {
		helpView = "\n" + helpStyle.Render(m.help.View(keys))
	}

	// Combine everything
	return fmt.Sprintf("%s%s\n\n%s%s%s", header, main, input, errorView, helpView)
}

func main() {
	p := tea.NewProgram(
		initialModel(),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running program: %v\n", err)
		os.Exit(1)
	}
}