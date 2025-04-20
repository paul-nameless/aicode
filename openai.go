package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Global variables for OpenAI
var openaiTools []openaiTool // Store the tools loaded at startup

type openaiRequest struct {
	Model       string        `json:"model"`
	Messages    []interface{} `json:"messages"`
	Tools       []openaiTool  `json:"tools,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

type openaiTool struct {
	Type     string         `json:"type"`
	Function openaiFunction `json:"function"`
}

type openaiFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type openaiResponse struct {
	Choices []struct {
		Message struct {
			Role      string     `json:"role"`
			Content   string     `json:"content"`
			ToolCalls []toolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// loadOpenAITools loads tools using the schema constants defined in tools.go
func loadOpenAITools() ([]openaiTool, error) {
	var toolsList []openaiTool

	// Map of tool names to their schema constants and descriptions
	toolData := map[string]struct {
		Schema      string
		Description string
	}{
		"View":           {ViewToolSchema, ViewToolDescription},
		"Replace":        {ReplaceToolSchema, ReplaceToolDescription},
		"Edit":           {EditToolSchema, EditToolDescription},
		"Bash":           {BashToolSchema, BashToolDescription},
		"Ls":             {LsToolSchema, LsToolDescription},
		"FindFilesTool":  {FindFilesToolSchema, FindFilesToolDescription},
		"dispatch_agent": {DispatchAgentSchema, DispatchAgentDescription},
		"Fetch":          {FetchToolSchema, FetchToolDescription},
		"GrepTool":       {GrepToolSchema, GrepToolDescription},
	}

	// Process each tool
	for toolName, toolInfo := range toolData {
		// Parse the JSON schema
		var toolSchema struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		}

		if err := json.Unmarshal([]byte(toolInfo.Schema), &toolSchema); err == nil {
			// Successfully parsed the schema
			toolsList = append(toolsList, openaiTool{
				Type: "function",
				Function: openaiFunction{
					Name:        toolSchema.Name,
					Description: toolInfo.Description, // Use the markdown description
					Parameters:  toolSchema.Parameters,
				},
			})
		} else {
			fmt.Printf("Failed to parse JSON schema for tool %s: %v\n", toolName, err)

			// Fallback to just using the name
			toolsList = append(toolsList, openaiTool{
				Type: "function",
				Function: openaiFunction{
					Name:        toolName,
					Description: "Tool for " + toolName,
				},
			})
		}
	}

	return toolsList, nil
}

// LoadOpenAIContext loads tools for OpenAI
// This should be called once at startup
func LoadOpenAIContext() error {
	// Load tools
	var err error
	openaiTools, err = loadOpenAITools()
	if err != nil {
		return err
	}

	return nil
}

// Inference implements the Llm interface for OpenAI
func (o *OpenAI) Inference(messages []interface{}) (InferenceResponse, error) {
	// Try inference with potential retry for rate limiting
	return o.inferenceWithRetry(messages, false)
}

// inferenceWithRetry handles the actual inference with optional retry for rate limiting
func (o *OpenAI) inferenceWithRetry(messages []interface{}, isRetry bool) (InferenceResponse, error) {
	// Check if we need to summarize the conversation
	if o.shouldSummarizeConversation() || isRetry {
		if debugMode {
			fmt.Println("Context usage approaching limit. Summarizing conversation...")
		}
		beforeCount := len(conversationHistory)
		beforeTokens := o.InputTokens

		err := o.summarizeConversation(messages)
		if err != nil {
			if debugMode {
				fmt.Printf("Warning: Failed to summarize conversation: %v\n", err)
			}
		} else if debugMode {
			afterCount := len(conversationHistory)
			afterTokens := o.InputTokens
			reductionPercent := 100 - (float64(afterTokens) * 100 / float64(beforeTokens))
			fmt.Printf("Conversation summarized: %d messages → %d messages (%.1f%% token reduction)\n",
				beforeCount, afterCount, reductionPercent)
		}

		// Rebuild messages from updated conversation history
		messages = ConvertToInterfaces(conversationHistory)
	}

	// Get base URL from environment variable or use default
	baseURL := os.Getenv("OPENAI_API_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	// Convert content blocks to OpenAI format if needed
	convertedMessages := convertMessagesToOpenAIFormat(messages)

	url := baseURL + "/v1/chat/completions"
	reqBody := openaiRequest{
		Model:     o.Model,
		Messages:  convertedMessages,
		Tools:     openaiTools,
		MaxTokens: 4000,
	}
	bodyBytes, _ := json.Marshal(&reqBody)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return InferenceResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return InferenceResponse{}, err
	}
	defer resp.Body.Close()

	// Check for rate limit error (HTTP 429)
	if resp.StatusCode == 429 && !isRetry {
		if debugMode {
			fmt.Println("Received rate limit (429) error. Summarizing conversation and retrying...")
		}
		return o.inferenceWithRetry(messages, true)
	}

	body, _ := io.ReadAll(resp.Body)

	// Debug output
	if debugMode {
		if len(body) > 200 {
			fmt.Printf("OpenAI response (first 200 chars): %s...\n", string(body[:200]))
		} else {
			fmt.Printf("OpenAI response: %s\n", string(body))
		}
	}

	var out openaiResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return InferenceResponse{}, fmt.Errorf("error unmarshaling response: %v\nResponse body: %s", err, string(body))
	}
	if out.Error != nil {
		// Check if the error is about rate limiting and we haven't retried yet
		fmt.Printf("Inference error: url=%s, error=%s\n", url, out.Error.Message)
		if (strings.Contains(strings.ToLower(out.Error.Message), "rate limit") ||
			strings.Contains(strings.ToLower(out.Error.Message), "too many requests")) && !isRetry {
			if debugMode {
				fmt.Println("Received rate limit error in response. Summarizing conversation and retrying...")
			}
			return o.inferenceWithRetry(messages, true)
		}
		return InferenceResponse{}, errors.New(out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return InferenceResponse{}, errors.New("no choices in OpenAI response")
	}

	// Accumulate token usage
	o.InputTokens += out.Usage.PromptTokens
	o.OutputTokens += out.Usage.CompletionTokens

	// Convert to our unified response format
	response := InferenceResponse{
		Content:   out.Choices[0].Message.Content,
		ToolCalls: []ToolCall{},
	}

	// Extract any tool calls
	hasToolUse := false
	var toolUseBlocks []ContentBlock

	for _, toolCall := range out.Choices[0].Message.ToolCalls {
		hasToolUse = true

		toolCallData := ToolCall{
			ID:    toolCall.ID,
			Name:  toolCall.Function.Name,
			Input: toolCall.Function.Arguments,
		}

		response.ToolCalls = append(response.ToolCalls, toolCallData)

		// Collect tool use blocks for conversation history
		toolUseBlocks = append(toolUseBlocks, ContentBlock{
			Type:  "tool_use",
			ID:    toolCall.ID,
			Name:  toolCall.Function.Name,
			Input: toolCall.Function.Arguments,
		})
	}

	// If there are tool calls, add them to conversation history
	if hasToolUse {
		// Also add the tool use to our conversation history
		responseBlocks := []ContentBlock{
			{
				Type: "text",
				Text: response.Content,
			},
		}

		// Add all tool use blocks
		responseBlocks = append(responseBlocks, toolUseBlocks...)

		// Update conversation history with the blocks
		UpdateConversationHistoryBlocks(responseBlocks, "assistant")
	} else if response.Content != "" {
		// If there were no tool calls but we have content, just add it as text
		UpdateConversationHistoryText(response.Content, "assistant")
	}

	return response, nil
}

// convertMessagesToOpenAIFormat converts messages with content blocks to OpenAI format
func convertMessagesToOpenAIFormat(messages []interface{}) []interface{} {
	// Add system message at beginning with defaultSystemPrompt
	result := []interface{}{
		map[string]interface{}{
			"role":    "system",
			"content": defaultSystemPrompt,
		},
	}

	for _, msg := range messages {
		if msgMap, ok := msg.(map[string]interface{}); ok {
			role, _ := msgMap["role"].(string)
			content := msgMap["content"]

			// If it's a regular string content, just keep it as is
			if _, ok := content.(string); ok {
				result = append(result, msgMap)
				continue
			}

			// If it's a tool result (from user), convert to OpenAI's format
			if role == "user" {
				if blocks, ok := content.([]ContentBlock); ok {
					for _, block := range blocks {
						if block.Type == "tool_result" {
							// Format for OpenAI
							result = append(result, map[string]interface{}{
								"role":         "tool", // OpenAI uses "tool" role
								"tool_call_id": block.ToolUseID,
								"content":      block.Content,
							})
						}
					}
				}
				continue
			}

			// If it's an assistant with tool calls
			if role == "assistant" {
				if blocks, ok := content.([]ContentBlock); ok {
					hasToolUse := false

					// Check if there are any tool_use blocks
					for _, block := range blocks {
						if block.Type == "tool_use" {
							hasToolUse = true
							break
						}
					}

					if hasToolUse {
						// Create a formatted message for OpenAI
						toolCalls := []map[string]interface{}{}
						var textContent string

						for _, block := range blocks {
							if block.Type == "text" {
								textContent = block.Text
							} else if block.Type == "tool_use" {
								toolCalls = append(toolCalls, map[string]interface{}{
									"id":   block.ID,
									"type": "function",
									"function": map[string]interface{}{
										"name":      block.Name,
										"arguments": string(block.Input),
									},
								})
							}
						}

						result = append(result, map[string]interface{}{
							"role":       role,
							"content":    textContent,
							"tool_calls": toolCalls,
						})
						continue
					}
				}
			}

			// Default - just pass through
			result = append(result, msgMap)
		}
	}

	return result
}

// OpenAI struct implements Llm interface
type OpenAI struct {
	Model                 string
	InputTokens           int     // Track total input tokens used
	OutputTokens          int     // Track total output tokens used
	InputPricePerMillion  float64 // Price per million input tokens
	OutputPricePerMillion float64 // Price per million output tokens
	Config                Config  // Configuration
	apiKey                string  // API key for OpenAI API
	ContextWindowSize     int     // Maximum context window size in tokens
}

// shouldSummarizeConversation checks if the conversation needs to be summarized
// based on the actual token usage compared to the context window size
func (o *OpenAI) shouldSummarizeConversation() bool {
	// Use the actual token count from previous API calls
	usedTokens := o.InputTokens

	// Check if we're using more than 80% of the context window
	contextThreshold := int(float64(o.ContextWindowSize) * 0.8)
	return usedTokens > contextThreshold
}

// summarizeConversation creates a summary of the conversation history
// and replaces the history with the summary, while preserving the last user message
func (o *OpenAI) summarizeConversation(messages []interface{}) error {
	if len(conversationHistory) <= 2 {
		// Not enough conversation to summarize
		return nil
	}

	lastMessages := conversationHistory[len(conversationHistory)-2:]

	// Convert messages to OpenAI format for the summarization request
	convertedMessages := convertMessagesToOpenAIFormat(messages)

	// Prepare a special message asking for the summary
	// This makes it clearer to the model what we want
	convertedMessages = append(convertedMessages, map[string]interface{}{
		"role":    "user",
		"content": summaryPrompt,
	})

	// Create a request to summarize the conversation
	url := "https://api.openai.com/v1/chat/completions"
	reqBody := openaiRequest{
		Model:       o.Model,
		Messages:    convertedMessages,
		MaxTokens:   4000,
		Temperature: 0.2, // Lower temperature for more consistent summaries
	}

	// Create request
	bodyBytes, _ := json.Marshal(&reqBody)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var out openaiResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("error unmarshaling response: %v", err)
	}

	if out.Error != nil {
		fmt.Printf("Inference error: url=%s, error=%s\n", url, out.Error.Message)
		return errors.New(out.Error.Message)
	}

	if len(out.Choices) == 0 {
		return errors.New("no choices in OpenAI summary response")
	}

	// Extract the summary text
	summaryText := out.Choices[0].Message.Content

	// Clean up any extra whitespace and ensure the summary is not empty
	summaryText = strings.TrimSpace(summaryText)

	if summaryText == "" {
		return errors.New("received empty summary")
	}

	// Replace conversation history with the summary as an assistant message
	conversationHistory = []Message{
		{
			Role:    "assistant",
			Content: summaryText,
		},
	}

	if len(lastMessages) != 0 {
		conversationHistory = append(conversationHistory, lastMessages...)
	}

	// Reset the token counter since we've summarized the conversation
	o.InputTokens = 0
	o.OutputTokens = 0

	return nil
}

// CalculatePrice calculates the price for OpenAI API usage
func (o *OpenAI) CalculatePrice() float64 {
	inputPrice := float64(o.InputTokens) * o.InputPricePerMillion / 1000000.0
	outputPrice := float64(o.OutputTokens) * o.OutputPricePerMillion / 1000000.0
	return inputPrice + outputPrice
}

// NewOpenAI creates a new OpenAI provider
func NewOpenAI(config Config) *OpenAI {
	model := config.Model
	if model == "" {
		model = os.Getenv("OPENAI_MODEL")
		if model == "" {
			model = "gpt-4.1-nano"
		}
	}

	apiKey := config.ApiKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	return &OpenAI{
		Model:                 model,
		apiKey:                apiKey,
		InputTokens:           0,
		OutputTokens:          0,
		InputPricePerMillion:  0.5, // Approximate price for GPT-4 input tokens
		OutputPricePerMillion: 1.5, // Approximate price for GPT-4 output tokens
		ContextWindowSize:     128000,
	}
}

// Init initializes the OpenAI provider with given configuration
func (o *OpenAI) Init(config Config) error {
	o.Config = config
	return LoadOpenAIContext()
}
