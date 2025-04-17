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

// Global variables for OpenAI
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

// LoadOpenAIContext loads tools for OpenAI
// This should be called once at startup
func LoadOpenAIContext() error {
	// Load tools
	var err error
	tools, err = loadOpenAITools()
	if err != nil {
		return err
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

func (o *OpenAI) CalculatePrice() float64 {
	return 0
}

// Inference implements the Llm interface for OpenAI
func (o *OpenAI) Inference(messages []interface{}) (InferenceResponse, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return InferenceResponse{}, errors.New("OPENAI_API_KEY environment variable not set")
	}

	// Get base URL from environment variable or use default
	baseURL := os.Getenv("AI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	// Convert content blocks to OpenAI format if needed
	convertedMessages := convertMessagesToOpenAIFormat(messages)

	url := baseURL + "/v1/chat/completions"
	reqBody := openaiRequest{
		Model:    o.Model,
		Messages: convertedMessages,
		Tools:    tools,
	}
	bodyBytes, _ := json.Marshal(&reqBody)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return InferenceResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return InferenceResponse{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// Debug output
	if len(body) > 200 {
		fmt.Printf("OpenAI response (first 200 chars): %s...\n", string(body[:200]))
	} else {
		fmt.Printf("OpenAI response: %s\n", string(body))
	}

	var out openaiResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return InferenceResponse{}, err
	}
	if out.Error != nil {
		return InferenceResponse{}, errors.New(out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return InferenceResponse{}, errors.New("no choices in OpenAI response")
	}

	// Convert to our unified response format
	response := InferenceResponse{
		Content:   out.Choices[0].Message.Content,
		ToolCalls: []ToolCall{},
	}

	// Extract any tool calls
	for _, toolCall := range out.Choices[0].Message.ToolCalls {
		toolCallData := ToolCall{
			ID:    toolCall.ID,
			Name:  toolCall.Function.Name,
			Input: toolCall.Function.Arguments,
		}

		response.ToolCalls = append(response.ToolCalls, toolCallData)
	}

	// If there are tool calls, add them to the conversation history
	if len(response.ToolCalls) > 0 {
		blocks := []ContentBlock{}

		// Only add text block if there's actual content
		if response.Content != "" {
			blocks = append(blocks, ContentBlock{
				Type: "text",
				Text: response.Content,
			})
		}

		// Add each tool call as a block
		for _, call := range response.ToolCalls {
			blocks = append(blocks, ContentBlock{
				Type:  "tool_use",
				ID:    call.ID,
				Name:  call.Name,
				Input: call.Input,
			})
		}

		// Add to conversation history
		UpdateConversationHistoryBlocks(blocks, "assistant")
	} else if response.Content != "" {
		// If there were no tool calls but we have content, just add it as text
		UpdateConversationHistoryText(response.Content, "assistant")
	}

	return response, nil
}

// convertMessagesToOpenAIFormat converts messages with content blocks to OpenAI format
func convertMessagesToOpenAIFormat(messages []interface{}) []interface{} {
	result := make([]interface{}, 0, len(messages))

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
	Model string
}

// NewOpenAI creates a new OpenAI provider
func NewOpenAI() *OpenAI {
	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = "gpt-4.1-nano"
	}
	return &OpenAI{Model: model}
}

// Init initializes the OpenAI provider
func (o *OpenAI) Init() error {
	return LoadOpenAIContext()
}
