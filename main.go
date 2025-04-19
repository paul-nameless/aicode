package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
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
		_, toolResults, err := HandleToolCallsWithResults(inferenceResponse.ToolCalls)
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

	// Print token usage and price if debug mode is enabled
	if debugMode {
		if claude, ok := llm.(*Claude); ok {
			price := claude.CalculatePrice()
			inputDisplay := formatTokenCount(claude.InputTokens)
			outputDisplay := formatTokenCount(claude.OutputTokens)
			fmt.Printf("Tokens: %s input, %s output. Cost: $%.2f\n", inputDisplay, outputDisplay, price)
		}
	}
}

// runInteractiveMode reads user input in a loop until Ctrl+C/D
func runInteractiveMode(llm Llm, config Config) {
	// Get model from environment variable or use default based on provider
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
			inferenceResponse, err := llm.Inference(messages)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				break
			}

			// Check if we have tool calls
			if len(inferenceResponse.ToolCalls) == 0 {
				// No tool calls, print the response and continue the outer loop
				if inferenceResponse.Content != "" {
					fmt.Println("< " + inferenceResponse.Content)
					// The assistant message is already added to history in the Inference method
				}
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

		// Print token usage and price if debug mode is enabled
		if debugMode {
			if claude, ok := llm.(*Claude); ok {
				price := claude.CalculatePrice()
				inputDisplay := formatTokenCount(claude.InputTokens)
				outputDisplay := formatTokenCount(claude.OutputTokens)
				fmt.Printf("Tokens: %s input, %s output. Cost: $%.2f\n", inputDisplay, outputDisplay, price)
			}
		}
		fmt.Println(strings.Repeat("━", 64))
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		os.Exit(1)
	}
}

// Global debug flag
var debugMode bool

// AllTools is a list of all available tools
var AllTools = []string{
	"GrepTool",
	"GlobTool",
	"FindFilesTool",
	"Bash",
	"Ls",
	"View",
	"Edit",
	"Replace",
	"Fetch",
	"dispatch_agent",
}

// DefaultDispatchAgentTools is the list of tools available to dispatch_agent by default
var DefaultDispatchAgentTools = []string{
	"GlobTool",
	"GrepTool",
	"Ls",
	"View",
}

// initLLM initializes the appropriate LLM provider based on configuration
func initLLM(config Config) (Llm, error) {
	var llm Llm

	// Set global debug flag
	debugMode = config.Debug

	// Choose provider based on configuration or available API keys
	if config.Provider == "claude" || os.Getenv("ANTHROPIC_API_KEY") != "" {
		llm = NewClaude(config)
	} else {
		llm = NewOpenAI()
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
	configFlag := flag.String("p", "~/.config/aicode/config.yml", "Profile/config file")
	toolsFlag := flag.String("tools", "", "Comma-separated list of tools to enable (default: all tools)")
	flag.Parse()

	configPath := expandHomeDir(*configFlag)

	// Load configuration
	config, err := LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to load configuration: %v\n", err)
	}
	
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
	}

	runInteractiveMode(llm)

}
