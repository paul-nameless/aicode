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

// Global variables for Claude
var claudeTools []claudeTool // Store the tools loaded at startup

type claudeRequest struct {
	Model       string          `json:"model"`
	Messages    []claudeMessage `json:"messages"`
	System      string          `json:"system,omitempty"`
	Tools       []claudeTool    `json:"tools,omitempty"`
	MaxTokens   int             `json:"max_tokens"`
	Temperature float64         `json:"temperature,omitempty"`
}

type claudeTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type claudeMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
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

type toolUseBlock struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type claudeResponse struct {
	ID         string               `json:"id"`
	Type       string               `json:"type"`
	Role       string               `json:"role"`
	Content    []claudeContentBlock `json:"content"`
	StopReason string               `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// LoadClaudeContext loads tools for Claude
func LoadClaudeContext() error {
	// Load tools
	var err error
	claudeTools, err = loadClaudeTools()
	if err != nil {
		return err
	}

	return nil
}

// loadClaudeTools loads tools using the schema constants defined in tools.go
func loadClaudeTools() ([]claudeTool, error) {
	var toolsList []claudeTool

	// Map of tool names to their schema constants and descriptions
	toolData := map[string]struct {
		Schema      string
		Description string
	}{
		"View":           {ViewToolSchema, ViewToolDescription},
		"Replace":        {ReplaceToolSchema, ReplaceToolDescription},
		"Edit":           {EditToolSchema, EditToolDescription},
		"Bash":           {BashToolSchema, BashToolDescription},
		"Ls":             {LsToolSchema, LsToolDescription},
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
			toolsList = append(toolsList, claudeTool{
				Name:        toolSchema.Name,
				Description: toolInfo.Description, // Use the markdown description
				InputSchema: toolSchema.Parameters,
			})
		} else {
			fmt.Printf("Failed to parse JSON schema for tool %s: %v\n", toolName, err)

			// Fallback to just using the name
			toolsList = append(toolsList, claudeTool{
				Name:        toolName,
				Description: "Tool for " + toolName,
			})
		}
	}

	return toolsList, nil
}

// Inference implements the Llm interface for Claude
func (c *Claude) Inference(messages []interface{}) (InferenceResponse, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return InferenceResponse{}, errors.New("ANTHROPIC_API_KEY environment variable not set")
	}

	// Convert messages to Claude format
	var claudeMessages []claudeMessage
	var systemContent string

	for _, msg := range messages {
		if msgMap, ok := msg.(map[string]interface{}); ok {
			role, _ := msgMap["role"].(string)
			content := msgMap["content"]

			if role == "system" {
				// For system messages, we just append to systemContent
				if contentStr, ok := content.(string); ok {
					systemContent += contentStr + "\n\n"
				}
			} else {
				// For user and assistant messages, we need to handle different formats
				msgContent := convertToClaudeContent(content)
				claudeMessages = append(claudeMessages, claudeMessage{
					Role:    role,
					Content: msgContent,
				})
			}
		}
	}

	// Get base URL from environment variable or use default
	baseURL := os.Getenv("ANTHROPIC_API_URL")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}

	// Start a conversation
	url := baseURL + "/v1/messages"
	reqBody := claudeRequest{
		Model:     c.Model,
		Messages:  claudeMessages,
		System:    systemContent,
		Tools:     claudeTools,
		MaxTokens: 4096,
	}

	// Create request
	bodyBytes, _ := json.Marshal(&reqBody)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return InferenceResponse{}, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return InferenceResponse{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// For debugging
	// if len(body) > 200 {
	// 	fmt.Printf("Claude response (first 200 chars): %s...\n", string(body[:200]))
	// } else {
	// 	fmt.Printf("Claude response: %s\n", string(body))
	// }

	var out claudeResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return InferenceResponse{}, fmt.Errorf("error unmarshaling response: %v\nResponse body: %s", err, string(body))
	}

	if out.Error != nil {
		return InferenceResponse{}, errors.New(out.Error.Message)
	}

	// Accumulate token usage
	c.InputTokens += out.Usage.InputTokens
	c.OutputTokens += out.Usage.OutputTokens

	// Process the response into our unified format
	response := InferenceResponse{
		ToolCalls: []ToolCall{},
	}

	// Extract text and tool calls
	hasToolUse := false
	var toolUseBlocks []ContentBlock

	for _, block := range out.Content {
		if block.Type == "text" {
			response.Content += block.Text
		} else if block.Type == "tool_use" {
			hasToolUse = true

			// Convert to our unified tool call format
			response.ToolCalls = append(response.ToolCalls, ToolCall{
				ID:    block.ID,
				Name:  block.Name,
				Input: block.Input,
			})

			// Collect tool use blocks for conversation history
			toolUseBlocks = append(toolUseBlocks, ContentBlock{
				Type:  "tool_use",
				ID:    block.ID,
				Name:  block.Name,
				Input: block.Input,
			})
		}
	}

	// If there were tool calls, add them to conversation history
	if hasToolUse {
		// Also add the tool use to our conversation history
		responseBlocks := []ContentBlock{
			{
				Type: "text",
				Text: response.Content,
			},
		}

		// Add all tool use blocks
		responseBlocks = append(responseBlocks, toolUseBlocks...)

		// Update conversation history with the blocks
		UpdateConversationHistoryBlocks(responseBlocks, "assistant")
	} else if response.Content != "" {
		// If there were no tool calls but we have content, just add it as text
		UpdateConversationHistoryText(response.Content, "assistant")
	}

	return response, nil
}

// convertToClaudeContent converts our generic content to Claude's specific format
func convertToClaudeContent(content interface{}) interface{} {
	// If it's a string, just return it
	if contentStr, ok := content.(string); ok {
		return contentStr
	}

	// If it's an array of ContentBlock, convert to claudeContentBlock
	if contentBlocks, ok := content.([]ContentBlock); ok {
		claudeBlocks := make([]claudeContentBlock, 0, len(contentBlocks))

		for _, block := range contentBlocks {
			switch block.Type {
			case "text":
				// Only add text blocks if they have actual content
				if block.Text != "" {
					claudeBlocks = append(claudeBlocks, claudeContentBlock{
						Type: "text",
						Text: block.Text,
					})
				}
			case "tool_use":
				claudeBlocks = append(claudeBlocks, claudeContentBlock{
					Type:  "tool_use",
					ID:    block.ID,
					Name:  block.Name,
					Input: block.Input,
				})
			case "tool_result":
				claudeBlocks = append(claudeBlocks, claudeContentBlock{
					Type:      "tool_result",
					ToolUseID: block.ToolUseID,
					Content:   block.Content,
				})
			}
		}

		// If we ended up with no blocks, just return an empty string
		// to avoid Claude API errors
		if len(claudeBlocks) == 0 {
			return ""
		}

		return claudeBlocks
	}

	// Try to handle other formats (arrays of maps, etc.)
	return content
}

// executeTool runs the actual tool implementation
func (c *Claude) executeTool(toolName string, toolInput json.RawMessage) (string, error) {
	fmt.Printf("  tool: %s(%s)\n", toolName, string(toolInput))

	var result string
	var err error

	switch toolName {
	case "GrepTool":
		result, err = ExecuteGrepTool(toolInput)
	case "FindFilesTool":
		result, err = ExecuteFindFilesTool(toolInput)
	case "Bash":
		result, err = ExecuteBashTool(toolInput)
	case "Ls":
		result, err = ExecuteLsTool(toolInput)
	case "View":
		result, err = ExecuteViewTool(toolInput)
	case "Edit":
		result, err = ExecuteEditTool(toolInput)
	case "Fetch":
		result, err = ExecuteFetchTool(toolInput)
	default:
		return "", fmt.Errorf("tool %s is not implemented", toolName)
	}

	if err != nil {
		return "", fmt.Errorf("error executing %s: %v", toolName, err)
	}

	return result, nil
}

// Claude struct implements Llm interface
type Claude struct {
	Model                 string
	InputTokens           int     // Track total input tokens used
	OutputTokens          int     // Track total output tokens used
	InputPricePerMillion  float64 // Price per million input tokens
	OutputPricePerMillion float64 // Price per million output tokens
}

// CalculatePrice calculates the price for Claude API usage
func (c *Claude) CalculatePrice() float64 {
	inputPrice := float64(c.InputTokens) * c.InputPricePerMillion / 1000000.0
	outputPrice := float64(c.OutputTokens) * c.OutputPricePerMillion / 1000000.0
	return inputPrice + outputPrice
}

// NewClaude creates a new Claude provider
func NewClaude() *Claude {
	model := os.Getenv("ANTHROPIC_MODEL")
	if model == "" {
		model = "claude-3-opus-20240229"
	}
	return &Claude{
		Model:                 model,
		InputTokens:           0,
		OutputTokens:          0,
		InputPricePerMillion:  3.0,  // $3 per million input tokens
		OutputPricePerMillion: 15.0, // $15 per million output tokens
	}
}

// Init initializes the Claude provider
func (c *Claude) Init() error {
	return LoadClaudeContext()
}
