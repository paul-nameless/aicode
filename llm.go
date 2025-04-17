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
	Inference(model string, messages []interface{}) (InferenceResponse, error)
	// Init initializes the LLM provider
	Init() error
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

// Global conversation history shared between providers
var conversationHistory []Message

// InitContext loads system messages and tools
// This should be called once at startup
func InitContext() error {
	// Initialize conversation history
	conversationHistory = []Message{}

	// Add system prompts
	UpdateConversationHistoryText(defaultSystemPrompt, "system")

	// Check for AI.md file
	if aiContent, err := os.ReadFile("AI.md"); err == nil {
		// Add AI.md content as system message
		UpdateConversationHistoryText(string(aiContent), "system")
	}

	if claudeContent, err := os.ReadFile("CLAUDE.md"); err == nil {
		// Add CLAUDE.md content as system message
		UpdateConversationHistoryText(string(claudeContent), "system")
	}

	return nil
}

// UpdateConversationHistoryText updates the conversation history with a new text entry
func UpdateConversationHistoryText(content, role string) {
	if content == "" {
		return
	}
	conversationHistory = append(conversationHistory, Message{
		Content: content,
		Role:    role,
	})
}

// UpdateConversationHistory maintains backward compatibility
func UpdateConversationHistory(content, role string) {
	UpdateConversationHistoryText(content, role)
}

// UpdateConversationHistoryBlocks updates the conversation history with content blocks
func UpdateConversationHistoryBlocks(contentBlocks []ContentBlock, role string) {
	conversationHistory = append(conversationHistory, Message{
		Content: contentBlocks,
		Role:    role,
	})
}

// AddToolResultToHistory adds a tool result to the conversation history
func AddToolResultToHistory(toolUseID, result string) {
	// Make sure we have a non-empty result
	if result == "" {
		result = "No result"
	}

	toolResult := []ContentBlock{
		{
			Type:      "tool_result",
			ToolUseID: toolUseID,
			Content:   result,
		},
	}

	conversationHistory = append(conversationHistory, Message{
		Role:    "user",
		Content: toolResult,
	})
}

// GetConversationHistory returns the conversation history
func GetConversationHistory() []Message {
	return conversationHistory
}

// ConvertToInterfaces converts Message slice to interface{} slice for provider-agnostic usage
func ConvertToInterfaces(messages []Message) []interface{} {
	var messagesInterface []interface{}
	for _, msg := range messages {
		messageMap := map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		}
		messagesInterface = append(messagesInterface, messageMap)
	}
	return messagesInterface
}
