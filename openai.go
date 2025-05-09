package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

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

type openaiReasoning struct {
	Effort string `json:"effort,omitempty"`
}

type openaiRequest struct {
	Model       string           `json:"model"`
	Messages    []openaiMessage  `json:"messages"`
	Tools       []openaiTool     `json:"tools,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	Reasoning   *openaiReasoning `json:"reasoning,omitempty"`
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
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details,omitempty"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// loadOpenAITools loads tools using the schema constants defined in tools.go
func loadOpenAITools() []openaiTool {
	var toolsList []openaiTool

	// Process each tool
	for toolName, toolInfo := range ToolData {
		// Parse the JSON schema
		var toolSchema struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		}

		err := json.Unmarshal([]byte(toolInfo.Schema), &toolSchema)
		if err != nil {
			slog.Error("Failed to unmarshal tool schema", "tool", toolName, "error", err)
			os.Exit(1)
		}

		toolsList = append(toolsList, openaiTool{
			Type: "function",
			Function: openaiFunction{
				Name:        toolSchema.Name,
				Description: toolInfo.Description, // Use the markdown description
				Parameters:  toolSchema.Parameters,
			},
		})

	}

	return toolsList
}

// Inference implements the Llm interface for OpenAI
func (o *OpenAI) Inference(ctx context.Context, prompt string) (InferenceResponse, error) {
	// Add the user's prompt to the conversation
	o.AddMessage(prompt, "user")

	// Try inference with potential retry for rate limiting
	return o.inferenceWithRetry(ctx, false)
}

// inferenceWithRetry handles the actual inference with optional retry for rate limiting
func (o *OpenAI) inferenceWithRetry(ctx context.Context, isRetry bool) (InferenceResponse, error) {
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
	baseURL := o.Config.BaseUrl
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	url := baseURL + "/v1/chat/completions"
	reqBody := openaiRequest{
		Model:     o.Config.Model,
		Messages:  o.conversationHistory,
		Tools:     o.tools,
		MaxTokens: o.MaxTokens,
	}

	// Add reasoning effort parameter for OpenAI models that support it
	if strings.HasPrefix(o.Config.Model, "o") {
		reqBody.Reasoning = &openaiReasoning{
			Effort: o.Config.ReasoningEffort,
		}
	}
	bodyBytes, _ := json.Marshal(&reqBody)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return InferenceResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.Config.ApiKey)

	// Use the context for cancellation
	req = req.WithContext(ctx)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return InferenceResponse{}, err
	}
	defer resp.Body.Close()

	// Check for rate limit error (HTTP 429)
	if resp.StatusCode == 429 && !isRetry {
		slog.Debug("Received rate limit (429) error. Summarizing conversation and retrying...")
		return o.inferenceWithRetry(ctx, true)
	}

	body, _ := io.ReadAll(resp.Body)

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
			return o.inferenceWithRetry(ctx, true)
		}
		return InferenceResponse{}, errors.New(out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return InferenceResponse{}, errors.New("no choices in OpenAI response")
	}

	// Accumulate token usage
	o.InputTokens += out.Usage.PromptTokens
	o.TotalInputTokens += out.Usage.PromptTokens
	o.OutputTokens += out.Usage.CompletionTokens
	o.TotalOutputTokens += out.Usage.CompletionTokens

	// Track cached tokens if available
	if out.Usage.PromptTokensDetails.CachedTokens > 0 {
		o.CachedInputTokens += out.Usage.PromptTokensDetails.CachedTokens
	}

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

// Function removed since it was unused

// OpenAI struct implements Llm interface
type OpenAI struct {
	Model                      string
	TotalInputTokens           int     // Track total input tokens used
	TotalOutputTokens          int     // Track total output tokens used
	InputTokens                int     // Track total input tokens used
	CachedInputTokens          int     // Track total cached input tokens used
	OutputTokens               int     // Track total output tokens used
	InputPricePerMillion       float64 // Price per million input tokens
	CachedInputPricePerMillion float64
	OutputPricePerMillion      float64         // Price per million output tokens
	Config                     Config          // Configuration
	ContextWindowSize          int             // Maximum context window size in tokens
	conversationHistory        []openaiMessage // Internal conversation history
	tools                      []openaiTool
	MaxTokens                  int
}

func (o *OpenAI) Clear() {
	o.conversationHistory = make([]openaiMessage, 0)
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
		Model:       o.Config.Model,
		Messages:    summaryMessages,
		MaxTokens:   o.MaxTokens,
		Temperature: 0.2, // Lower temperature for more consistent summaries
	}

	// Add reasoning effort parameter for OpenAI models that support it
	if strings.HasPrefix(o.Config.Model, "o") {
		reqBody.Reasoning = &openaiReasoning{
			Effort: o.Config.ReasoningEffort,
		}
	}

	// Create request
	bodyBytes, _ := json.Marshal(&reqBody)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.Config.ApiKey)

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
		{
			Role:    "system",
			Content: GetSystemPrompt(o.Config),
			Type:    "text",
		},
		{
			Role:    "assistant",
			Content: summaryText,
			Type:    "text",
		},
	}

	// Check if the last message is a tool response that needs its corresponding tool call
	toolCallNeeded := false
	var toolCallID string

	// If we have at least 1 message and it's a tool message
	if len(lastMessages) > 0 && lastMessages[len(lastMessages)-1].Role == "tool" {
		// Check if it's a tool result message
		if lastMessages[len(lastMessages)-1].Type == "tool_result" {
			toolCallNeeded = true
			toolCallID = lastMessages[len(lastMessages)-1].ToolCallID
		}
	}

	// If we need to find a matching tool call, look through history
	if toolCallNeeded {
		// Find the corresponding assistant message with the tool call
		for i := len(o.conversationHistory) - 3; i >= 0; i-- {
			if o.conversationHistory[i].Role == "assistant" && len(o.conversationHistory[i].ToolCalls) > 0 {
				for _, toolCall := range o.conversationHistory[i].ToolCalls {
					if toolCall.ID == toolCallID {
						// Found the matching tool call, include it in preserved messages
						lastMessages = append([]openaiMessage{o.conversationHistory[i]}, lastMessages...)
						break
					}
				}
			}
			// Once we found the tool call, stop searching
			if len(lastMessages) > 2 {
				break
			}
		}
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
	// Calculate uncached input tokens
	nonCachedInputTokens := o.TotalInputTokens - o.CachedInputTokens
	nonCachedInputPrice := float64(nonCachedInputTokens) * o.InputPricePerMillion / 1000000.0
	cachedInputPrice := float64(o.CachedInputTokens) * o.CachedInputPricePerMillion / 1000000.0
	inputPrice := nonCachedInputPrice + cachedInputPrice
	outputPrice := float64(o.TotalOutputTokens) * o.OutputPricePerMillion / 1000000.0

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
	outputs = append(outputs, fmt.Sprintf("Model: %s", o.Config.Model))

	for _, msg := range o.conversationHistory {
		if msg.Role == "system" || msg.Content == "" {
			continue
		}
		role := ""
		if msg.Role == "user" {
			role = "> "
		} else if msg.Role == "assistant" {
			role = ""
		} else if msg.Role == "tool" {
			continue
		} else {
			role = msg.Role + ": "
		}

		if msg.Type == "tool_result" {
			outputs = append(outputs, fmt.Sprintf("%s%s", role, msg.Content))
		} else if msg.Type == "text" || msg.Type == "" {
			outputs = append(outputs, fmt.Sprintf("%s%s", role, msg.Content))
		}
	}

	return outputs
}

func (o *OpenAI) GetModel() string {
	return o.Config.Model
}

// NewOpenAI creates a new OpenAI provider
func NewOpenAI(config Config) *OpenAI {
	conversationHistory := []openaiMessage{
		{
			Role:    "system",
			Content: GetSystemPrompt(config),
			Type:    "text",
		},
	}

	tools := loadOpenAITools()

	return &OpenAI{
		Config:                     config,
		InputTokens:                0,
		OutputTokens:               0,
		InputPricePerMillion:       2,
		CachedInputPricePerMillion: 0.5,
		OutputPricePerMillion:      8,
		ContextWindowSize:          200_000,
		conversationHistory:        conversationHistory,
		tools:                      tools,
		MaxTokens:                  20_000,
	}
}
