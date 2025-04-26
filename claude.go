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

type claudeRequest struct {
	Model       string                `json:"model"`
	Messages    []claudeMessage       `json:"messages"`
	System      []claudeSystemMessage `json:"system,omitempty"`
	Tools       []claudeTool          `json:"tools,omitempty"`
	MaxTokens   int                   `json:"max_tokens"`
	Temperature float64               `json:"temperature,omitempty"`
}

type claudeCacheControl struct {
	Type string `json:"type"`
}

type claudeTool struct {
	Name         string              `json:"name"`
	Description  string              `json:"description"`
	InputSchema  json.RawMessage     `json:"input_schema"`
	CacheControl *claudeCacheControl `json:"cache_control,omitempty"`
}

type claudeMessage struct {
	Role         string              `json:"role"`
	Content      interface{}         `json:"content"`
	CacheControl *claudeCacheControl `json:"cache_control,omitempty"`
}

type claudeSystemMessage struct {
	Type         string              `json:"type"`
	Text         string              `json:"text"`
	CacheControl *claudeCacheControl `json:"cache_control,omitempty"`
}

type claudeContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Name      string          `json:"name,omitempty"`
	ID        string          `json:"id,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type claudeResponse struct {
	ID         string               `json:"id"`
	Type       string               `json:"type"`
	Role       string               `json:"role"`
	Content    []claudeContentBlock `json:"content"`
	StopReason string               `json:"stop_reason"`
	Usage      struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// loadClaudeTools loads tools using the schema constants defined in tools.go
func loadClaudeTools() []claudeTool {
	var toolsList []claudeTool

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

		toolsList = append(toolsList, claudeTool{
			Name:        toolSchema.Name,
			Description: toolInfo.Description, // Use the markdown description
			InputSchema: toolSchema.Parameters,
		})
	}

	if len(toolsList) >= 1 {
		toolsList[len(toolsList)-1].CacheControl = &claudeCacheControl{Type: "ephemeral"}
	}

	return toolsList
}

// Inference implements the Llm interface for Claude
func (c *Claude) Inference(prompt string) (InferenceResponse, error) {
	// Add the user's prompt to the conversation
	c.AddMessage(prompt, "user")

	// Try inference with potential retry for rate limiting
	return c.inferenceWithRetry(false)
}

// inferenceWithRetry handles the actual inference with optional retry for rate limiting
func (c *Claude) inferenceWithRetry(isRetry bool) (InferenceResponse, error) {
	// Check if we need to summarize the conversation
	if c.shouldSummarizeConversation() || isRetry {
		slog.Debug("Context usage approaching limit. Summarizing conversation...")
		beforeCount := len(c.conversationHistory)
		beforeTokens := c.InputTokens

		err := c.summarizeConversation()
		if err != nil {
			slog.Warn("Failed to summarize conversation", "error", err)
		} else {
			afterCount := len(c.conversationHistory)
			afterTokens := c.InputTokens
			reductionPercent := 100 - (float64(afterTokens) * 100 / float64(beforeTokens))
			slog.Debug("Conversation summarized",
				"beforeCount", beforeCount,
				"afterCount", afterCount,
				"reductionPercent", reductionPercent,
				"beforeTokens", beforeTokens,
				"afterTokens", afterTokens)
		}
	}

	// Get base URL from environment variable or use default
	baseURL := c.Config.BaseUrl
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}

	url := baseURL + "/v1/messages"
	reqBody := claudeRequest{
		Model:     c.Config.Model,
		Messages:  c.conversationHistory,
		System:    c.systemMessages,
		Tools:     c.tools,
		MaxTokens: 20000,
	}

	// Create request
	bodyBytes, _ := json.Marshal(&reqBody)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return InferenceResponse{}, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.Config.ApiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return InferenceResponse{}, err
	}
	defer resp.Body.Close()

	// Check for rate limit error (HTTP 429)
	if resp.StatusCode == 429 && !isRetry {
		slog.Debug("Received rate limit (429) error. Summarizing conversation and retrying...")
		return c.inferenceWithRetry(true)
	}

	body, _ := io.ReadAll(resp.Body)

	var out claudeResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return InferenceResponse{}, fmt.Errorf("error unmarshaling response: %v\nResponse body: %s", err, string(body))
	}

	if out.Error != nil {
		// Check if the error is about rate limiting and we haven't retried yet
		slog.Error("Inference error", "url", url, "error", out.Error.Message)
		if (strings.Contains(strings.ToLower(out.Error.Message), "rate limit") ||
			strings.Contains(strings.ToLower(out.Error.Message), "too many requests")) && !isRetry {
			slog.Debug("Received rate limit error in response. Summarizing conversation and retrying...")
			return c.inferenceWithRetry(true)
		}
		return InferenceResponse{}, errors.New(out.Error.Message)
	}

	// Accumulate token usage
	c.InputTokens += out.Usage.InputTokens
	c.TotalInputTokens += out.Usage.InputTokens
	c.OutputTokens += out.Usage.OutputTokens
	c.TotalOutputTokens += out.Usage.OutputTokens

	// Track cache usage if available
	if out.Usage.CacheCreationInputTokens > 0 {
		c.CacheCreationInputTokens += out.Usage.CacheCreationInputTokens
	}
	if out.Usage.CacheReadInputTokens > 0 {
		c.CacheReadInputTokens += out.Usage.CacheReadInputTokens
		c.CachedInputTokens += out.Usage.CacheReadInputTokens
	}

	// Process the response into our unified format and build our response
	response := InferenceResponse{
		ToolCalls: []ToolCall{},
	}

	// Create the assistant message for history
	var assistantContent interface{}
	var assistantBlocks []claudeContentBlock
	hasBlocks := false

	for _, block := range out.Content {
		if block.Type == "text" {
			response.Content += block.Text
			assistantBlocks = append(assistantBlocks, claudeContentBlock{
				Type: "text",
				Text: block.Text,
			})
			hasBlocks = true
		} else if block.Type == "tool_use" {
			// Convert to our unified tool call format
			response.ToolCalls = append(response.ToolCalls, ToolCall{
				ID:    block.ID,
				Name:  block.Name,
				Input: block.Input,
			})

			// Add to Claude blocks format for history
			assistantBlocks = append(assistantBlocks, block)
			hasBlocks = true
		}
	}

	// Create the assistant message
	if hasBlocks {
		assistantContent = assistantBlocks
	} else {
		assistantContent = response.Content
	}

	// Add to conversation history
	c.conversationHistory = append(c.conversationHistory, claudeMessage{
		Role:    "assistant",
		Content: assistantContent,
	})

	return response, nil
}

// Function removed since it was unused

// Claude struct implements Llm interface
type Claude struct {
	Model                      string
	TotalInputTokens           int             // Track total input tokens used
	TotalOutputTokens          int             // Track total output tokens used
	InputTokens                int             // Track total input tokens used
	OutputTokens               int             // Track total output tokens used
	CachedInputTokens          int             // Track total cached input tokens used
	CacheCreationInputTokens   int             // Track total tokens used for cache creation
	CacheReadInputTokens       int             // Track total tokens read from cache
	InputPricePerMillion       float64         // Price per million input tokens
	CachedInputPricePerMillion float64         // Price per million cached input tokens
	OutputPricePerMillion      float64         // Price per million output tokens
	Config                     Config          // Configuration
	ContextWindowSize          int             // Maximum context window size in tokens
	conversationHistory        []claudeMessage // Internal conversation history
	systemMessages             []claudeSystemMessage
	tools                      []claudeTool
}

func (c *Claude) Clear() {
	c.conversationHistory = make([]claudeMessage, 0)
}

// shouldSummarizeConversation checks if the conversation needs to be summarized
// based on the actual token usage compared to the context window size
func (c *Claude) shouldSummarizeConversation() bool {
	// Use the actual token count from previous API calls
	usedTokens := c.InputTokens

	// Check if we're using more than 80% of the context window
	contextThreshold := int(float64(c.ContextWindowSize) * 0.8)
	return usedTokens > contextThreshold
}

// summarizeConversation creates a summary of the conversation history
// and updates the conversation history
func (c *Claude) summarizeConversation() error {
	if len(c.conversationHistory) <= 2 {
		// Not enough conversation to summarize
		return nil
	}

	slog.Debug("Summarizing conversation...")

	// Save the last couple of messages to preserve context
	lastMessages := c.conversationHistory[len(c.conversationHistory)-2:]

	// Copy conversation for summarization request
	summaryMessages := make([]claudeMessage, len(c.conversationHistory))
	copy(summaryMessages, c.conversationHistory)

	// Prepare a special message asking for the summary
	summaryMessages = append(summaryMessages, claudeMessage{
		Role:    "user",
		Content: "Please summarize our conversation so far following the instructions in the system prompt.",
	})

	systemMessages := []claudeSystemMessage{
		{
			Type:         "text",
			Text:         summaryPrompt,
			CacheControl: &claudeCacheControl{Type: "ephemeral"},
		},
	}

	// Create a request to summarize the conversation
	url := "https://api.anthropic.com/v1/messages"
	reqBody := claudeRequest{
		Model:       c.Config.Model,
		Messages:    summaryMessages,
		System:      systemMessages,
		MaxTokens:   20000,
		Temperature: 0.2, // Lower temperature for more consistent summaries
	}

	// Create request
	bodyBytes, _ := json.Marshal(&reqBody)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.Config.ApiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var out claudeResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("error unmarshaling response: %v", err)
	}

	if out.Error != nil {
		fmt.Printf("Inference error: url=%s, error=%s\n", url, out.Error.Message)
		return errors.New(out.Error.Message)
	}

	// Extract the summary text
	var summaryText string
	for _, block := range out.Content {
		if block.Type == "text" {
			summaryText += block.Text
		}
	}

	// Clean up any extra whitespace and ensure the summary is not empty
	summaryText = strings.TrimSpace(summaryText)

	if summaryText == "" {
		return errors.New("received empty summary")
	}

	// Replace conversation history with system message, summary, and last messages
	newConversation := []claudeMessage{
		// Keep the system message (should be the first one)
		// c.conversationHistory[0],
		// Add summary as assistant message
		{
			Role:    "assistant",
			Content: summaryText,
		},
	}

	// Check if last message is a tool result that needs its corresponding tool call
	toolCallNeeded := false
	var toolUseID string

	// If we have at least 1 message and it's a user message
	if len(lastMessages) > 0 && lastMessages[len(lastMessages)-1].Role == "user" {
		// Check if it's a tool result message
		if blocks, ok := lastMessages[len(lastMessages)-1].Content.([]claudeContentBlock); ok {
			for _, block := range blocks {
				if block.Type == "tool_result" {
					toolCallNeeded = true
					toolUseID = block.ToolUseID
				}
			}
		}
	}

	// If we need to find a matching tool call, look through history
	if toolCallNeeded {
		// Find the corresponding tool call
		for i := len(c.conversationHistory) - 3; i >= 0; i-- {
			if c.conversationHistory[i].Role == "assistant" {
				if blocks, ok := c.conversationHistory[i].Content.([]claudeContentBlock); ok {
					for _, block := range blocks {
						if block.Type == "tool_use" && block.ID == toolUseID {
							// Found the matching tool call, include it in preserved messages
							lastMessages = append([]claudeMessage{c.conversationHistory[i]}, lastMessages...)
							break
						}
					}
				}
			}
			// Once we found the tool call, stop searching
			if len(lastMessages) > 2 {
				break
			}
		}
	}

	// Add back the last messages
	newConversation = append(newConversation, lastMessages...)
	c.conversationHistory = newConversation

	// Calculate token stats before reset
	inputTokensBefore := c.InputTokens

	// We need to estimate the size of the new conversation
	// A simple approach is to count characters and divide by 4 (approximation)
	var summaryLength int
	for _, msg := range c.conversationHistory {
		// Handle string content
		if contentStr, ok := msg.Content.(string); ok {
			summaryLength += len(contentStr)
			continue
		}

		// Handle array of content blocks
		if contentBlocks, ok := msg.Content.([]claudeContentBlock); ok {
			for _, block := range contentBlocks {
				if block.Type == "text" {
					summaryLength += len(block.Text)
				} else if block.Type == "tool_result" {
					summaryLength += len(block.Content)
				} else if block.Type == "tool_use" {
					// Add estimated size for tool use blocks
					summaryLength += 100 // Rough estimate for tool metadata
					inputBytes, _ := block.Input.MarshalJSON()
					summaryLength += len(string(inputBytes))
				}
			}
		}
	}

	// Estimate tokens after summarization (roughly 4 characters per token)
	// Use float division for more accurate token estimation, then convert to int
	inputTokensAfter := int(float64(summaryLength) / 4.0)
	tokenReduction := 100.0
	if inputTokensAfter > 0 && inputTokensBefore > 0 {
		tokenReduction = 100 - (float64(inputTokensAfter) * 100 / float64(inputTokensBefore))
	}

	// Estimate character counts
	charsBefore := inputTokensBefore * 4
	charsAfter := summaryLength

	slog.Debug("Summarized conversation",
		"inputTokensBefore", inputTokensBefore,
		"inputTokensAfter", inputTokensAfter,
		"tokenReduction", tokenReduction,
		"charsBefore", charsBefore,
		"charsAfter", charsAfter)

	// Reset the token counter since we've summarized the conversation
	c.InputTokens = 0
	c.OutputTokens = 0

	return nil
}

// CalculatePrice calculates the price for Claude API usage
func (c *Claude) CalculatePrice() float64 {
	// Calculate uncached input tokens
	nonCachedInputTokens := c.TotalInputTokens - c.CachedInputTokens
	nonCachedInputPrice := float64(nonCachedInputTokens) * c.InputPricePerMillion / 1000000.0
	cachedInputPrice := float64(c.CachedInputTokens) * c.CachedInputPricePerMillion / 1000000.0
	inputPrice := nonCachedInputPrice + cachedInputPrice
	outputPrice := float64(c.TotalOutputTokens) * c.OutputPricePerMillion / 1000000.0
	return inputPrice + outputPrice
}

// AddMessage adds a message to the conversation history
func (c *Claude) AddMessage(content string, role string) {
	if content == "" {
		return
	}
	c.conversationHistory = append(c.conversationHistory, claudeMessage{
		Role:    role,
		Content: content,
	})
}

// AddToolResult adds a tool result to the conversation history
func (c *Claude) AddToolResult(toolUseID string, result string) {
	if result == "" {
		result = "No result"
	}

	c.conversationHistory = append(c.conversationHistory, claudeMessage{
		Role: "user",
		Content: []claudeContentBlock{
			{
				Type:      "tool_result",
				ToolUseID: toolUseID,
				Content:   result,
			},
		},
	})
}

// GetFormattedHistory returns the conversation history formatted for display
func (c *Claude) GetFormattedHistory() []string {
	var outputs []string
	outputs = append(outputs, fmt.Sprintf("Model: %s", c.Config.Model))

	for _, msg := range c.conversationHistory {
		role := msg.Role
		if role == "user" {
			role = ">"
		} else if role == "assistant" {
			role = "<"
		}

		// Handle string content
		if contentStr, ok := msg.Content.(string); ok {
			outputs = append(outputs, fmt.Sprintf("%s %s", role, contentStr))
			continue
		}

		// Handle array of content blocks
		if contentBlocks, ok := msg.Content.([]claudeContentBlock); ok {
			for _, block := range contentBlocks {
				if block.Type == "text" {
					outputs = append(outputs, fmt.Sprintf("%s %s", role, block.Text))
				} else if block.Type == "tool_result" {
					outputs = append(outputs, fmt.Sprintf("%s [Tool Result: %s]", role, block.Content))
				} else if block.Type == "tool_use" {
					outputs = append(outputs, fmt.Sprintf("%s [Tool Use: %s]", role, block.Name))
				}
			}
		}
	}

	return outputs
}

// NewClaude creates a new Claude provider
func NewClaude(config Config) *Claude {
	tools := loadClaudeTools()

	return &Claude{
		Config:                     config,
		InputTokens:                0,
		OutputTokens:               0,
		CachedInputTokens:          0,
		CacheCreationInputTokens:   0,
		CacheReadInputTokens:       0,
		InputPricePerMillion:       3.0, // $3 per million input tokens
		CachedInputPricePerMillion: 3.75,
		OutputPricePerMillion:      15.0, // $15 per million output tokens
		ContextWindowSize:          80_000,
		conversationHistory:        []claudeMessage{},
		tools:                      tools,
		systemMessages: []claudeSystemMessage{
			{
				Type:         "text",
				Text:         GetSystemPrompt(config),
				CacheControl: &claudeCacheControl{Type: "ephemeral"},
			},
		},
	}
}

// Init initializes the Claude provider with given configuration
func (c *Claude) Init(config Config) error {
	return nil
}
