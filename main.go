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

const (
	defaultOpenAIModel = "gpt-4.1-nano"
	defaultClaudeModel = "claude-3-haiku-20240307"
)

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
	llm       Llm
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Type markdown and press enter..."
	ti.Focus()
	ti.Width = 80

	// Default to OpenAI
	var llm Llm
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		llm = NewClaude()
	} else {
		llm = NewOpenAI()
	}

	return model{
		textInput: ti,
		entries:   []entry{},
		ready:     false,
		llm:       llm,
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

			// Update conversation history
			UpdateConversationHistoryText(input, "user")

			// Update viewport content
			m.updateViewportContent()

			// Send input to LLM
			return m, func() tea.Msg {
				// Get model from environment variable or use default
				model := getModelForProvider(m.llm)

				// Get conversation history and convert to interfaces
				history := GetConversationHistory()
				messages := ConvertToInterfaces(history)

				// Process the initial request and any tool calls
				var finalResponse string
				for {
					// Get response from LLM
					inferenceResponse, err := m.llm.Inference(model, messages)
					if err != nil {
						return entry{
							raw:      fmt.Sprintf("Error: %v", err),
							rendered: fmt.Sprintf("Error: %v", err),
							role:     "assistant",
						}
					}

					// Check if we have tool calls
					if len(inferenceResponse.ToolCalls) == 0 {
						// No tool calls, use this as our final response
						finalResponse = inferenceResponse.Content
						UpdateConversationHistoryText(finalResponse, "assistant")
						break
					}

					// Process tool calls
					_, toolResults, err := HandleToolCallsWithResults(inferenceResponse.ToolCalls)
					if err != nil {
						return entry{
							raw:      fmt.Sprintf("Error handling tool calls: %v", err),
							rendered: fmt.Sprintf("Error handling tool calls: %v", err),
							role:     "assistant",
						}
					}

					// Add tool results to conversation history
					for _, result := range toolResults {
						// Add tool result to conversation history
						AddToolResultToHistory(result.CallID, result.Output)
					}

					// Refresh the messages from conversation history
					history = GetConversationHistory()
					messages = ConvertToInterfaces(history)
				}

				renderedResponse, err := renderer.Render(finalResponse)
				if err != nil {
					renderedResponse = "\n" + finalResponse
				}

				return entry{
					raw:      finalResponse,
					rendered: renderedResponse,
					role:     "assistant",
				}
			}
		}
	case entry:
		// Handle the response from LLM
		m.entries = append(m.entries, msg)

		// Update conversation history
		UpdateConversationHistoryText(msg.raw, msg.role)
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
func runSimpleMode(prompt string, llm Llm) {
	// Update conversation history with the user prompt
	UpdateConversationHistoryText(prompt, "user")

	// Get model from environment variable or use default based on provider
	model := getModelForProvider(llm)

	// Convert conversation history to interfaces
	history := GetConversationHistory()
	messages := ConvertToInterfaces(history)

	// Process the initial request and any tool calls
	for {
		// Get response from LLM
		inferenceResponse, err := llm.Inference(model, messages)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Check if we have tool calls
		if len(inferenceResponse.ToolCalls) == 0 {
			// No tool calls, print the response and exit
			fmt.Println(inferenceResponse.Content)
			UpdateConversationHistoryText(inferenceResponse.Content, "assistant")
			break
		}

		// Process tool calls
		_, toolResults, err := HandleToolCallsWithResults(inferenceResponse.ToolCalls)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error handling tool calls: %v\n", err)
			break
		}

		// Add tool results to conversation history
		for _, result := range toolResults {
			// Add tool result to conversation history
			AddToolResultToHistory(result.CallID, result.Output)
		}

		// Refresh the messages from conversation history
		history = GetConversationHistory()
		messages = ConvertToInterfaces(history)
	}
}

// runInteractiveMode reads user input in a loop until Ctrl+C/D
func runInteractiveMode(llm Llm) {
	// Get model from environment variable or use default based on provider
	model := getModelForProvider(llm)

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

		// Send to LLM and get response
		UpdateConversationHistoryText(input, "user")

		// Convert conversation history to interfaces
		history := GetConversationHistory()
		messages := ConvertToInterfaces(history)

		// Process the initial request and any tool calls
		for {
			// Get response from LLM
			inferenceResponse, err := llm.Inference(model, messages)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				break
			}

			// Check if we have tool calls
			if len(inferenceResponse.ToolCalls) == 0 {
				// No tool calls, print the response and continue the outer loop
				fmt.Println(inferenceResponse.Content)
				UpdateConversationHistoryText(inferenceResponse.Content, "assistant")
				break
			}

			// Process tool calls
			_, toolResults, err := HandleToolCallsWithResults(inferenceResponse.ToolCalls)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error handling tool calls: %v\n", err)
				break
			}

			// Add tool results to conversation history
			for _, result := range toolResults {
				// Add tool result to conversation history
				AddToolResultToHistory(result.CallID, result.Output)
			}

			// Refresh the messages from conversation history
			history = GetConversationHistory()
			messages = ConvertToInterfaces(history)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		os.Exit(1)
	}
}

// getModelForProvider returns the appropriate model name based on the LLM provider
func getModelForProvider(llm Llm) string {
	switch llm.(type) {
	case *Claude:
		model := os.Getenv("ANTHROPIC_MODEL")
		if model == "" {
			model = defaultClaudeModel
		}
		return model
	case *OpenAI:
		model := os.Getenv("OPENAI_MODEL")
		if model == "" {
			model = defaultOpenAIModel
		}
		return model
	default:
		return defaultOpenAIModel
	}
}

// initLLM initializes the appropriate LLM provider based on available API keys
func initLLM() (Llm, error) {
	var llm Llm

	// Choose provider based on available API keys
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		llm = NewClaude()
	} else {
		llm = NewOpenAI()
	}

	// Initialize the provider
	if err := llm.Init(); err != nil {
		return nil, err
	}

	return llm, nil
}

func main() {
	// Parse command line flags
	quietFlag := flag.Bool("q", false, "Run in simple mode with a single prompt")
	flag.Parse()

	// Initialize context and load system prompts
	if err := InitContext(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to initialize context: %v\n", err)
	}

	// Initialize LLM provider
	llm, err := initLLM()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to initialize LLM provider: %v\n", err)
		os.Exit(1)
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
		runSimpleMode(prompt, llm)
		return
	} else {
		runInteractiveMode(llm)
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
