package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// runSimpleMode processes a single prompt in non-interactive mode
func runSimpleMode(llm Llm, config Config) {
	var finalResponse string

	// Create a fresh context for this operation
	GlobalAppContext.Reset()
	ctx := GlobalAppContext.Context()

	// Process the initial request and any tool calls
	for {
		// Get response from LLM with context
		inferenceResponse, err := llm.Inference(ctx, config.InitialPrompt)
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

		// Process tool calls with context
		_, toolResults, err := HandleToolCallsWithResultsContext(ctx, inferenceResponse.ToolCalls, config)
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

// initLLM initializes the appropriate LLM provider based on configuration
func initLLM(config Config) (Llm, error) {
	var llm Llm

	// Choose provider based on configuration or available API keys
	if strings.HasPrefix(config.Model, "claude") {
		llm = NewClaude(config)
	} else {
		llm = NewOpenAI(config)
	}

	return llm, nil
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
		slog.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Set config.Quiet to CLI flag if present
	config.Quiet = config.Quiet || *quietFlag
	config.Debug = config.Debug || *debugFlag
	config.NonInteractive = config.NonInteractive || *nonInteractiveFlag
	if config.InitialPrompt == "" {
		args := flag.Args()
		if len(args) != 0 {
			config.InitialPrompt = strings.Join(args, " ")
		}
	}

	// Initialize the logger
	InitLogger(config.Debug)
	defer LogFile.Close()

	// Initialize enabled tools
	initializeTools(*toolsFlag, &config)

	// Initialize LLM provider with configuration
	llm, err := initLLM(config)
	if err != nil {
		slog.Error("Failed to initialize LLM provider", "error", err)
		os.Exit(1)
	}

	if config.NonInteractive {
		if config.InitialPrompt == "" {
			fmt.Println("No initial prompt provided")
			os.Exit(1)
		}
		runSimpleMode(llm, config)
		return
	}

	runInteractiveMode(llm, config)
}
