package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/goccy/go-yaml"
)

// Config represents the application configuration
type Config struct {
	ApiKeyShell    string   `yaml:"api_key_shell"`
	ApiKey         string   `yaml:"api_key"`
	Model          string   `yaml:"model"`
	InitialPrompt  string   `yaml:"initial_prompt"`
	NonInteractive bool     `yaml:"non_interactive"`
	Debug          bool     `yaml:"debug"`
	Quiet          bool     `yaml:"quiet"`
	EnabledTools   []string `yaml:"enabled_tools"`
	SystemFiles    []string `yaml:"system_files"`
}

// LoadConfig loads configuration from a YAML file
func LoadConfig(configPath string) (Config, error) {
	config := Config{}

	// Default values
	config.Model = "gpt-4.1-nano"
	config.Debug = false
	config.NonInteractive = false
	config.SystemFiles = []string{"AI.md", "CLAUDE.md"}

	// If config file doesn't exist, return default config
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return config, nil
	}

	// Read config file
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return config, err
	}

	// Unmarshal YAML
	if err := yaml.Unmarshal(configData, &config); err != nil {
		return config, err
	}

	// If claude_api_key_shell is set, execute it to get the API key
	if config.ApiKeyShell != "" {
		aptiKey, err := executeShellCommand(config.ApiKeyShell)
		if err != nil {
			return config, errors.New("failed to get API key from shell command: " + err.Error())
		}
		config.ApiKey = strings.TrimSpace(aptiKey)
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
