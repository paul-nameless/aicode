package main

import (
	"encoding/json"
	"os"
)

// ToolCall represents a tool to be called
type ToolCall struct {
	ID    string
	Name  string
	Input []byte
}

// InferenceResponse represents a unified response format for all LLM providers
type InferenceResponse struct {
	Content   string
	ToolCalls []ToolCall
}

// Llm interface defines methods for LLM providers
type Llm interface {
	// Inference sends a prompt to the LLM and returns the unified response
	Inference(prompt string) (InferenceResponse, error)
	// AddMessage adds a message to the conversation history
	AddMessage(content string, role string)
	// AddToolResult adds a tool result to the conversation history
	AddToolResult(toolUseID string, result string)
	// GetFormattedHistory returns the conversation history formatted for display
	GetFormattedHistory() []string
	// Init initializes the LLM provider with configuration
	Init(config Config) error
	// CalculatePrice calculates the total cost of the conversation
	CalculatePrice() float64
	// Clear clears the conversation history and preserves the system prompt
	Clear()
}

// ContentBlock represents a block of content in a message (text or tool related)
type ContentBlock struct {
	Type      string          `json:"type"` // "text", "tool_use", or "tool_result"
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"` // For tool_result
}

// Message defines a standard message format for all LLM providers
type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // Can be string or ContentBlock array
}

// InitContext loads system message files
// This should be called once at startup for each LLM provider
func InitContext(llm Llm) error {
	// Check for AI.md file
	if aiContent, err := os.ReadFile("AI.md"); err == nil {
		// Add AI.md content as user message
		llm.AddMessage(string(aiContent), "user")
	}

	if claudeContent, err := os.ReadFile("CLAUDE.md"); err == nil {
		// Add CLAUDE.md content as user message
		llm.AddMessage(string(claudeContent), "user")
	}

	return nil
}
