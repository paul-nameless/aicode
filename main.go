package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// formatTokenCount converts token counts to a more readable format
// For counts >= 1000, it displays as X.Xk (e.g., 1500 → 1.5k)
func formatTokenCount(count int) string {
	if count >= 1000 {
		return fmt.Sprintf("%.1fk", float64(count)/1000)
	}
	return fmt.Sprintf("%d", count)
}

// runSimpleMode processes a single prompt in non-interactive mode
func runSimpleMode(prompt string, llm Llm, config Config) {
	var finalResponse string

	// Process the initial request and any tool calls
	for {
		// Get response from LLM
		inferenceResponse, err := llm.Inference(prompt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Store the response content for later output
		finalResponse = inferenceResponse.Content

		// Check if we have tool calls
		if len(inferenceResponse.ToolCalls) == 0 {
			// No tool calls, we'll print the response outside the loop
			break
		}

		// Process tool calls
		_, toolResults, err := HandleToolCallsWithResults(inferenceResponse.ToolCalls, config)
		if err != nil {
			if config.Debug {
				fmt.Fprintf(os.Stderr, "Error handling tool calls: %v\n", err)
			}
			break
		}

		// Add tool results to the LLM's conversation history
		for _, result := range toolResults {
			llm.AddToolResult(result.CallID, result.Output)
		}

		// Clear prompt for next iteration - we'll continue from conversation history
		prompt = ""
	}

	// In quiet mode, only print the final response content
	fmt.Println(finalResponse)

	// Print token usage and price if NOT in quiet mode
	if !config.Quiet {
		switch provider := llm.(type) {
		case *Claude:
			price := provider.CalculatePrice()
			inputDisplay := formatTokenCount(provider.InputTokens)
			outputDisplay := formatTokenCount(provider.OutputTokens)
			fmt.Printf("Tokens: %s input, %s output. Cost: $%.2f\n", inputDisplay, outputDisplay, price)
		case *OpenAI:
			price := provider.CalculatePrice()
			inputDisplay := formatTokenCount(provider.InputTokens)
			outputDisplay := formatTokenCount(provider.OutputTokens)
			fmt.Printf("Tokens: %s input, %s output. Cost: $%.2f\n", inputDisplay, outputDisplay, price)
		}
	}
}

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

// Function removed since it was unused

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
		content += output
		// Add blank line between messages
		if i < len(m.outputs)-1 {
			content += "\n\n"
		}
	}

	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
}

func (m chatModel) getMaxScroll() int {
	lines := m.getOutputLines()
	usableHeight := m.getHistoryHeight()
	if len(lines) > usableHeight {
		return len(lines) - usableHeight
	}
	return 0
}

func (m chatModel) getOutputLines() []string {
	var lines []string
	for _, output := range m.outputs {
		entry := fmt.Sprintf("%s\n", output)
		lines = append(lines, splitLines(entry)...)
		lines = append(lines, "")
	}
	return lines
}

func (m chatModel) getHistoryHeight() int {
	taLines := m.textarea.LineCount() + 2
	if m.windowHeight > taLines+1 {
		return m.windowHeight - taLines - 1
	}
	return 0
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
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

// This function was removed as each LLM now implements GetFormattedHistory()

// Global reference to the running program, used for async updates
var programRef *tea.Program

// Channel for receiving tool execution updates

func runInteractiveMode(llm Llm, config Config) {
	p := tea.NewProgram(initialChatModel(llm, config), tea.WithAltScreen())
	programRef = p
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// initLLM initializes the appropriate LLM provider based on configuration
func initLLM(config Config) (Llm, error) {
	var llm Llm

	// Choose provider based on configuration or available API keys
	if os.Getenv("ANTHROPIC_API_KEY") != "" || strings.HasPrefix(config.Model, "claude") {
		llm = NewClaude(config)
	} else {
		llm = NewOpenAI(config)
	}

	// Initialize the provider with configuration
	if err := llm.Init(config); err != nil {
		return nil, err
	}

	return llm, nil
}

func expandHomeDir(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}

	usr, err := user.Current()
	if err != nil {
		return path // Return original path if we can't get user home
	}

	return filepath.Join(usr.HomeDir, path[1:])
}

// initializeTools sets up the enabled tools based on user input and updates the config
func initializeTools(toolsFlag string, config *Config) {
	// Initialize enabled tools map in config if it's nil
	if config.EnabledTools == nil {
		config.EnabledTools = []string{}
	}

	// If no tools flag is provided, use what's in config or enable all tools
	if toolsFlag == "" {
		// If config doesn't have enabled tools specified, enable all tools
		// Dynamically enable all the tools from ToolData keys if toolsFlag is empty
		if len(config.EnabledTools) == 0 {
			config.EnabledTools = make([]string, len(ToolData))
			for toolName := range ToolData {
				config.EnabledTools = append(config.EnabledTools, toolName)
			}
		}
		return
	}

	// Parse the comma-separated list of tools
	requestedTools := strings.Split(toolsFlag, ",")

	// Reset enabled tools
	config.EnabledTools = []string{}

	// Validate each tool
	for _, tool := range requestedTools {
		tool = strings.TrimSpace(tool)
		if tool == "" {
			continue
		}

		// Check if the tool is valid
		validTool := false

		for toolName := range ToolData {
			if tool == toolName {
				validTool = true
				break
			}
		}

		if validTool {
			config.EnabledTools = append(config.EnabledTools, tool)
		} else {
			fmt.Fprintf(os.Stderr, "Warning: Unknown tool '%s' will be ignored\n", tool)
		}
	}

	// If no valid tools were provided, enable all tools
	if len(config.EnabledTools) == 0 {
		fmt.Fprintf(os.Stderr, "Warning: No valid tools specified, enabling all tools\n")
		config.EnabledTools = make([]string, len(ToolData))
		for toolName := range ToolData {
			config.EnabledTools = append(config.EnabledTools, toolName)
		}
	}
}

func main() {
	// Parse command line flags
	quietFlag := flag.Bool("q", false, "Run in simple mode with a single prompt")
	nonInteractiveFlag := flag.Bool("n", false, "Run in non-interactive mode")
	configFlag := flag.String("p", "~/.config/aicode/config.yml", "Profile/config file")
	toolsFlag := flag.String("tools", "", "Comma-separated list of tools to enable (default: all tools)")
	debugFlag := flag.Bool("d", false, "Enable debug logging")
	flag.Parse()

	configPath := expandHomeDir(*configFlag)

	// Load configuration
	config, err := LoadConfig(configPath)
	if err != nil {
		slog.Warn("Failed to load configuration", "error", err)
	}

	// Set config.Quiet to CLI flag if present
	config.Quiet = config.Quiet || *quietFlag
	config.Debug = config.Debug || *debugFlag
	config.NonInteractive = config.NonInteractive || *nonInteractiveFlag

	// Setup logging to file using slog
	f, err := os.OpenFile("aicode.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	handler := slog.NewTextHandler(f, &slog.HandlerOptions{
		Level: func() slog.Level {
			if config.Debug {
				return slog.LevelDebug
			}
			return slog.LevelInfo
		}(),
	})
	logger := slog.New(handler)
	slog.SetDefault(logger)
	slog.Info("AiCode started", "version", "0.1")

	// Initialize enabled tools
	initializeTools(*toolsFlag, &config)

	// Initialize LLM provider with configuration
	llm, err := initLLM(config)
	if err != nil {
		slog.Error("Failed to initialize LLM provider", "error", err)
		os.Exit(1)
	}

	// Initialize context and load system prompts
	if err := InitContext(llm); err != nil {
		slog.Warn("Failed to initialize context", "error", err)
	}

	if config.NonInteractive {
		initialPrompt := config.InitialPrompt
		args := flag.Args()
		if len(args) != 0 {
			initialPrompt = strings.Join(args, " ")
		}
		if initialPrompt != "" {
			runSimpleMode(initialPrompt, llm, config)
			return
		}
	}

	runInteractiveMode(llm, config)
}
