package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
)

// Global variables
var conversationHistory []ConversationEntry
var tools []openaiTool // Store the tools loaded at startup

type openaiRequest struct {
	Model    string        `json:"model"`
	Messages []interface{} `json:"messages"`
	Tools    []openaiTool  `json:"tools,omitempty"`
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

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiResponse struct {
	Choices []struct {
		Message struct {
			Role      string     `json:"role"`
			Content   string     `json:"content"`
			ToolCalls []toolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// ConversationEntry represents an entry in the conversation history
type ConversationEntry struct {
	raw  string
	role string // "user" or "assistant"
}

// getConversationHistory returns the conversation history as OpenAI messages
func getConversationHistory() []openaiMessage {
	// This is a package-level variable that will be set from main.go
	var messages []openaiMessage

	// Convert entries to OpenAI messages
	for _, entry := range conversationHistory {
		if entry.role == "user" || entry.role == "assistant" {
			messages = append(messages, openaiMessage{
				Role:    entry.role,
				Content: entry.raw,
			})
		}
	}

	return messages
}

// loadTools loads tools using the schema constants defined in tools.go
func loadTools() ([]openaiTool, error) {
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
		"LS":             {LSToolSchema, LSToolDescription},
		"FindFilesTool":  {FindFilesToolSchema, FindFilesToolDescription},
		"dispatch_agent": {DispatchAgentSchema, DispatchAgentDescription},
		"Fetch":          {FetchToolSchema, FetchToolDescription},
		"GrepTool":       {GrepToolSchema, GrepToolDescription},
	}
	
	// Process each tool
	for toolName, toolInfo := range toolData {
		// Skip dispatch_agent if needed
		if toolName == "dispatch_agent" {
			continue
		}
		
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
	
	fmt.Printf("Loaded %d tools from schemas\n", len(toolsList))
	return toolsList, nil
}

// LoadContext loads system messages and tools
// This should be called once at startup
func LoadContext() error {
	// Load tools
	var err error
	tools, err = loadTools()
	if err != nil {
		return err
	}

	UpdateConversationHistory(defaultSystemPrompt, "system")

	// Check for AI.md file
	if aiContent, err := os.ReadFile("AI.md"); err == nil {
		// Add AI.md content as system message
		UpdateConversationHistory(string(aiContent), "system")
	}

	if claudeContent, err := os.ReadFile("CLAUDE.md"); err == nil {
		// Add CLAUDE.md content as system message
		UpdateConversationHistory(string(claudeContent), "system")
	}

	return nil
}

// ToolCallMessage represents a message with a tool call
type ToolCallMessage struct {
	Role      string     `json:"role"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []toolCall `json:"tool_calls,omitempty"`
}

// ToolResultMessage represents a message with a tool result
type ToolResultMessage struct {
	Role       string `json:"role"`
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
}

// AskLlm sends a prompt to OpenAI's API and returns the response.
// model: for example, "gpt-3.5-turbo" or "gpt-4"
// prompt: your user input
func AskLlm(model, prompt string) (string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return "", errors.New("OPENAI_API_KEY environment variable not set")
	}

	// Get conversation history (which now includes the system message)
	messages := getConversationHistory()

	// Append the current user prompt
	messages = append(messages, openaiMessage{Role: "user", Content: prompt})

	// Convert messages to interface{} slice
	var messagesInterface []interface{}
	for _, msg := range messages {
		messagesInterface = append(messagesInterface, map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}

	url := "https://api.openai.com/v1/chat/completions"
	reqBody := openaiRequest{
		Model:    model,
		Messages: messagesInterface,
		Tools:    tools,
	}
	bodyBytes, _ := json.Marshal(&reqBody)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var out openaiResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.Error != nil {
		return "", errors.New(out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", errors.New("no choices in OpenAI response")
	}

	// Check if the response contains tool calls
	if len(out.Choices[0].Message.ToolCalls) > 0 {
		toolCallsResult, toolResults, err := HandleToolCallsWithResults(out.Choices[0].Message.ToolCalls)
		if err != nil {
			return "", err
		}

		// If we have tool calls and results, send a follow-up request
		if len(toolResults) > 0 {
			// Create a new set of messages with the tool results
			var followupMessages []interface{}

			// Copy all previous messages (convert openaiMessage to map to ensure proper serialization)
			for _, msg := range messages {
				followupMessages = append(followupMessages, map[string]interface{}{
					"role":    msg.Role,
					"content": msg.Content,
				})
			}

			// Add assistant message with tool calls
			followupMessages = append(followupMessages, map[string]interface{}{
				"role":       "assistant",
				"tool_calls": out.Choices[0].Message.ToolCalls,
			})

			// Add tool results
			for _, result := range toolResults {
				followupMessages = append(followupMessages, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": result.CallID,
					"content":      result.Output,
				})
			}

			// Make a follow-up request with tool results
			followupReqBody := openaiRequest{
				Model:    model,
				Messages: followupMessages,
				Tools:    tools,
			}

			followupBodyBytes, _ := json.Marshal(&followupReqBody)
			followupReq, err := http.NewRequest("POST", url, bytes.NewBuffer(followupBodyBytes))
			if err != nil {
				return toolCallsResult, nil // Fall back to just showing tool calls result
			}

			followupReq.Header.Set("Content-Type", "application/json")
			followupReq.Header.Set("Authorization", "Bearer "+apiKey)

			followupResp, err := http.DefaultClient.Do(followupReq)
			if err != nil {
				return toolCallsResult, nil // Fall back to just showing tool calls result
			}
			defer followupResp.Body.Close()

			followupBody, _ := io.ReadAll(followupResp.Body)
			var followupOut openaiResponse

			if err := json.Unmarshal(followupBody, &followupOut); err != nil {
				return toolCallsResult, nil // Fall back to just showing tool calls result
			}

			if followupOut.Error != nil {
				return toolCallsResult, nil // Fall back to just showing tool calls result
			}

			if len(followupOut.Choices) == 0 {
				return toolCallsResult, nil // Fall back to just showing tool calls result
			}

			// Return the model's response after processing the tool results
			return followupOut.Choices[0].Message.Content, nil
		}

		return toolCallsResult, nil
	}

	// Return the regular content response if no tool calls
	return out.Choices[0].Message.Content, nil
}

// UpdateConversationHistory updates the conversation history with a new entry
func UpdateConversationHistory(raw, role string) {
	conversationHistory = append(conversationHistory, ConversationEntry{
		raw:  raw,
		role: role,
	})
}
