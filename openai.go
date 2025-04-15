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

// AskOpenAI sends a prompt to OpenAI's API and returns the response.
// model: for example, "gpt-3.5-turbo" or "gpt-4"
// prompt: your user input
func AskOpenAI(model, prompt string) (string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return "", errors.New("OPENAI_API_KEY environment variable not set")
	}
	
	// Load system messages from tools directory
	systemMessages, err := loadSystemMessages()
	if err != nil {
		return "", err
	}
	
	// Create messages array with system messages first, then user prompt
	messages := append(systemMessages, openaiMessage{Role: "user", Content: prompt})
	
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
