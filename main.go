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
func runSimpleMode(prompt string, llm Llm) {
	// Update conversation history with the user prompt
	UpdateConversationHistoryText(prompt, "user")

	// Convert conversation history to interfaces
	history := GetConversationHistory()
	messages := ConvertToInterfaces(history)

	// Process the initial request and any tool calls
	for {
		// Get response from LLM
		inferenceResponse, err := llm.Inference(messages)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Check if we have tool calls
		if len(inferenceResponse.ToolCalls) == 0 {
			// No tool calls, print the response and exit
			fmt.Println(inferenceResponse.Content)
			// The assistant message is already added to history in the Inference method
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

	// Print token usage and price if available
	if claude, ok := llm.(*Claude); ok {
		price := claude.CalculatePrice()
		inputDisplay := formatTokenCount(claude.InputTokens)
		outputDisplay := formatTokenCount(claude.OutputTokens)
		fmt.Printf("Tokens: %s input, %s output. Cost: $%.2f\n", inputDisplay, outputDisplay, price)
	}
}

// runInteractiveMode reads user input in a loop until Ctrl+C/D
func runInteractiveMode(llm Llm) {
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

		// Print token usage and price if available
		if claude, ok := llm.(*Claude); ok {
			price := claude.CalculatePrice()
			inputDisplay := formatTokenCount(claude.InputTokens)
			outputDisplay := formatTokenCount(claude.OutputTokens)
			fmt.Printf("Tokens: %s input, %s output. Cost: $%.2f\n", inputDisplay, outputDisplay, price)
		}
		fmt.Println(strings.Repeat("━", 64))
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		os.Exit(1)
	}
}

// initLLM initializes the appropriate LLM provider based on configuration
func initLLM(configPath string) (Llm, error) {
	var llm Llm

	// Load configuration
	config, err := LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %v", err)
	}

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

func main() {
	// Parse command line flags
	quietFlag := flag.Bool("q", false, "Run in simple mode with a single prompt")
	configFlag := flag.String("p", "~/.config/aicode/config.yml", "Profile/config file")
	flag.Parse()

	configPath := expandHomeDir(*configFlag)

	// Initialize context and load system prompts
	if err := InitContext(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to initialize context: %v\n", err)
	}

	// Initialize LLM provider with configuration
	llm, err := initLLM(configPath)
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
