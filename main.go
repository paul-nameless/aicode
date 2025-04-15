package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
)

type entry struct {
	raw      string
	rendered string
}

type model struct {
	textInput textinput.Model
	entries   []entry
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Type markdown and press enter..."
	ti.Focus()
	ti.Width = 80

	return model{
		textInput: ti,
		entries:   []entry{},
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyCtrlD:
			return m, tea.Quit

		case tea.KeyEnter:
			input := m.textInput.Value()
			if input == "" {
				return m, nil
			}

			isCommand, cmdResult := handleCommand(input, &m)
			if isCommand {
				m.textInput.Reset()
				return m, cmdResult
			}

			renderer, _ := glamour.NewTermRenderer(
				glamour.WithStandardStyle("dark"),
				glamour.WithWordWrap(80),
			)

			rendered, err := renderer.Render(input)
			if err != nil {
				rendered = "Error rendering markdown\n"
			}

			m.entries = append(m.entries, entry{
				raw:      input,
				rendered: rendered,
			})

			m.textInput.Reset()
			return m, nil
		}
	}

	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func handleCommand(input string, m *model) (bool, tea.Cmd) {
	if len(input) > 0 && input[0] == '/' {
		cmd := input[1:]

		switch cmd {
		case "exit":
			return true, tea.Quit

		case "clear":
			m.entries = []entry{}
			return true, nil

		case "help":
			renderer, _ := glamour.NewTermRenderer(
				glamour.WithStandardStyle("dark"),
				glamour.WithWordWrap(80),
			)
			helpText := "# Available Commands\n- `/exit` - Quit the application\n- `/clear` - Clear all entries\n- `/help` - Show this help"
			rendered, _ := renderer.Render(helpText)
			
			m.entries = append(m.entries, entry{
				raw:      helpText,
				rendered: rendered,
			})
			return true, nil

		default:
			helpText := fmt.Sprintf("Unknown command: %s", cmd)
			m.entries = append(m.entries, entry{
				raw:      helpText,
				rendered: helpText,
			})
			return true, nil
		}
	}
	return false, nil
}

func (m model) View() string {
	var s strings.Builder

	// Display history of entries
	for i, entry := range m.entries {
		s.WriteString(fmt.Sprintf("--- Entry %d ---\n", i+1))
		s.WriteString(fmt.Sprintf("Input: %s\n\n", entry.raw))
		s.WriteString("Rendered:\n")
		s.WriteString(entry.rendered)
		s.WriteString("\n")
	}

	// Display input field at the bottom
	s.WriteString("\n----------------------------------------\n")
	s.WriteString("(Type markdown and press Enter, Ctrl+C to quit)\n")
	s.WriteString(fmt.Sprintf("%s\n", m.textInput.View()))

	return s.String()
}

func main() {
	p := tea.NewProgram(initialModel())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running program: %v\n", err)
		os.Exit(1)
	}
}