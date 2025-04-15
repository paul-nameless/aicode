package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type openaiRequest struct {
	Model    string           `json:"model"`
	Messages []openaiMessage  `json:"messages"`
	Tools    []openaiTool     `json:"tools,omitempty"`
}

type openaiTool struct {
	Type       string           `json:"type"`
	Function   openaiFunction   `json:"function"`
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

type toolCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type toolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function toolCallFunction `json:"function"`
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

// Global variables
var conversationHistory []ConversationEntry
var systemMessage openaiMessage // Store the system message (text.md)
var tools []openaiTool // Store the tools loaded at startup

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

// loadSystemMessagesAndTools reads files in the tools/ directory
// and categorizes them as either system message (text.md) or tools
func loadSystemMessagesAndTools() (openaiMessage, []openaiTool, error) {
	var sysMsg openaiMessage
	var toolsList []openaiTool
	
	err := filepath.Walk("tools", func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		
		// Skip directories
		if info.IsDir() {
			return nil
		}
		
		// Read file content
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		
		// Get the base filename
		baseName := filepath.Base(path)
		contentStr := string(content)
		
		// If it's text.md, it's the system message
		if baseName == "text.md" {
			sysMsg = openaiMessage{
				Role:    "system",
				Content: contentStr,
			}
		} else {
			// It's a tool - extract JSON schema if available
			// Extract tool name from filename (remove .md extension)
			toolName := strings.TrimSuffix(baseName, filepath.Ext(baseName))
			
			// Look for JSON schema between triple backticks
			description := contentStr
			
			// Find the JSON schema section
			jsonSchemaStart := strings.Index(contentStr, "```json")
			if jsonSchemaStart != -1 {
				// Find the end of the JSON block
				schemaText := contentStr[jsonSchemaStart+7:]
				endMarkerIdx := strings.Index(schemaText, "```")
				
				if endMarkerIdx != -1 {
					// Extract and parse the JSON schema
					jsonSchema := schemaText[:endMarkerIdx]
					
					// Attempt to parse the JSON
					var toolSchema struct {
						Name        string          `json:"name"`
						Description string          `json:"description"`
						Parameters  json.RawMessage `json:"parameters"`
					}
					
					if err := json.Unmarshal([]byte(jsonSchema), &toolSchema); err == nil {
						fmt.Printf("Successfully parsed JSON schema for tool: %s\n", toolName)
						
						// Successfully parsed the schema
						toolsList = append(toolsList, openaiTool{
							Type: "function",
							Function: openaiFunction{
								Name:        toolSchema.Name,
								Description: toolSchema.Description,
								Parameters:  toolSchema.Parameters,
							},
						})
						return nil // Skip to the next file
					} else {
						fmt.Printf("Failed to parse JSON schema for tool %s: %v\n", toolName, err)
					}
				}
			}
			
			// If JSON parsing failed or no JSON schema found, use the name and description approach
			toolsList = append(toolsList, openaiTool{
				Type: "function",
				Function: openaiFunction{
					Name:        toolName,
					Description: description,
				},
			})
		}
		
		return nil
	})
	
	// If tools directory doesn't exist, just return empty values without error
	if err != nil && !os.IsNotExist(err) {
		return openaiMessage{}, nil, err
	}
	
	return sysMsg, toolsList, nil
}

// LoadSystemMessages loads system messages and tools from the tools directory
// This should be called once at startup
func LoadSystemMessages() error {
	var err error
	systemMessage, tools, err = loadSystemMessagesAndTools()
	return err
}

// AskOpenAI sends a prompt to OpenAI's API and returns the response.
// model: for example, "gpt-3.5-turbo" or "gpt-4"
// prompt: your user input
func AskOpenAI(model, prompt string) (string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return "", errors.New("OPENAI_API_KEY environment variable not set")
	}
	
	// Create messages array starting with the system message
	var messages []openaiMessage
	
	// Add the system message if it's not empty
	if systemMessage.Content != "" {
		messages = append(messages, systemMessage)
	}
	
	// Get conversation history
	historyMessages := getConversationHistory()
	
	// Append history messages
	messages = append(messages, historyMessages...)
	
	// Append the current user prompt
	messages = append(messages, openaiMessage{Role: "user", Content: prompt})
	
	url := "https://api.openai.com/v1/chat/completions"
	reqBody := openaiRequest{
		Model:    model,
		Messages: messages,
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
		// For now, just return a message indicating which tools were called
		var toolResponse strings.Builder
		toolResponse.WriteString("Tool calls detected:\n")
		
		for _, toolCall := range out.Choices[0].Message.ToolCalls {
			toolResponse.WriteString(fmt.Sprintf("- %s with arguments: %s\n", 
				toolCall.Function.Name, 
				string(toolCall.Function.Arguments)))
		}
		
		return toolResponse.String(), nil
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
