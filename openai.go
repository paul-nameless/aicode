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
var conversationHistory []openaiMessage
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

// getConversationHistory returns the conversation history as OpenAI messages
func getConversationHistory() []openaiMessage {
	return conversationHistory
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
func AskLlm(model string, messages []openaiMessage) (string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return "", errors.New("OPENAI_API_KEY environment variable not set")
	}

	// Get base URL from environment variable or use default
	baseURL := os.Getenv("AI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	// Convert messages to interface{} slice
	var messagesInterface []interface{}
	for _, msg := range messages {
		messagesInterface = append(messagesInterface, map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}

	url := baseURL + "/v1/chat/completions"
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
		// Initialize conversation for tool calls
		var conversationMessages []interface{}

		// Copy all previous messages
		for _, msg := range messages {
			conversationMessages = append(conversationMessages, map[string]interface{}{
				"role":    msg.Role,
				"content": msg.Content,
			})
		}

		// Maximum number of iterations to prevent infinite loops
		maxIterations := 100
		currentIteration := 0

		// Continue processing tool calls until there are none left or we hit the max iterations
		currentResponse := out
		for len(currentResponse.Choices[0].Message.ToolCalls) > 0 && currentIteration < maxIterations {
			currentIteration++

			// Process the current tool calls
			toolCallsResult, toolResults, err := HandleToolCallsWithResults(currentResponse.Choices[0].Message.ToolCalls)
			if err != nil {
				return "", err
			}

			// If we don't have any tool results, just return the tool calls result
			if len(toolResults) == 0 {
				return toolCallsResult, nil
			}

			// Add assistant message with tool calls
			conversationMessages = append(conversationMessages, map[string]interface{}{
				"role":       "assistant",
				"tool_calls": currentResponse.Choices[0].Message.ToolCalls,
			})

			// Add tool results
			for _, result := range toolResults {
				conversationMessages = append(conversationMessages, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": result.CallID,
					"content":      result.Output,
				})
			}

			// Make a follow-up request with tool results
			followupReqBody := openaiRequest{
				Model:    model,
				Messages: conversationMessages,
				Tools:    tools,
			}

			followupBodyBytes, _ := json.Marshal(&followupReqBody)
			followupReq, err := http.NewRequest("POST", baseURL+"/v1/chat/completions", bytes.NewBuffer(followupBodyBytes))
			if err != nil {
				return toolCallsResult, nil // Fall back to just showing tool calls result
			}

			followupReq.Header.Set("Content-Type", "application/json")
			followupReq.Header.Set("Authorization", "Bearer "+apiKey)

			followupResp, err := http.DefaultClient.Do(followupReq)
			if err != nil {
				return toolCallsResult, nil // Fall back to just showing tool calls result
			}

			followupBody, _ := io.ReadAll(followupResp.Body)
			followupResp.Body.Close()

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

			// Update the current response for the next iteration
			currentResponse = followupOut

			// If there are no more tool calls, we're done
			if len(currentResponse.Choices[0].Message.ToolCalls) == 0 {
				return currentResponse.Choices[0].Message.Content, nil
			}
		}

		// If we've reached the maximum number of iterations, return the last response
		if currentIteration >= maxIterations {
			return fmt.Sprintf("Reached maximum number of tool call iterations (%d). Last response: %s",
				maxIterations, currentResponse.Choices[0].Message.Content), nil
		}

		// Return the final response after all tool calls are processed
		return currentResponse.Choices[0].Message.Content, nil
	}

	// Return the regular content response if no tool calls
	return out.Choices[0].Message.Content, nil
}

// UpdateConversationHistory updates the conversation history with a new entry
func UpdateConversationHistory(content, role string) {
	conversationHistory = append(conversationHistory, openaiMessage{
		Content: content,
		Role:    role,
	})
}
