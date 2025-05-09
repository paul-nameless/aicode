package main

import (
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

// Config represents the application configuration
type Config struct {
	ApiKeyShell     string   `yaml:"api_key_shell"`
	ApiKey          string   `yaml:"api_key"`
	Model           string   `yaml:"model"`
	InitialPrompt   string   `yaml:"initial_prompt"`
	NonInteractive  bool     `yaml:"non_interactive"`
	Debug           bool     `yaml:"debug"`
	Quiet           bool     `yaml:"quiet"`
	EnabledTools    []string `yaml:"enabled_tools"`
	SystemFiles     []string `yaml:"system_files"`
	BaseUrl         string   `yaml:"base_url"`
	NotifyCmd       string   `yaml:"notify_cmd"`
	ReasoningEffort string   `yaml:"reasoning_effort"`
}

// LoadConfig loads configuration from a YAML file
func LoadConfig(configPath string) (Config, error) {
	config := Config{}

	config.SystemFiles = []string{"AI.md", "CLAUDE.md"}

	// First check if the provided path exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// If path doesn't exist, check in ~/.config/aicode/ directory
		fileName := filepath.Base(configPath)
		configName := strings.TrimSuffix(fileName, filepath.Ext(fileName))

		// Try with yml extension
		altPath := filepath.Join(expandHomeDir("~/.config/aicode"), configName+".yml")
		if _, err := os.Stat(altPath); err == nil {
			configPath = altPath
		}
	}

	// Read config file
	configData, err := os.ReadFile(configPath)
	if err != nil {
		slog.Debug("Failed to read config file:", "error", err)
	}

	// Unmarshal YAML
	if err := yaml.Unmarshal(configData, &config); err != nil {
		slog.Debug("Failed to parse config file:", "error", err)
	}

	// If claude_api_key_shell is set, execute it to get the API key
	if config.ApiKeyShell != "" {
		aptiKey, err := executeShellCommand(config.ApiKeyShell)
		if err != nil {
			return config, errors.New("failed to get API key from shell command: " + err.Error())
		}
		config.ApiKey = strings.TrimSpace(aptiKey)
	}

	if envVal := os.Getenv("OPENAI_API_KEY"); envVal != "" {
		config.ApiKey = envVal
	} else if envVal := os.Getenv("ANTHROPIC_API_KEY"); envVal != "" {
		config.ApiKey = envVal
	}

	if envVal := os.Getenv("OPENAI_MODEL"); envVal != "" {
		config.Model = envVal
	} else if envVal := os.Getenv("ANTHROPIC_MODEL"); envVal != "" {
		config.Model = envVal
	}

	if config.Model == "" {
		config.Model = "claude-3-7-sonnet-latest"
		if os.Getenv("OPENAI_API_KEY") != "" {
			config.Model = "o4-mini"
		}
	}

	if config.BaseUrl == "" {
		config.BaseUrl = os.Getenv("BASE_URL")
	}

	// Set default reasoning effort to medium if not specified
	if config.ReasoningEffort == "" {
		config.ReasoningEffort = "medium"
	}

	if config.ApiKey == "" || config.Model == "" {

		return config, errors.New("API key and model are required")
	}

	return config, nil
}

// executeShellCommand executes a shell command and returns the output
func executeShellCommand(command string) (string, error) {
	cmd := exec.Command("sh", "-c", command)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}
