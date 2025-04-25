package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Custom message types for updating results asynchronously
type updateResultMsg struct {
	outputs []string
	err     error
}

// Message for tool execution status updates
type toolExecutingMsg struct {
	toolName string
	params   string
}

// Bubbletea model for interactive mode
type chatModel struct {
	textarea     textarea.Model
	viewport     viewport.Model
	llm          Llm
	config       Config
	outputs      []string
	windowHeight int
	err          error
	processing   bool
}

func initialChatModel(llm Llm, config Config) chatModel {
	ta := textarea.New()
	ta.Placeholder = "Ask anything..."
	ta.Focus()
	ta.Prompt = "┃ "
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	ta.SetHeight(4)

	outputs := llm.GetFormattedHistory()

	// Initialize viewport
	vp := viewport.New(80, 20)
	vp.Style = lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder())

	// Create model
	model := chatModel{
		textarea:     ta,
		viewport:     vp,
		llm:          llm,
		config:       config,
		outputs:      outputs,
		windowHeight: 0,
		processing:   false,
	}

	// Set initial viewport content
	initialContent := ""
	for i, output := range outputs {
		initialContent += output
		// Add blank line between messages
		if i < len(outputs)-1 {
			initialContent += "\n\n"
		}
	}
	vp.SetContent(initialContent)
	vp.GotoBottom()

	return model
}

func (m chatModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case toolExecutingMsg:
		m.outputs = append(m.outputs, fmt.Sprintf("%s(%s)", msg.toolName, msg.params))
		m.updateViewportContent()
		return m, nil
	case updateResultMsg:
		// Handle the update from our async processing
		m.outputs = msg.outputs
		m.err = msg.err
		m.processing = false
		m.updateViewportContent()

		// Scroll viewport to the bottom to show latest content
		m.viewport.GotoBottom()
		return m, nil
	case tea.KeyMsg:
		switch {
		case msg.Type == tea.KeyEnter && msg.Alt:
			// Insert newline on Alt+Enter
			m.textarea.InsertString("\n")
			return m, nil
		case msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyCtrlD:
			return m, tea.Quit
		case msg.Type == tea.KeyEnter:
			// If we're already processing, ignore the input
			if m.processing {
				return m, nil
			}

			input := m.textarea.Value()
			if input != "" {
				trimmedInput := strings.TrimSpace(input)
				if trimmedInput == "/clear" {
					triggerMsg := "Command /clear triggered"
					m.outputs = append(m.outputs, triggerMsg)
					m.textarea.Reset()
					m.updateViewportContent()
					return m, nil
				} else if trimmedInput == "/cost" {
					var price float64
					var inputDisplay, outputDisplay string
					switch provider := m.llm.(type) {
					case *Claude:
						price = provider.CalculatePrice()
						inputDisplay = formatTokenCount(provider.InputTokens)
						outputDisplay = formatTokenCount(provider.OutputTokens)
					case *OpenAI:
						price = provider.CalculatePrice()
						inputDisplay = formatTokenCount(provider.InputTokens)
						outputDisplay = formatTokenCount(provider.OutputTokens)
					}
					msg := fmt.Sprintf("Tokens: %s input, %s output. Cost: $%.2f", inputDisplay, outputDisplay, price)
					m.outputs = append(m.outputs, msg)
					m.textarea.Reset()
					m.updateViewportContent()
					m.viewport.GotoBottom()
					return m, nil
				} else if trimmedInput == "/init" {
					input = initPrompt
				}

				// Mark as processing
				m.processing = true
				m.textarea.Reset()

				// Add a processing message to the display
				m.outputs = append(m.outputs, "> "+input)
				m.outputs = append(m.outputs, "Thinking...")
				m.updateViewportContent()
				m.viewport.GotoBottom()

				// Store a copy of the model for the goroutine to use
				llm := m.llm
				config := m.config

				// Get the prompt to process
				prompt := input

				// Use a goroutine to process the request asynchronously
				go func() {
					var finalErr error

					for {
						// Get response from LLM
						inferenceResponse, err := llm.Inference(prompt)
						if err != nil {
							finalErr = err
							break
						}

						// Clear prompt for next iteration
						prompt = ""

						// Check if we have tool calls
						if len(inferenceResponse.ToolCalls) == 0 {
							break
						}

						// Process tool calls
						_, toolResults, err := HandleToolCallsWithResults(inferenceResponse.ToolCalls, config)
						if err != nil {
							finalErr = err
							break
						}

						// Add tool results to LLM conversation history
						for _, result := range toolResults {
							llm.AddToolResult(result.CallID, result.Output)
						}
					}

					// Once processing is complete, update the UI via the global program reference
					if programRef != nil {
						programRef.Send(updateResultMsg{
							outputs: llm.GetFormattedHistory(),
							err:     finalErr,
						})
					}
				}()

				return m, nil
			}
		// Handle viewport scrolling
		case msg.String() == "up" || msg.String() == "k":
			m.viewport, cmd = m.viewport.Update(msg)
			cmds = append(cmds, cmd)
		case msg.String() == "down" || msg.String() == "j":
			m.viewport, cmd = m.viewport.Update(msg)
			cmds = append(cmds, cmd)
		case msg.String() == "pgup":
			m.viewport, cmd = m.viewport.Update(msg)
			cmds = append(cmds, cmd)
		case msg.String() == "pgdown":
			m.viewport, cmd = m.viewport.Update(msg)
			cmds = append(cmds, cmd)
		case msg.Type == tea.KeyHome:
			m.viewport.GotoTop()
		case msg.Type == tea.KeyEnd:
			m.viewport.GotoBottom()
		}
	case tea.WindowSizeMsg:
		// Calculate height for the viewport based on window size
		headerHeight := 1 // Title
		footerHeight := 6 // Textarea (4) + status (1) + padding (1)

		viewportHeight := msg.Height - headerHeight - footerHeight
		if viewportHeight < 1 {
			viewportHeight = 1
		}

		m.viewport.Width = msg.Width - 4
		m.viewport.Height = viewportHeight

		// Update textarea width
		m.textarea.SetWidth(msg.Width - 4)

		m.windowHeight = msg.Height

		// Update content after resize
		m.updateViewportContent()
	}

	// Update both components
	m.textarea, cmd = m.textarea.Update(msg)
	cmds = append(cmds, cmd)

	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// Update the viewport content based on current outputs
func (m *chatModel) updateViewportContent() {
	content := ""

	// Concatenate all outputs with a blank line between them
	for i, output := range m.outputs {
		// Wrap long lines to fit viewport width
		wrappedOutput := wrapText(output, m.viewport.Width)
		content += wrappedOutput
		// Add blank line between messages
		if i < len(m.outputs)-1 {
			content += "\n\n"
		}
	}

	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
}

// wrapText wraps long lines to fit within the specified width
func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	var result strings.Builder
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		if len(line) <= width {
			result.WriteString(line)
		} else {
			// Process the line in chunks of width characters
			for len(line) > 0 {
				if len(line) <= width {
					result.WriteString(line)
					line = ""
				} else {
					// Find the last space before width
					lastSpace := strings.LastIndex(line[:width], " ")
					if lastSpace == -1 || lastSpace == 0 {
						// No space found or space at beginning, just cut at width
						result.WriteString(line[:width])
						line = line[width:]
					} else {
						// Cut at the last space
						result.WriteString(line[:lastSpace])
						line = line[lastSpace+1:] // Skip the space
					}
					result.WriteString("\n")
				}
			}
		}

		// Add newline between original lines (but not after the last line)
		if i < len(lines)-1 {
			result.WriteString("\n")
		}
	}

	return result.String()
}

func (m chatModel) View() string {
	// Title style
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("170")).
		Bold(true).
		PaddingLeft(2)

	// Status line style
	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Italic(true)

	// Error style
	errorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("9")).
		Bold(true)

	// Create title with scroll information
	title := titleStyle.Render("Chat History")

	// Render viewport content
	contentView := m.viewport.View()

	// Render textarea input
	inputView := m.textarea.View()

	// Render status line
	statusLine := ""
	if m.viewport.TotalLineCount() > 0 {
		statusLine = statusStyle.Render(fmt.Sprintf("Lines: %d/%d",
			m.viewport.VisibleLineCount(),
			m.viewport.TotalLineCount()))
	}

	// Add error message if needed
	if m.err != nil {
		statusLine += " " + errorStyle.Render(fmt.Sprintf("Error: %v", m.err))
	}

	// Add processing indicator if needed
	if m.processing {
		statusLine += " " + statusStyle.Render("[Processing...]")
	}

	// Combine all elements
	return fmt.Sprintf("%s\n%s\n\n%s\n%s",
		title,
		contentView,
		inputView,
		statusLine)
}

// Global reference to the running program, used for async updates
var programRef *tea.Program

// runInteractiveMode initializes and runs the terminal UI
func runInteractiveMode(llm Llm, config Config) {
	p := tea.NewProgram(initialChatModel(llm, config), tea.WithAltScreen())
	programRef = p
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
