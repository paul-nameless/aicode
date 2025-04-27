package main

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
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

// Message for cancellation notification
type cancelOperationMsg struct{}

// Message indicating processing is done
type processingDoneMsg struct{}

// registerCmdCommands reads the ~/.config/aicode/cmds directory and registers commands
func registerCmdCommands(m *chatModel) {
	// Get user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		slog.Error("Failed to get user home directory", "err", err)
		return
	}

	// Path to commands directory
	cmdsDir := filepath.Join(homeDir, ".config/aicode/cmds")

	// Check if directory exists
	if _, err := os.Stat(cmdsDir); os.IsNotExist(err) {
		// Directory doesn't exist yet
		return
	}

	// Walk through all .md files in the directory
	err = filepath.WalkDir(cmdsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Only process .md files
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}

		// Extract base name without extension
		baseName := strings.TrimSuffix(d.Name(), ".md")

		// Register command
		cmdName := "/cmd:" + baseName
		m.commands[cmdName] = SlashCommand{
			Description: "Custom command from " + d.Name(),
			Handler:     nil, // We'll handle these commands separately
		}

		return nil
	})

	if err != nil {
		slog.Error("Failed to read commands directory", "err", err)
	}
}

type SlashCommand struct {
	Description string
	Handler     func(m *chatModel) error
}

// Bubbletea model for interactive mode
type chatModel struct {
	textarea          textarea.Model
	viewport          viewport.Model
	spinner           spinner.Model
	llm               Llm
	config            Config
	outputs           []string
	windowHeight      int
	processing        bool
	lastExitKeypress  tea.KeyType
	lastExitTimestamp int64
	focused           bool
	commands          map[string]SlashCommand
}

func helpHandler(m *chatModel) error {
	helpMsg := "Available commands:\n"
	for cmd, desc := range m.commands {
		helpMsg += fmt.Sprintf("  %s - %s\n", cmd, desc.Description)
	}
	m.outputs = append(m.outputs, helpMsg)
	return nil
}

func clearHandler(m *chatModel) error {
	m.llm.Clear()
	m.outputs = getInitialMsgs(&m.llm)
	return nil
}

func costHandler(m *chatModel) error {
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
	return nil
}

func (m *chatModel) isCmd(input string) (string, bool) {
	if strings.HasPrefix(input, "/") {
		fields := strings.Fields(input)
		if len(fields) < 1 {
			return "", false
		}
		cmdName := fields[0]
		_, exists := m.commands[cmdName]
		return cmdName, exists
	}
	return "", false
}

func getInitialMsgs(llm *Llm) []string {
	return []string{
		fmt.Sprintf("Welcome to %s", lipgloss.NewStyle().Bold(true).Render("AiCode")),
		fmt.Sprintf("Model: %s", (*llm).GetModel()),
	}
}

func initialChatModel(llm Llm, config Config) chatModel {
	ta := textarea.New()
	ta.Placeholder = "Ask anything..."
	ta.Focus()
	ta.Prompt = "┃ "
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	ta.SetHeight(4)

	outputs := getInitialMsgs(&llm)

	// Initialize viewport
	vp := viewport.New(80, 20)
	vp.Style = lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder())
	vp.KeyMap = customViewportKeyMap()

	// Initialize spinner
	sp := spinner.New()
	sp.Spinner = spinner.Points
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)

	// Create model
	model := chatModel{
		textarea:          ta,
		viewport:          vp,
		spinner:           sp,
		llm:               llm,
		config:            config,
		outputs:           outputs,
		windowHeight:      0,
		processing:        false,
		lastExitKeypress:  0,
		lastExitTimestamp: 0,
		focused:           true,
	}

	model.commands = map[string]SlashCommand{
		"/help":  {Description: "Show available commands", Handler: helpHandler},
		"/clear": {Description: "Clear conversation history", Handler: clearHandler},
		"/cost":  {Description: "Display token usage and cost information", Handler: costHandler},
		"/init":  {Description: "Initialize with the system prompt", Handler: nil},
	}

	// Add custom commands from ~/.config/aicode/cmds directory
	registerCmdCommands(&model)

	// Set initial viewport content
	initialContent := ""
	for i, output := range outputs {
		initialContent += output
		// Add blank line between messages
		if i < len(outputs)-1 {
			initialContent += "\n"
		}
	}
	vp.SetContent(initialContent)
	vp.GotoBottom()

	return model
}

func (m chatModel) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.spinner.Tick)
}

func (m chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.FocusMsg:
		m.focused = true
		return m, nil
	case tea.BlurMsg:
		m.focused = false
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case toolExecutingMsg:
		m.outputs = append(m.outputs, fmt.Sprintf("%s(%s)", msg.toolName, msg.params))
		m.updateViewportContent()
		return m, nil
	case cancelOperationMsg:
		m.outputs = append(m.outputs, "Operation canceled")
		m.processing = false
		m.updateViewportContent()
		return m, nil
	case processingDoneMsg:
		m.processing = false
		if !m.focused {
			_, err := executeShellCommand(m.config.NotifyCmd)
			if err != nil {
				slog.Error("Failed to run notify cmd", "err", err)

			}
		}
		return m, nil
	case updateResultMsg:
		// Handle the update from our async processing
		m.outputs = append(m.outputs, msg.outputs...)
		if msg.err != nil {
			errorStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("9")).
				Bold(true)
			error := errorStyle.Render(fmt.Sprintf("Error: %v", msg.err))
			m.outputs = append(m.outputs, error)
		}
		m.updateViewportContent()
		return m, nil
	case tea.KeyMsg:
		switch {
		case msg.Type == tea.KeyEsc && m.processing:
			// Cancel the current operation
			m.outputs = append(m.outputs, "Canceling operation...")
			m.updateViewportContent()

			// Cancel the global context
			GlobalAppContext.Cancel()

			// Instead of immediate reset, mark as no longer processing
			// We'll reset the context after the goroutine exits
			m.processing = false

			return m, nil
		case msg.Type == tea.KeyTab:
			// Get current text
			input := strings.TrimSpace(m.textarea.Value())
			if strings.HasPrefix(input, "/") {
				// Show command suggestions
				prefix := input
				suggestions := []string{}

				// Find commands matching the prefix
				for cmd := range m.commands {
					if strings.HasPrefix(cmd, prefix) {
						suggestions = append(suggestions, cmd)
					}
				}

				// If we have suggestions, show them
				if len(suggestions) > 0 {
					// Sort suggestions alphabetically
					sort.Strings(suggestions)

					// Build suggestion message
					suggestionMsg := strings.Join(suggestions, ", ")
					m.outputs = append(m.outputs, suggestionMsg)
					m.updateViewportContent()

					// If only one suggestion, replace the text
					if len(suggestions) == 1 {
						m.textarea.SetValue(suggestions[0] + " ")
					}
				}
			}
			return m, nil

		case msg.Type == tea.KeyEnter && msg.Alt:
			// Insert newline on Alt+Enter
			m.textarea.InsertString("\n")
			return m, nil
		case msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyCtrlQ:
			now := time.Now().UnixNano()
			// Check if this is the second press of the same key within 2 seconds
			if m.lastExitKeypress == msg.Type && (now-m.lastExitTimestamp) < int64(2*time.Second) {
				return m, tea.Quit
			}
			// Update the last exit keypress and timestamp
			m.lastExitKeypress = msg.Type
			m.lastExitTimestamp = now

			// Notify user about the exit process
			statusMsg := "Press Ctrl+"
			if msg.Type == tea.KeyCtrlC {
				statusMsg += "C"
			} else {
				statusMsg += "Q"
			}
			statusMsg += " again to exit"
			m.outputs = append(m.outputs, statusMsg)
			m.updateViewportContent()
			return m, nil
		case msg.Type == tea.KeyEnter:
			// If we're already processing, ignore the input
			if m.processing {
				return m, nil
			}

			input := strings.TrimSpace(m.textarea.Value())
			if input == "" {
				return m, nil
			}

			if cmdName, exists := m.isCmd(input); exists {
				if strings.HasPrefix(cmdName, "/cmd:") {
					// Handle /cmd: commands directly
					cmdFile := strings.TrimPrefix(cmdName, "/cmd:")
					cmdPath := filepath.Join(os.Getenv("HOME"), ".config/aicode/cmds", cmdFile+".md")
					content, err := os.ReadFile(cmdPath)
					if err != nil {
						m.outputs = append(m.outputs, fmt.Sprintf("Error loading command file: %v", err))
					} else {
						// Extract arguments - everything after the command name
						args := ""
						if len(strings.Fields(input)) > 1 {
							args = strings.TrimPrefix(input, cmdName)
							args = strings.TrimSpace(args)
						}

						// Process the command template with arguments
						processedCmd, err := processCommandTemplate(string(content), args)
						if err != nil {
							m.outputs = append(m.outputs, fmt.Sprintf("Error processing command template: %v", err))
						} else {
							input = processedCmd
						}
					}
				} else if cmd, exists := m.commands[cmdName]; exists && cmd.Handler != nil {
					err := cmd.Handler(&m)
					if err != nil {
						m.outputs = append(m.outputs, fmt.Sprintf("Error executing command: %v", err))
					}
					m.textarea.Reset()
					m.updateViewportContent()
					return m, nil
				} else if cmdName == "/init" {
					input = initPrompt
				}
			}

			// Mark as processing
			m.processing = true
			m.textarea.Reset()

			// Add the input message to the display
			m.outputs = append(m.outputs, "> "+input)
			m.updateViewportContent()

			// Store a copy of the model for the goroutine to use
			llm := m.llm
			config := m.config

			// Get the prompt to process
			prompt := input

			// Reset the global app context for this new operation
			GlobalAppContext.Reset()

			// Use a goroutine to process the request asynchronously
			go func() {
				defer func() {
					// Always notify that processing is done when we exit this goroutine
					if programRef != nil {
						programRef.Send(processingDoneMsg{})
						// Reset context for next operation
						GlobalAppContext.Reset()
					}
				}()

				// Get context for this operation
				ctx := GlobalAppContext.Context()

				// First check if context is already canceled
				if ctx.Err() != nil {
					return
				}

				for {
					// Check if context was cancelled before making any API call
					if ctx.Err() != nil {
						// Operation was cancelled
						return
					}

					// Get response from LLM
					inferenceResponse, err := llm.Inference(ctx, prompt)
					if programRef != nil {
						updateMsgs := []string{}
						if inferenceResponse.Content != "" {
							updateMsgs = append(updateMsgs, inferenceResponse.Content)
						}
						programRef.Send(updateResultMsg{
							outputs: updateMsgs,
							err:     err,
						})

					}
					if err != nil {
						break
					}

					// Clear prompt for next iteration
					prompt = ""

					// Check if we have tool calls
					if len(inferenceResponse.ToolCalls) == 0 {
						break
					}

					// Check context again before processing tool calls
					if ctx.Err() != nil {
						return
					}

					// Process tool calls
					_, toolResults, err := HandleToolCallsWithResultsContext(ctx, inferenceResponse.ToolCalls, config)
					if err != nil {
						// Check if this was a cancellation
						if ctx.Err() != nil {
							return
						}
						if programRef != nil {
							programRef.Send(updateResultMsg{
								outputs: []string{},
								err:     err,
							})
						}
						break
					}

					// Add tool results to LLM conversation history
					for _, result := range toolResults {
						llm.AddToolResult(result.CallID, result.Output)
						if programRef != nil {
							programRef.Send(updateResultMsg{
								outputs: []string{result.Output},
								err:     nil,
							})
						}
					}
				}

			}()

			return m, nil

		// Handle viewport scrolling
		case msg.String() == "up":
			m.viewport, cmd = m.viewport.Update(msg)
			cmds = append(cmds, cmd)
		case msg.String() == "down":
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
			content += "\n"
		}
	}

	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
}

func customViewportKeyMap() viewport.KeyMap {
	return viewport.KeyMap{
		HalfPageUp: key.NewBinding(
			key.WithKeys("ctrl+u"),
			key.WithHelp("ctrl+u", "½ page up"),
		),
		HalfPageDown: key.NewBinding(
			key.WithKeys("ctrl+d"),
			key.WithHelp("ctrl+d", "½ page down"),
		),
		Up: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "down"),
		),
	}
}

// processCommandTemplate processes a command template, replacing {{.ARGS}} with the provided arguments
func processCommandTemplate(cmdContent, args string) (string, error) {
	// If the template doesn't contain {{.ARGS}}, return the content as is
	if !strings.Contains(cmdContent, "{{.ARGS}}") {
		return cmdContent, nil
	}

	// Create template data with arguments
	data := struct {
		ARGS string
	}{
		ARGS: args,
	}

	// Parse and execute the template
	tmpl, err := template.New("cmd").Parse(cmdContent)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return result.String(), nil
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
	// Token info style
	tokenStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")).
		Italic(true)

	// Render viewport content
	contentView := m.viewport.View()

	// Render textarea input
	inputView := m.textarea.View()

	// Render status line
	statusLine := ""

	// Add token usage and cost
	tokenInfo := getTokenInfoString(m.llm)
	statusLine = tokenStyle.Render(tokenInfo)

	// Create spinner line if processing
	spinnerLine := ""
	if m.processing {
		spinnerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			PaddingLeft(2).
			Width(m.viewport.Width)

		spinnerLine = spinnerStyle.Render(m.spinner.View() + " (Press ESC to cancel)")
	}

	// Combine all elements
	if m.processing {
		return fmt.Sprintf("%s\n%s\n%s\n%s",
			contentView,
			spinnerLine,
			inputView,
			statusLine)
	} else {
		return fmt.Sprintf("%s\n\n%s\n%s",
			contentView,
			inputView,
			statusLine)
	}
}

// getTokenInfoString returns a formatted string with token usage and cost information
func getTokenInfoString(llm Llm) string {
	var price float64
	var inputTokens, outputTokens int

	switch provider := llm.(type) {
	case *Claude:
		price = provider.CalculatePrice()
		inputTokens = provider.InputTokens
		outputTokens = provider.OutputTokens
	case *OpenAI:
		price = provider.CalculatePrice()
		inputTokens = provider.InputTokens
		outputTokens = provider.OutputTokens
	}

	return fmt.Sprintf("Tokens: %s in, %s out | Cost: $%.2f",
		formatTokenCount(inputTokens),
		formatTokenCount(outputTokens),
		price)

}

// Global reference to the running program, used for async updates
var programRef *tea.Program

// runInteractiveMode initializes and runs the terminal UI
func runInteractiveMode(llm Llm, config Config) {
	p := tea.NewProgram(initialChatModel(llm, config),
		tea.WithAltScreen(),
		tea.WithReportFocus())
	programRef = p
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
