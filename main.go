package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type model struct {
	textInput textinput.Model
	inputs    []string
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Type something and press enter..."
	ti.Focus()
	ti.Width = 40

	return model{
		textInput: ti,
		inputs:    []string{},
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
			// Get the current input value
			input := m.textInput.Value()

			// Check if the input is a command
			isCommand, cmdResult := handleCommand(input, &m)
			if isCommand {
				// If it was a command, we've already handled it
				m.textInput.Reset()
				return m, cmdResult
			}

			// Add it to our list of inputs
			m.inputs = append(m.inputs, input)

			// Clear the input field for next entry
			m.textInput.Reset()

			return m, nil
		}
	}

	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// handleCommand processes command inputs starting with '/'
// Returns true if the input was a command, false otherwise
// Also returns a tea.Cmd if needed for execution
func handleCommand(input string, m *model) (bool, tea.Cmd) {
	// Check if the input starts with a '/' to identify commands
	if len(input) > 0 && input[0] == '/' {
		cmd := input[1:] // Remove the '/' prefix

		switch cmd {
		case "exit":
			// Exit the application
			return true, tea.Quit
		
		case "clear":
			// Clear all previous inputs
			m.inputs = []string{}
			return true, nil
		
		case "help":
			// Add a help message to the inputs
			m.inputs = append(m.inputs, "Available commands: /exit, /clear, /help")
			return true, nil
		
		default:
			// Unknown command
			m.inputs = append(m.inputs, fmt.Sprintf("Unknown command: %s", cmd))
			return true, nil
		}
	}
	return false, nil
}

func (m model) View() string {
	var s string

	// Show previous inputs
	if len(m.inputs) > 0 {
		s += "Previous inputs:\n"
		for i, input := range m.inputs {
			s += fmt.Sprintf("%d. %s\n", i+1, input)
		}
		s += "\n"
	}

	// Show current input field
	s += fmt.Sprintf(
		"%s\n\n(Press ctrl+c or ctrl+d to quit)\n",
		m.textInput.View(),
	)

	// Show command help
	s += "\nCommands: /exit, /clear, /help\n"

	return s
}

func main() {
	p := tea.NewProgram(initialModel())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running program: %v\n", err)
		os.Exit(1)
	}
}
