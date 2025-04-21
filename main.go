package main

import (
	"flag"
	"fmt"
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
	// Update conversation history with the user prompt
	UpdateConversationHistoryText(prompt, "user")

	// Convert conversation history to interfaces
	history := GetConversationHistory()
	messages := ConvertToInterfaces(history)

	var finalResponse string

	// Process the initial request and any tool calls
	for {
		// Get response from LLM
		inferenceResponse, err := llm.Inference(messages)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Store the response content for later output
		finalResponse = inferenceResponse.Content

		// Check if we have tool calls
		if len(inferenceResponse.ToolCalls) == 0 {
			// No tool calls, we'll print the response outside the loop
			// The assistant message is already added to history in the Inference method
			break
		}

		// Process tool calls
		_, toolResults, err := HandleToolCallsWithResults(inferenceResponse.ToolCalls, config)
		if err != nil {
			if debugMode {
				fmt.Fprintf(os.Stderr, "Error handling tool calls: %v\n", err)
			}
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

// Bubbletea model for interactive mode
type chatModel struct {
	textarea     textarea.Model
	llm          Llm
	config       Config
	outputs      []string
	scrollOffset int
	windowHeight int
	err          error
}

func initialChatModel(llm Llm, config Config) chatModel {
	ta := textarea.New()
	ta.Placeholder = "Ask anything..."
	ta.Focus()
	ta.Prompt = "┃ "
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	ta.SetHeight(4)
	return chatModel{
		textarea:     ta,
		llm:          llm,
		config:       config,
		outputs:      []string{},
		scrollOffset: 0,
		windowHeight: 0,
	}
}

func (m chatModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case msg.Type == tea.KeyEnter && msg.Alt:
			// Insert newline on Alt+Enter
			m.textarea.InsertString("\n")
			return m, nil
		case msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyCtrlD:
			return m, tea.Quit
		case msg.Type == tea.KeyEnter:
			input := m.textarea.Value()
			if input != "" {
				// Send to LLM and get response
				UpdateConversationHistoryText(input, "user")
				history := GetConversationHistory()
				messages := ConvertToInterfaces(history)
				for {
					inferenceResponse, err := m.llm.Inference(messages)
					if err != nil {
						m.err = err
						break
					}
					if len(inferenceResponse.ToolCalls) == 0 {
						break
					}
					_, toolResults, err := HandleToolCallsWithResults(inferenceResponse.ToolCalls, m.config)
					if err != nil {
						m.err = err
						break
					}
					for _, result := range toolResults {
						AddToolResultToHistory(result.CallID, result.Output)
					}
					history = GetConversationHistory()
					messages = ConvertToInterfaces(history)
				}
				// Add all conversation content to outputs
				m.outputs = getAllConversationContents()
				m.textarea.Reset()
				m.scrollOffset = 0
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
		for _, line := range splitLines(entry) {
			lines = append(lines, line)
		}
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
	if maxScroll > 0 {
		view += fmt.Sprintf("\n[Scroll: %d/%d]", scrollOffset, maxScroll)
	}
	if m.err != nil {
		view += fmt.Sprintf("\nError: %v", m.err)
	}
	return view
}

// getAllConversationContents returns all conversation messages' content as strings
func getAllConversationContents() []string {
	history := GetConversationHistory()
	var outputs []string
	for _, msg := range history {
		role := msg.Role
		if role == "user" {
			role = ">"
		} else if role == "assistant" {
			role = "<"
		}
		switch content := msg.Content.(type) {
		case string:
			outputs = append(outputs, fmt.Sprintf("%s %s", role, content))
		case []ContentBlock:
			for _, block := range content {
				if block.Text != "" {
					outputs = append(outputs, fmt.Sprintf("%s %s", role, block.Text))
				} else if block.Content != "" {
					outputs = append(outputs, fmt.Sprintf("%s %s", role, block.Content))
				}
			}
		}
	}
	return outputs
}

func runInteractiveMode(llm Llm, config Config) {
	if !config.Quiet {
		fmt.Printf("Model: %s\n", config.Model)
	}
	p := tea.NewProgram(initialChatModel(llm, config), tea.WithAltScreen())
	if err := p.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// Global debug flag
var debugMode bool

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

	// Set global debug flag
	debugMode = config.Debug

	// Choose provider based on configuration or available API keys
	if strings.HasPrefix(config.Model, "claude") || os.Getenv("ANTHROPIC_API_KEY") != "" {
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
	flag.Parse()

	configPath := expandHomeDir(*configFlag)

	// Load configuration
	config, err := LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to load configuration: %v\n", err)
	}

	// Set config.Quiet to CLI flag if present
	config.Quiet = config.Quiet || *quietFlag
	config.NonInteractive = config.NonInteractive || *nonInteractiveFlag

	// Initialize enabled tools
	initializeTools(*toolsFlag, &config)

	// Initialize context and load system prompts
	if err := InitContext(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to initialize context: %v\n", err)
	}

	// Initialize LLM provider with configuration
	llm, err := initLLM(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to initialize LLM provider: %v\n", err)
		os.Exit(1)
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
