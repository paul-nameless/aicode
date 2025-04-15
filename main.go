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

	return s
}

func main() {
	p := tea.NewProgram(initialModel())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running program: %v\n", err)
		os.Exit(1)
	}
}
