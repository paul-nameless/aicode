package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

// Global variables for OpenAI
var openaiTools []openaiTool // Store the tools loaded at startup

type openaiMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Type       string           `json:"type,omitempty"` // For determining message type internally
}

type openaiToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function openaiFunction `json:"function"`
}

type openaiRequest struct {
	Model       string          `json:"model"`
	Messages    []openaiMessage `json:"messages"`
	Tools       []openaiTool    `json:"tools,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
}

type openaiTool struct {
	Type     string         `json:"type"`
	Function openaiFunction `json:"function"`
}

type openaiFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Arguments   json.RawMessage `json:"arguments,omitempty"`
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
		"View":          {ViewToolSchema, ViewToolDescription},
		"Replace":       {ReplaceToolSchema, ReplaceToolDescription},
		"Edit":          {EditToolSchema, EditToolDescription},
		"Bash":          {BashToolSchema, BashToolDescription},
		"Ls":            {LsToolSchema, LsToolDescription},
		"FindFiles":     {FindFilesSchema, FindFilesDescription},
		"DispatchAgent": {DispatchAgentSchema, DispatchAgentDescription},
		"Fetch":         {FetchToolSchema, FetchToolDescription},
		"Grep":          {GrepSchema, GrepDescription},
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
func (o *OpenAI) Inference(prompt string) (InferenceResponse, error) {
	// Add the user's prompt to the conversation
	o.AddMessage(prompt, "user")

	// Try inference with potential retry for rate limiting
	return o.inferenceWithRetry(false)
}

// inferenceWithRetry handles the actual inference with optional retry for rate limiting
func (o *OpenAI) inferenceWithRetry(isRetry bool) (InferenceResponse, error) {
	// Check if we need to summarize the conversation
	if o.shouldSummarizeConversation() || isRetry {
		slog.Debug("Context usage approaching limit. Summarizing conversation...")
		beforeCount := len(o.conversationHistory)
		beforeTokens := o.InputTokens

		err := o.summarizeConversation()
		if err != nil {
			slog.Warn("Failed to summarize conversation", "error", err)
		} else {
			afterCount := len(o.conversationHistory)
			afterTokens := o.InputTokens
			reductionPercent := 100 - (float64(afterTokens) * 100 / float64(beforeTokens))
			slog.Debug("Conversation summarized",
				"beforeCount", beforeCount,
				"afterCount", afterCount,
				"reductionPercent", reductionPercent)
		}
	}

	// Get base URL from environment variable or use default
	baseURL := os.Getenv("OPENAI_API_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	url := baseURL + "/v1/chat/completions"
	reqBody := openaiRequest{
		Model:     o.Model,
		Messages:  o.conversationHistory,
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
		slog.Debug("Received rate limit (429) error. Summarizing conversation and retrying...")
		return o.inferenceWithRetry(true)
	}

	body, _ := io.ReadAll(resp.Body)

	// Debug output
	if len(body) > 200 {
		slog.Debug("OpenAI response (truncated)", "response", string(body[:200]))
	} else {
		slog.Debug("OpenAI response", "response", string(body))
	}

	var out openaiResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return InferenceResponse{}, fmt.Errorf("error unmarshaling response: %v\nResponse body: %s", err, string(body))
	}
	if out.Error != nil {
		// Check if the error is about rate limiting and we haven't retried yet
		slog.Error("Inference error", "url", url, "error", out.Error.Message)
		if (strings.Contains(strings.ToLower(out.Error.Message), "rate limit") ||
			strings.Contains(strings.ToLower(out.Error.Message), "too many requests")) && !isRetry {
			slog.Debug("Received rate limit error in response. Summarizing conversation and retrying...")
			return o.inferenceWithRetry(true)
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

	// Create assistant message for conversation history
	assistantMessage := openaiMessage{
		Role:    "assistant",
		Content: out.Choices[0].Message.Content,
		Type:    "text",
	}

	// Process tool calls if any
	if len(out.Choices[0].Message.ToolCalls) > 0 {
		var toolCalls []openaiToolCall

		for _, toolCall := range out.Choices[0].Message.ToolCalls {
			// Add to response for API consumer
			toolCallData := ToolCall{
				ID:    toolCall.ID,
				Name:  toolCall.Function.Name,
				Input: toolCall.Function.Arguments,
			}
			response.ToolCalls = append(response.ToolCalls, toolCallData)

			// Add to OpenAI format tool calls for history
			toolCalls = append(toolCalls, openaiToolCall{
				ID:   toolCall.ID,
				Type: "function",
				Function: openaiFunction{
					Name:      toolCall.Function.Name,
					Arguments: toolCall.Function.Arguments,
				},
			})
		}

		// Add tool calls to the message
		assistantMessage.ToolCalls = toolCalls
	}

	// Add the assistant message to conversation history
	o.conversationHistory = append(o.conversationHistory, assistantMessage)

	return response, nil
}

// convertMessagesToOpenAIFormat converts messages with content blocks to OpenAI format
func convertMessagesToOpenAIFormat(messages []Message) []interface{} {
	// Add system message at beginning with defaultSystemPrompt
	result := []interface{}{
		map[string]interface{}{
			"role":    "system",
			"content": defaultSystemPrompt,
		},
	}

	for _, msg := range messages {
		role := msg.Role
		content := msg.Content

		// If it's a regular string content, just keep it as is
		if contentStr, ok := content.(string); ok {
			result = append(result, map[string]interface{}{
				"role":    role,
				"content": contentStr,
			})
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

		// Default - convert to map
		result = append(result, map[string]interface{}{
			"role":    role,
			"content": content,
		})
	}

	return result
}

// OpenAI struct implements Llm interface
type OpenAI struct {
	Model                 string
	InputTokens           int             // Track total input tokens used
	OutputTokens          int             // Track total output tokens used
	InputPricePerMillion  float64         // Price per million input tokens
	OutputPricePerMillion float64         // Price per million output tokens
	Config                Config          // Configuration
	apiKey                string          // API key for OpenAI API
	ContextWindowSize     int             // Maximum context window size in tokens
	conversationHistory   []openaiMessage // Internal conversation history
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
// and updates the conversation with the summary
func (o *OpenAI) summarizeConversation() error {
	if len(o.conversationHistory) <= 2 {
		// Not enough conversation to summarize
		return nil
	}

	// Save the last few messages (typically user messages that need responses)
	lastMessages := o.conversationHistory[len(o.conversationHistory)-2:]

	// Copy the current conversation for the summarization request
	summaryMessages := make([]openaiMessage, len(o.conversationHistory))
	copy(summaryMessages, o.conversationHistory)

	// Prepare a special message asking for the summary
	summaryMessages = append(summaryMessages, openaiMessage{
		Role:    "user",
		Content: summaryPrompt,
		Type:    "text",
	})

	// Create a request to summarize the conversation
	url := "https://api.openai.com/v1/chat/completions"
	reqBody := openaiRequest{
		Model:       o.Model,
		Messages:    summaryMessages,
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

	// Replace the conversation history with just the system message, summary and recent messages
	newHistory := []openaiMessage{
		// Keep the system prompt that should be first in the list
		o.conversationHistory[0],
		// Add summary as assistant message
		{
			Role:    "assistant",
			Content: summaryText,
			Type:    "text",
		},
	}

	// Add back the most recent messages
	newHistory = append(newHistory, lastMessages...)
	o.conversationHistory = newHistory

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

// AddMessage adds a message to the conversation history
func (o *OpenAI) AddMessage(content string, role string) {
	if content == "" {
		return
	}
	o.conversationHistory = append(o.conversationHistory, openaiMessage{
		Role:    role,
		Content: content,
		Type:    "text",
	})
}

// AddToolResult adds a tool result to the conversation history
func (o *OpenAI) AddToolResult(toolUseID string, result string) {
	if result == "" {
		result = "No result"
	}

	o.conversationHistory = append(o.conversationHistory, openaiMessage{
		Role:       "tool",
		ToolCallID: toolUseID,
		Content:    result,
		Type:       "tool_result",
	})
}

// GetFormattedHistory returns the conversation history formatted for display
func (o *OpenAI) GetFormattedHistory() []string {
	var outputs []string
	outputs = append(outputs, fmt.Sprintf("Model: %s", o.Model))

	for _, msg := range o.conversationHistory {
		role := msg.Role
		if role == "system" {
			continue
		}
		if role == "user" {
			role = ">"
		} else if role == "assistant" {
			role = "<"
		} else if role == "tool" {
			role = "T"
		}

		if msg.Type == "tool_result" {
			outputs = append(outputs, fmt.Sprintf("%s %s", role, msg.Content))
		} else if msg.Type == "text" || msg.Type == "" {
			outputs = append(outputs, fmt.Sprintf("%s %s", role, msg.Content))
		}
	}

	return outputs
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
		InputPricePerMillion:  2,
		OutputPricePerMillion: 8,
		ContextWindowSize:     200000,
		conversationHistory:   []openaiMessage{},
	}
}

// Init initializes the OpenAI provider with given configuration
func (o *OpenAI) Init(config Config) error {
	o.Config = config

	// Add system prompt as the first message
	o.conversationHistory = append(o.conversationHistory, openaiMessage{
		Role:    "system",
		Content: defaultSystemPrompt,
		Type:    "text",
	})

	return LoadOpenAIContext()
}
