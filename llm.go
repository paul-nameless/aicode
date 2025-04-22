package main

import (
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"time"
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

func GetSystemPrompt(config Config) string {
	var b strings.Builder

	b.WriteString(defaultSystemPrompt)
	b.WriteString("\n\nHere is useful information about the environment you are running in:\n<env>\n")

	wd, _ := os.Getwd()
	b.WriteString("Working directory: " + wd + "\n")

	// Platform
	b.WriteString("Platform: " + runtime.GOOS + "\n")

	// Date
	b.WriteString("Today's date: " + time.Now().Format("1/2/2006") + "\n")

	// Model
	b.WriteString("Model: " + config.Model + "\n")
	b.WriteString("</env>\n\n")

	b.WriteString("As you answer the user's questions, you can use the following context:\n\n")

	b.WriteString(`<context name="directoryStructure">Below is a snapshot of this project's file structure at the start of the conversation. This snapshot will NOT update during the conversation.`)
	// b.WriteString(listProjectFiles())
	b.WriteString("</context>\n")

	// 	b.WriteString(`<context name="gitStatus">This is the git status at the start of the conversation. Note that this status is a snapshot in time, and will not update during the conversation.
	// Current branch: better-input

	// Main branch (you will usually use this for PRs):

	// Status:
	// (clean)

	// Recent commits:
	// 91bc1ad Add tool execution status updates and messaging support for async UI feedback
	// 4e5831f Update task dependencies to include formatting step in the lint process
	// 7202ebd Fix lint
	// 0f61995 Ignore configs directory in gitignore
	// 6658216 Add readme.md file</context>
	// `)

	return b.String()
}

// func listProjectFiles() string {
// 	wd, err := os.Getwd()
// 	if err != nil {
// 		return ""
// 	}
// 	files, err := os.ReadDir(wd)
// 	if err != nil {
// 		return ""
// 	}
// 	var b strings.Builder
// 	b.WriteString("- " + wd + "/\n")
// 	for _, f := range files {
// 		name := f.Name()
// 		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
// 			continue
// 		}
// 		b.WriteString("  - " + name + "\n")
// 	}
// 	return b.String()
// }

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
