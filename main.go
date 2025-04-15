package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
)

type entry struct {
	raw      string
	rendered string
}

type model struct {
	textInput textinput.Model
	viewport  viewport.Model
	entries   []entry
	ready     bool
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Type markdown and press enter..."
	ti.Focus()
	ti.Width = 80

	return model{
		textInput: ti,
		entries:   []entry{},
		ready:     false,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, tea.EnterAltScreen)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if !m.ready {
			// Initialize the viewport now that we know the terminal dimensions
			headerHeight := 0
			footerHeight := 3 // Input field + divider
			verticalMarginHeight := headerHeight + footerHeight
			
			m.viewport = viewport.New(msg.Width, msg.Height-verticalMarginHeight)
			m.viewport.SetContent("")
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - 3
		}
		
		m.textInput.Width = msg.Width - 2
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyCtrlD:
			return m, tea.Quit

		case tea.KeyEnter:
			input := m.textInput.Value()
			if input == "" {
				return m, nil
			}

			// Clear input immediately
			m.textInput.Reset()

			// Check if it's a special command
			if len(input) > 0 && input[0] == '/' {
				isCommand, cmdResult := handleCommand(input, &m)
				if isCommand {
					return m, cmdResult
				}
			}

			// Send input to OpenAI API
			return m, func() tea.Msg {
				// Display user input first
				renderer, _ := glamour.NewTermRenderer(
					glamour.WithStandardStyle("dark"),
					glamour.WithWordWrap(80),
				)

				rendered, err := renderer.Render("> " + input)
				if err != nil {
					rendered = "> " + input
				}

				m.entries = append(m.entries, entry{
					raw:      input,
					rendered: rendered,
				})
				
				// Update viewport content
				m.updateViewportContent()

				// Send to OpenAI and get response
				response, err := AskOpenAI("gpt-4.1-nano", input)
				if err != nil {
					return entry{
						raw:      fmt.Sprintf("Error: %v", err),
						rendered: fmt.Sprintf("Error: %v", err),
					}
				}

				renderedResponse, err := renderer.Render(response)
				if err != nil {
					renderedResponse = "\n" + response
				}

				return entry{
					raw:      response,
					rendered: renderedResponse,
				}
			}
		}
	case entry:
		// Handle the response from AskOpenAI
		m.entries = append(m.entries, msg)
		m.updateViewportContent()
		return m, nil
	}

	// Handle viewport updates
	if m.ready {
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	// Handle text input updates
	m.textInput, cmd = m.textInput.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func handleCommand(input string, m *model) (bool, tea.Cmd) {
	if len(input) > 0 && input[0] == '/' {
		cmd := input[1:]

		// Handle commands that don't have parameters
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
			m.updateViewportContent()
			return true, nil
		}

		// Default case for unknown commands
		helpText := fmt.Sprintf("Unknown command: %s", cmd)
		m.entries = append(m.entries, entry{
			raw:      helpText,
			rendered: helpText,
		})
		m.updateViewportContent()
		return true, nil
	}
	return false, nil
}

// updateViewportContent updates the viewport's content with all entries
func (m *model) updateViewportContent() {
	var s strings.Builder
	for _, entry := range m.entries {
		s.WriteString(entry.rendered)
	}
	m.viewport.SetContent(s.String())
	m.viewport.GotoBottom()
}

func (m model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	var s strings.Builder

	// Display chat history in the viewport
	s.WriteString(m.viewport.View())
	
	// Display input field at the bottom with a divider
	s.WriteString("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	s.WriteString(fmt.Sprintf("%s\n", m.textInput.View()))

	return s.String()
}

func main() {
	p := tea.NewProgram(
		initialModel(),
		tea.WithAltScreen(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running program: %v\n", err)
		os.Exit(1)
	}
}
