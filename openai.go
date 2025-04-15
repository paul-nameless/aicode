package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
)

type openaiRequest struct {
	Model    string           `json:"model"`
	Messages []openaiMessage  `json:"messages"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiResponse struct {
	Choices []struct {
		Message openaiMessage `json:"message"`
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
var systemMessages []openaiMessage // Store system messages loaded at startup

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

// loadSystemMessages reads all files in the tools/ directory and returns their contents as system messages
func loadSystemMessages() ([]openaiMessage, error) {
	var messages []openaiMessage
	
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
		
		// Add as system message
		messages = append(messages, openaiMessage{
			Role:    "system",
			Content: string(content),
		})
		
		return nil
	})
	
	// If tools directory doesn't exist, just return empty slice without error
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	
	return messages, nil
}

// LoadSystemMessages loads system messages from the tools directory
// This should be called once at startup
func LoadSystemMessages() error {
	var err error
	systemMessages, err = loadSystemMessages()
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
	
	// Create messages array with system messages first
	messages := make([]openaiMessage, len(systemMessages))
	copy(messages, systemMessages)
	
	// Get conversation history from main.go
	historyMessages := getConversationHistory()
	
	// Append history messages
	messages = append(messages, historyMessages...)
	
	// Append the current user prompt
	messages = append(messages, openaiMessage{Role: "user", Content: prompt})
	
	url := "https://api.openai.com/v1/chat/completions"
	reqBody := openaiRequest{
		Model:    model,
		Messages: messages,
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
	return out.Choices[0].Message.Content, nil
}
// UpdateConversationHistory updates the conversation history with a new entry
func UpdateConversationHistory(raw, role string) {
	conversationHistory = append(conversationHistory, ConversationEntry{
		raw:  raw,
		role: role,
	})
}
