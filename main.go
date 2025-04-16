package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
)

const defaultModel = "gpt-4.1-nano"

type entry struct {
	raw      string
	rendered string
	role     string // "user" or "assistant"
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

			// Display user input first
			renderer, _ := glamour.NewTermRenderer(
				glamour.WithStandardStyle("dark"),
				glamour.WithWordWrap(80),
			)

			rendered, err := renderer.Render("> " + input)
			if err != nil {
				rendered = "> " + input
			}

			// Add entry to UI
			m.entries = append(m.entries, entry{
				raw:      input,
				rendered: rendered,
				role:     "user",
			})

			// Update conversation history in openai.go
			UpdateConversationHistory(input, "user")

			// Update viewport content
			m.updateViewportContent()

			// Send input to OpenAI API
			return m, func() tea.Msg {
				// Get model from environment variable or use default
				model := os.Getenv("OPENAI_MODEL")
				if model == "" {
					model = defaultModel
				}

				// Send to OpenAI and get response
				messages := getConversationHistory()
				response, err := AskLlm(model, messages)
				if err != nil {
					return entry{
						raw:      fmt.Sprintf("Error: %v", err),
						rendered: fmt.Sprintf("Error: %v", err),
						role:     "assistant",
					}
				}

				renderedResponse, err := renderer.Render(response)
				if err != nil {
					renderedResponse = "\n" + response
				}

				return entry{
					raw:      response,
					rendered: renderedResponse,
					role:     "assistant",
				}
			}
		}
	case entry:
		// Handle the response from AskLlm
		m.entries = append(m.entries, msg)

		// Update conversation history in openai.go
		UpdateConversationHistory(msg.raw, msg.role)
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
				role:     "assistant", // Help text is from the assistant
			})
			m.updateViewportContent()
			return true, nil
		}

		// Default case for unknown commands
		helpText := fmt.Sprintf("Unknown command: %s", cmd)
		m.entries = append(m.entries, entry{
			raw:      helpText,
			rendered: helpText,
			role:     "assistant", // Error messages are from the assistant
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

// runSimpleMode processes a single prompt in non-interactive mode
func runSimpleMode(prompt string) {
	// Update conversation history with the user prompt

	// Get model from environment variable or use default
	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = defaultModel
	}

	// Send to OpenAI and get response
	UpdateConversationHistory(prompt, "user")

	messages := getConversationHistory()

	response, err := AskLlm(model, messages)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Print the response
	fmt.Println(response)
}

// runNonInteractiveMode reads user input in a loop until Ctrl+C/D
func runInteractiveMode() {
	// Get model from environment variable or use default
	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = defaultModel

	}
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			// EOF (Ctrl+D) detected
			break
		}

		input := scanner.Text()
		if input == "" {
			continue
		}

		// Send to OpenAI and get response
		UpdateConversationHistory(input, "user")
		messages := getConversationHistory()

		response, err := AskLlm(model, messages)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			continue
		}

		// Print the response
		fmt.Println(response)
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		os.Exit(1)
	}
}

func main() {
	// Parse command line flags
	quietFlag := flag.Bool("q", false, "Run in simple mode with a single prompt")
	flag.Parse()

	// Load context at startup
	if err := LoadContext(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to load context: %v\n", err)
	}

	// Check if quiet flag is set
	if *quietFlag {
		// Get the prompt from the remaining arguments
		args := flag.Args()
		if len(args) == 0 {
			fmt.Fprintf(os.Stderr, "Error: No prompt provided in simple mode\n")
			fmt.Fprintf(os.Stderr, "Usage: %s -q \"your prompt here\"\n", os.Args[0])
			os.Exit(1)
		}

		// Join all arguments as the prompt
		prompt := strings.Join(args, " ")
		runSimpleMode(prompt)
		return
	} else {
		runInteractiveMode()
		os.Exit(1)
	}

	// Run the fancy TUI mode
	p := tea.NewProgram(
		initialModel(),
		tea.WithAltScreen(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running program: %v\n", err)
		os.Exit(1)
	}
}
