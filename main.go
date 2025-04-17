package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
)

const (
	defaultOpenAIModel = "gpt-4.1-nano"
	defaultClaudeModel = "claude-3-haiku-20240307"
)

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
				fmt.Println("< " + inferenceResponse.Content)
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
	}

	runInteractiveMode(llm)

}
