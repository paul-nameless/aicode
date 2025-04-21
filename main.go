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
	tea "github.com/charmbracelet/bubbletea"
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

// Custom message type for updating results asynchronously
type updateResultMsg struct {
	outputs []string
	err     error
}

// Bubbletea model for interactive mode
type chatModel struct {
	textarea     textarea.Model
	llm          Llm
	config       Config
	outputs      []string
	scrollOffset int
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
	return chatModel{
		textarea:     ta,
		llm:          llm,
		config:       config,
		outputs:      outputs,
		scrollOffset: 0,
		windowHeight: 0,
		processing:   false,
	}
}

func (m chatModel) Init() tea.Cmd {
	return textarea.Blink
}

// Function removed since it was unused

func (m chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case updateResultMsg:
		// Handle the update from our async processing
		m.outputs = msg.outputs
		m.err = msg.err
		m.processing = false
		m.scrollOffset = 0
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
					m.scrollOffset = 0
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
					m.scrollOffset = 0
					return m, nil
				}

				// Mark as processing
				m.processing = true
				m.textarea.Reset()

				// Add a processing message to the display
				m.outputs = append(m.outputs, "> "+input)
				m.outputs = append(m.outputs, "< Processing...")
				m.scrollOffset = 0

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
		case msg.String() == "up":
			maxScroll := m.getMaxScroll()
			if m.scrollOffset < maxScroll {
				m.scrollOffset++
			}
			return m, nil
		case msg.String() == "down":
			if m.scrollOffset > 0 {
				m.scrollOffset--
			}
			return m, nil
		case msg.String() == "pgup":
			maxScroll := m.getMaxScroll()
			if m.scrollOffset+5 < maxScroll {
				m.scrollOffset += 5
			} else {
				m.scrollOffset = maxScroll
			}
			return m, nil
		case msg.String() == "pgdown":
			if m.scrollOffset-5 > 0 {
				m.scrollOffset -= 5
			} else {
				m.scrollOffset = 0
			}
			return m, nil
		case (msg.Type == tea.KeyCtrlD):
			if m.scrollOffset-10 > 0 {
				m.scrollOffset -= 10
			} else {
				m.scrollOffset = 0
			}
			return m, nil
		case (msg.Type == tea.KeyCtrlU):
			maxScroll := m.getMaxScroll()
			if m.scrollOffset+10 < maxScroll {
				m.scrollOffset += 10
			} else {
				m.scrollOffset = maxScroll
			}
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.textarea.SetWidth(msg.Width - 4)
		m.windowHeight = msg.Height
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
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
	var view string
	lines := m.getOutputLines()
	historyHeight := m.getHistoryHeight()
	maxScroll := m.getMaxScroll()
	scrollOffset := m.scrollOffset
	if scrollOffset > maxScroll {
		scrollOffset = maxScroll
	}
	if scrollOffset < 0 {
		scrollOffset = 0
	}
	start := 0
	if len(lines) > historyHeight {
		start = len(lines) - historyHeight - scrollOffset
		if start < 0 {
			start = 0
		}
	}
	end := start + historyHeight
	if end > len(lines) {
		end = len(lines)
	}
	for _, line := range lines[start:end] {
		view += line + "\n"
	}
	view += m.textarea.View()

	// Show status information
	statusInfo := ""
	if maxScroll > 0 {
		statusInfo += fmt.Sprintf("[Scroll: %d/%d]", scrollOffset, maxScroll)
	}
	if m.processing {
		if statusInfo != "" {
			statusInfo += " "
		}
		statusInfo += "[Processing...]"
	}
	if statusInfo != "" {
		view += fmt.Sprintf("\n%s", statusInfo)
	}

	if m.err != nil {
		view += fmt.Sprintf("\nError: %v", m.err)
	}
	return view
}

// This function was removed as each LLM now implements GetFormattedHistory()

// Global reference to the running program, used for async updates
var programRef *tea.Program

func runInteractiveMode(llm Llm, config Config) {
	p := tea.NewProgram(initialChatModel(llm, config), tea.WithAltScreen())
	programRef = p
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// AllTools is a list of all available tools
var AllTools = []string{
	"Grep",
	"GlobTool",
	"FindFiles",
	"Bash",
	"Ls",
	"View",
	"Edit",
	"Replace",
	"Fetch",
	"DispatchAgent",
}

// DefaultDispatchAgentTools is the list of tools available to DispatchAgent by default
var DefaultDispatchAgentTools = []string{
	"GlobTool",
	"Grep",
	"Ls",
	"View",
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
		if len(config.EnabledTools) == 0 {
			config.EnabledTools = make([]string, len(AllTools))
			copy(config.EnabledTools, AllTools)
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
		for _, availableTool := range AllTools {
			if tool == availableTool {
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
		config.EnabledTools = make([]string, len(AllTools))
		copy(config.EnabledTools, AllTools)
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
