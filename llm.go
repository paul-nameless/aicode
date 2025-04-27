package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	Inference(ctx context.Context, prompt string) (InferenceResponse, error)
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
	GetModel() string
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
	b.WriteString(listProjectFiles())
	b.WriteString("</context>\n")

	// Add git status if available
	gitCurrentBranch, err := ExecuteCommand("git branch --show-current")
	if err == nil && gitCurrentBranch != "" {
		b.WriteString(`<context name="gitStatus">This is the git status at the start of the conversation. Note that this status is a snapshot in time, and will not update during the conversation.` + "\n")
		b.WriteString("Current branch: " + strings.TrimSpace(gitCurrentBranch) + "\n")

		// Get main/master branch
		gitMainBranch, err := ExecuteCommand("git symbolic-ref refs/remotes/origin/HEAD 2>/dev/null | sed 's@^refs/remotes/origin/@@'")
		if err == nil && gitMainBranch != "" {
			b.WriteString("Main branch (you will usually use this for PRs): " + strings.TrimSpace(gitMainBranch) + "\n")
		}

		// Get git status
		gitStatus, err := ExecuteCommand("git status")
		if err == nil {
			b.WriteString(gitStatus + "\n")
		}

		// Get recent commits
		gitLog, err := ExecuteCommand("git log --oneline -4")
		if err == nil {
			b.WriteString(gitLog + "\n")
		}

		b.WriteString("</context>\n")
	}

	for _, fname := range config.SystemFiles {
		if content, err := os.ReadFile(fname); err == nil {
			b.WriteString("\nContents of " + fname + "\n\n")
			b.WriteString(string(content))
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

func listProjectFiles() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(wd + "/\n")
	listFilesRecursive(wd, "", "  ", &b)
	return b.String()
}

func listFilesRecursive(root, path, indent string, b *strings.Builder) {
	fullPath := filepath.Join(root, path)
	files, err := os.ReadDir(fullPath)
	if err != nil {
		return
	}

	for _, f := range files {
		name := f.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}

		// relativePath := filepath.Join(path, name)
		if f.IsDir() {
			b.WriteString(indent + "- " + name + "/\n")
			// listFilesRecursive(root, relativePath, indent+"  ", b)
		} else {
			b.WriteString(indent + "- " + name + "\n")
		}
	}
}
