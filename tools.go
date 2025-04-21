package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
)

type toolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function toolCallFunction `json:"function"`
}

type BashToolParams struct {
	Command     string `json:"command"`
	Timeout     int    `json:"timeout,omitempty"`
	Description string `json:"description,omitempty"`
}

type toolCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type GrepParams struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
	Include string `json:"include,omitempty"`
}

type GlobToolParams struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

type LsToolParams struct {
	Path   string   `json:"path"`
	Ignore []string `json:"ignore,omitempty"`
}

func parseToolParams[T any](paramsJSON json.RawMessage, simpleStringField string) (T, error) {
	var params T

	// Clean up the JSON by removing any tab characters that might cause issues
	cleanJSON := strings.ReplaceAll(string(paramsJSON), "\t", "")

	// 1. Try direct unmarshaling first
	err := json.Unmarshal([]byte(cleanJSON), &params)

	// 2. If that fails, try to handle string-encoded JSON
	if err != nil {
		var strArg string
		if err2 := json.Unmarshal(paramsJSON, &strArg); err2 == nil {
			// We got a string, check if it's JSON
			if strings.HasPrefix(strArg, "{") && strings.HasSuffix(strArg, "}") {
				if err3 := json.Unmarshal([]byte(strArg), &params); err3 == nil {
					// Successfully parsed
				} else {
					// Both approaches failed
					return params, fmt.Errorf("failed to parse tool parameters: %v (from string: %v)", err, err3)
				}
			} else if simpleStringField != "" {
				// It's a simple string, set it to the specified field
				fmt.Printf("DEBUG - Treating as simple value for field %s: %s\n", simpleStringField, strArg)

				// Use reflection to set the field
				v := reflect.ValueOf(&params).Elem()
				f := v.FieldByName(simpleStringField)
				if f.IsValid() && f.CanSet() && f.Kind() == reflect.String {
					f.SetString(strArg)
				} else {
					return params, fmt.Errorf("invalid simple string field: %s", simpleStringField)
				}
			}
		} else {
			// Both approaches failed
			return params, fmt.Errorf("failed to parse tool parameters: %v", err)
		}
	}

	return params, nil
}

func ExecuteGrep(paramsJSON json.RawMessage) (string, error) {
	params, err := parseToolParams[GrepParams](paramsJSON, "Pattern")
	if err != nil {
		return "", fmt.Errorf("failed to parse grep tool parameters: %v", err)
	}

	// Validate parameters
	if params.Pattern == "" {
		return "", fmt.Errorf("pattern parameter is required")
	}

	// Default path to current directory if not provided
	if params.Path == "" {
		var err error
		params.Path, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current directory: %v", err)
		}
	}

	// Build the ripgrep command
	rgCmd := fmt.Sprintf("rg --pretty --smart-case '%s'",
		strings.ReplaceAll(params.Pattern, "'", "'\\''")) // Escape single quotes

	// Add path if specified
	if params.Path != "" {
		rgCmd += fmt.Sprintf(" '%s'", strings.ReplaceAll(params.Path, "'", "'\\''"))
	}

	// Add include pattern if specified
	if params.Include != "" {
		rgCmd += fmt.Sprintf(" --glob '%s'", strings.ReplaceAll(params.Include, "'", "'\\''"))
	}

	// Clean up the command by removing any tab characters that might cause issues
	rgCmd = strings.ReplaceAll(rgCmd, "\t", "")

	// Execute the ripgrep command
	result, _ := executeCommand(rgCmd)
	return result, nil
}

type FetchToolParams struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Method  string            `json:"method,omitempty"`
	Data    string            `json:"data,omitempty"`
}

type EditToolParams struct {
	FilePath             string `json:"file_path"`
	OldString            string `json:"old_string"`
	NewString            string `json:"new_string"`
	ExpectedReplacements int    `json:"expected_replacements,omitempty"`
}

type ReplaceToolParams struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

type ToolCallResult struct {
	CallID string
	Output string
}

func HandleToolCalls(toolCalls []ToolCall, config Config) (string, error) {
	response, _, err := HandleToolCallsWithResults(toolCalls, config)
	return response, err
}

func HandleToolCallsWithResults(toolCalls []ToolCall, config Config) (string, []ToolCallResult, error) {
	var toolResponse strings.Builder

	var results []ToolCallResult

	for _, toolCall := range toolCalls {
		toolName := toolCall.Name

		fmt.Printf("tool: %s(%s)\n", toolName, string(toolCall.Input))
		// Only print tool information if debug mode is enabled
		// if debugMode {
		// 	fmt.Printf("tool: %s(%s)\n", toolName, string(toolCall.Input))
		// }

		// Get the global config to check enabled tools
		// Check if the tool is enabled
		toolEnabled := false
		if debugMode {
			// In debug mode, access global config directly for tools
			for _, enabledTool := range config.EnabledTools {
				if enabledTool == toolName {
					toolEnabled = true
					break
				}
			}
		} else {
			// Not in debug mode, assume all tools are enabled
			toolEnabled = true
		}

		if !toolEnabled {
			result := fmt.Sprintf("Tool %s is not enabled. Use the --tools flag to enable it.", toolName)
			results = append(results, ToolCallResult{
				CallID: toolCall.ID,
				Output: result,
			})
			toolResponse.WriteString(fmt.Sprintf("%s\n", result))
			continue
		}

		// Execute the tool based on the name
		var result string
		var err error

		switch toolName {
		case "Grep":
			result, err = ExecuteGrep(toolCall.Input)
			if err != nil {
				result = fmt.Sprintf("Error executing Grep: %v", err)
			}
		case "FindFiles":
			result, err = ExecuteFindFiles(toolCall.Input)
			if err != nil {
				result = fmt.Sprintf("Error executing FindFiles: %v", err)
			}
		case "Bash":
			result, err = ExecuteBashTool(toolCall.Input)
			if err != nil {
				result = fmt.Sprintf("Error executing Bash: %v", err)
			}
		case "Ls":
			result, err = ExecuteLsTool(toolCall.Input)
			if err != nil {
				result = fmt.Sprintf("Error executing Ls: %v", err)
			}
		case "View":
			result, err = ExecuteViewTool(toolCall.Input)
			if err != nil {
				result = fmt.Sprintf("Error executing View: %v", err)
			}
		case "Edit":
			result, err = ExecuteEditTool(toolCall.Input)
			if err != nil {
				result = fmt.Sprintf("Error executing Edit: %v", err)
			}
		case "Replace":
			result, err = ExecuteReplaceTool(toolCall.Input)
			if err != nil {
				result = fmt.Sprintf("Error executing Replace: %v", err)
			}
		case "Fetch":
			result, err = ExecuteFetchTool(toolCall.Input)
			if err != nil {
				result = fmt.Sprintf("Error executing Fetch: %v", err)
			}
		case "DispatchAgent":
			result, err = ExecuteDispatchAgentTool(toolCall.Input)
			if err != nil {
				result = fmt.Sprintf("Error executing DispatchAgent: %v", err)
			}
		default:
			// For now, other tools aren't implemented yet
			result = fmt.Sprintf("Tool %s is not implemented yet.", toolName)
		}

		// Store the result for later use in follow-up requests
		results = append(results, ToolCallResult{
			CallID: toolCall.ID,
			Output: result,
		})

		if result != "" {
			toolResponse.WriteString(fmt.Sprintf("%s\n", result))
		}
	}

	// Only print debugging info if debug mode is enabled
	if debugMode {
		fmt.Println(toolResponse.String())
	}

	return toolResponse.String(), results, nil
}

func executeCommand(command string) (string, error) {
	// Create a command to execute the bash command
	cmd := exec.Command("bash", "-c", command)

	// Set up output capture
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("Error executing command: %v\nOutput: %s", err, string(output)), nil
	}

	// Truncate output if it exceeds 30000 characters
	result := string(output)
	if len(result) > 30000 {
		result = result[:30000] + "\n... [Output truncated due to size]"
	}

	return result, nil
}

// ExecuteFindFiles performs file pattern matching using the fd command with path patterns
func ExecuteFindFiles(paramsJSON json.RawMessage) (string, error) {
	params, err := parseToolParams[GlobToolParams](paramsJSON, "Pattern")
	if err != nil {
		return "", fmt.Errorf("failed to parse glob tool parameters: %v", err)
	}

	// Validate parameters
	if params.Pattern == "" {
		return "", fmt.Errorf("pattern parameter is required")
	}

	// Default path to current directory if not provided
	if params.Path == "" {
		var err error
		params.Path, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current directory: %v", err)
		}
	}

	// Escape the pattern for shell use
	escapedPattern := strings.ReplaceAll(params.Pattern, "'", "'\\''")
	escapedPath := strings.ReplaceAll(params.Path, "'", "'\\''")

	// Construct the fd command with glob pattern
	cmd := fmt.Sprintf("fd --glob '%s' '%s'",
		escapedPattern, escapedPath)

	// Execute the command
	result, err := executeCommand(cmd)
	if err != nil {
		return "", fmt.Errorf("error executing glob command: %v", err)
	}

	// Format the results
	if result == "" {
		return "No files found matching the pattern.", nil
	}

	return result, nil

	// 	// Split the result into lines
	// 	lines := strings.Split(strings.TrimSpace(result), "\n")

	// 	var sb strings.Builder
	// 	sb.WriteString(fmt.Sprintf("Found %d files matching pattern '%s':\n\n", len(lines), params.Pattern))

	// 	// Limit the number of files to display
	// 	maxFilesToShow := 100
	// 	for i, line := range lines {
	// 		if i >= maxFilesToShow {
	// 			remaining := len(lines) - maxFilesToShow
	// 			sb.WriteString(fmt.Sprintf("\n... and %d more files not shown\n", remaining))
	// 			break
	// 		}
	// 		sb.WriteString(fmt.Sprintf("%s\n", line))
	// 	}

	// return sb.String(), nil
}

// ExecuteLsTool lists files and directories in a given path using the shell ls command
func ExecuteLsTool(paramsJSON json.RawMessage) (string, error) {
	params, err := parseToolParams[LsToolParams](paramsJSON, "Path")
	if err != nil {
		return "", fmt.Errorf("failed to parse ls tool parameters: %v", err)
	}

	// Use current directory if path is not specified
	if params.Path == "" || params.Path == "/" {
		var err error
		params.Path, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current directory: %v", err)
		}
	}

	// Check if the path exists
	_, err = os.Stat(params.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("Path does not exist: %s", params.Path), nil
		}
		return "", fmt.Errorf("error accessing path: %v", err)
	}

	// Build the ls command with options
	lsCmd := fmt.Sprintf("ls -a '%s'", strings.ReplaceAll(params.Path, "'", "'\\''"))

	// Add ignore patterns if specified
	if len(params.Ignore) > 0 {
		// Create a grep pattern to exclude files
		grepExclude := ""
		for i, pattern := range params.Ignore {
			if i > 0 {
				grepExclude += " -e "
			}
			// Escape the pattern for grep
			escapedPattern := strings.ReplaceAll(pattern, "'", "'\\''")
			grepExclude += fmt.Sprintf("'%s'", escapedPattern)
		}

		// Pipe ls output through grep -v to exclude matching files
		if grepExclude != "" {
			lsCmd += fmt.Sprintf(" | grep -v %s", grepExclude)
		}
	}

	// Execute the command
	result, err := executeCommand(lsCmd)
	if err != nil {
		return "", fmt.Errorf("error executing ls command: %v", err)
	}

	// Format the output
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Directory: %s\n\n", params.Path))
	sb.WriteString(result)

	return sb.String(), nil
}

// ExecuteBashTool executes a bash command in a persistent shell session
func ExecuteBashTool(paramsJSON json.RawMessage) (string, error) {
	params, err := parseToolParams[BashToolParams](paramsJSON, "Command")
	if err != nil {
		return "", fmt.Errorf("failed to parse bash tool parameters: %v", err)
	}

	// Validate parameters
	if params.Command == "" {
		return "", fmt.Errorf("command parameter is required")
	}

	// Execute the command using the extracted function
	return executeCommand(params.Command)
}

// ViewToolParams represents the parameters for the ViewTool
type ViewToolParams struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

// ExecuteViewTool reads a file from the filesystem with optional offset and limit
func ExecuteViewTool(paramsJSON json.RawMessage) (string, error) {
	params, err := parseToolParams[ViewToolParams](paramsJSON, "FilePath")
	if err != nil {
		return "", fmt.Errorf("failed to parse view tool parameters: %v", err)
	}

	// Validate parameters
	if params.FilePath == "" {
		return "", fmt.Errorf("file_path parameter is required")
	}

	// Check if the file exists
	fileInfo, err := os.Stat(params.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("File does not exist: %s", params.FilePath), nil
		}
		return "", fmt.Errorf("error accessing file: %v", err)
	}

	// Check if it's a directory
	if fileInfo.IsDir() {
		return fmt.Sprintf("%s is a directory, not a file", params.FilePath), nil
	}

	// Check if it's an image file
	if isImageFile(params.FilePath) {
		return fmt.Sprintf("Image file detected: %s\nPlease use an image viewer to view this file.", params.FilePath), nil
	}

	// Set default limit if not provided
	if params.Limit <= 0 {
		params.Limit = 2000 // Default to 2000 lines
	}

	// Escape the file path for shell use
	escapedPath := strings.ReplaceAll(params.FilePath, "'", "'\\''")

	var cmd string
	if params.Offset > 0 {
		// Use tail and head to get lines starting from offset with limit
		cmd = fmt.Sprintf("tail -n +%d '%s' | head -n %d",
			params.Offset, escapedPath, params.Limit)
	} else {
		// Just use head to get the first N lines
		cmd = fmt.Sprintf("head -n %d '%s'", params.Limit, escapedPath)
	}

	// Execute the command
	result, err := executeCommand(cmd)
	if err != nil {
		return "", fmt.Errorf("error reading file: %v", err)
	}

	return result, nil
}

// ExecuteFetchTool fetches content from a URL using curl
func ExecuteFetchTool(paramsJSON json.RawMessage) (string, error) {
	params, err := parseToolParams[FetchToolParams](paramsJSON, "URL")
	if err != nil {
		return "", fmt.Errorf("failed to parse fetch tool parameters: %v", err)
	}

	// Validate parameters
	if params.URL == "" {
		return "", fmt.Errorf("url parameter is required")
	}

	// Build the curl command
	curlCmd := "curl -s"

	// Add HTTP method if specified
	if params.Method != "" {
		curlCmd += fmt.Sprintf(" -X %s", params.Method)
	}

	// Add headers if specified
	for key, value := range params.Headers {
		curlCmd += fmt.Sprintf(" -H '%s: %s'",
			strings.ReplaceAll(key, "'", "'\\''"),
			strings.ReplaceAll(value, "'", "'\\''"))
	}

	// Add data if specified for POST, PUT, etc.
	if params.Data != "" {
		curlCmd += fmt.Sprintf(" -d '%s'", strings.ReplaceAll(params.Data, "'", "'\\''"))
	}

	// Add URL
	curlCmd += fmt.Sprintf(" '%s'", strings.ReplaceAll(params.URL, "'", "'\\''"))

	// Execute the curl command
	result, err := executeCommand(curlCmd)
	if err != nil {
		return "", fmt.Errorf("error executing fetch command: %v", err)
	}

	return result, nil
}

// isImageFile checks if a file is an image based on its extension
func isImageFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	imageExts := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
		".bmp": true, ".tiff": true, ".webp": true, ".svg": true,
	}
	return imageExts[ext]
}

// ExecuteReplaceTool writes content to a file, overwriting it if it exists
func ExecuteReplaceTool(paramsJSON json.RawMessage) (string, error) {
	params, err := parseToolParams[ReplaceToolParams](paramsJSON, "FilePath")
	if err != nil {
		return "", fmt.Errorf("failed to parse replace tool parameters: %v", err)
	}

	// Validate parameters
	if params.FilePath == "" {
		return "", fmt.Errorf("file_path parameter is required")
	}
	if params.Content == "" {
		return "", fmt.Errorf("content parameter is required")
	}

	// Check if file exists to determine if we're creating or overwriting
	fileExists := true
	fileInfo, err := os.Stat(params.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			fileExists = false
		} else {
			return "", fmt.Errorf("error accessing file: %v", err)
		}
	} else if fileInfo.IsDir() {
		return "", fmt.Errorf("%s is a directory, not a file", params.FilePath)
	}

	// Write the content to the file
	if err := os.WriteFile(params.FilePath, []byte(params.Content), 0644); err != nil {
		return "", fmt.Errorf("error writing to file: %v", err)
	}

	if fileExists {
		return fmt.Sprintf("Successfully overwrote file: %s", params.FilePath), nil
	}
	return fmt.Sprintf("Successfully created file: %s", params.FilePath), nil
}

// ExecuteEditTool edits a file by replacing old_string with new_string
func ExecuteEditTool(paramsJSON json.RawMessage) (string, error) {
	// For EditTool, we don't support simple string parameters
	params, err := parseToolParams[EditToolParams](paramsJSON, "")
	if err != nil {
		return "", fmt.Errorf("failed to parse edit tool parameters: %v", err)
	}

	// Validate parameters
	if params.FilePath == "" {
		return "", fmt.Errorf("file_path parameter is required")
	}

	// For creating a new file, old_string can be empty
	if params.NewString == "" {
		return "", fmt.Errorf("new_string parameter is required")
	}

	// Check if the file exists (for edits of existing files)
	fileInfo, err := os.Stat(params.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			// If old_string is empty, create a new file
			if params.OldString == "" {
				// Make sure the directory exists
				dir := filepath.Dir(params.FilePath)
				if err := os.MkdirAll(dir, 0755); err != nil {
					return "", fmt.Errorf("failed to create directory %s: %v", dir, err)
				}

				// Write the new file
				if err := os.WriteFile(params.FilePath, []byte(params.NewString), 0644); err != nil {
					return "", fmt.Errorf("failed to create file: %v", err)
				}

				return fmt.Sprintf("Created new file: %s", params.FilePath), nil
			}
			return "", fmt.Errorf("file does not exist: %s", params.FilePath)
		}
		return "", fmt.Errorf("error accessing file: %v", err)
	}

	// Check if it's a directory
	if fileInfo.IsDir() {
		return "", fmt.Errorf("%s is a directory, not a file", params.FilePath)
	}

	// Read the file content
	content, err := os.ReadFile(params.FilePath)
	if err != nil {
		return "", fmt.Errorf("error reading file: %v", err)
	}

	// Default expected replacements is 1 if not specified
	expectedReplacements := 1
	if params.ExpectedReplacements > 0 {
		expectedReplacements = params.ExpectedReplacements
	}

	// Perform the replacement
	contentStr := string(content)
	count := strings.Count(contentStr, params.OldString)

	// Check that we're replacing exactly the expected number of occurrences
	if count != expectedReplacements {
		return "", fmt.Errorf("found %d occurrences of the old string, but expected %d", count, expectedReplacements)
	}

	// Replace the old string with the new string
	newContent := strings.Replace(contentStr, params.OldString, params.NewString, expectedReplacements)

	// Write the updated content back to the file
	if err := os.WriteFile(params.FilePath, []byte(newContent), fileInfo.Mode()); err != nil {
		return "", fmt.Errorf("error writing to file: %v", err)
	}

	return fmt.Sprintf("Successfully edited file %s, replacing %d occurrence(s) of old_string with new_string.", params.FilePath, expectedReplacements), nil
}

// DispatchAgentToolParams represents the parameters for the DispatchAgent tool
type DispatchAgentToolParams struct {
	Prompt string `json:"prompt"`
}

// ExecuteDispatchAgentTool launches a new instance of this application with the same configuration
// to process a prompt and return the summarized response
func ExecuteDispatchAgentTool(paramsJSON json.RawMessage) (string, error) {
	params, err := parseToolParams[DispatchAgentToolParams](paramsJSON, "Prompt")
	if err != nil {
		return "", fmt.Errorf("failed to parse DispatchAgent tool parameters: %v", err)
	}

	// Validate parameters
	if params.Prompt == "" {
		return "", fmt.Errorf("prompt parameter is required")
	}

	// Get the path to the current executable
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to get executable path: %v", err)
	}

	// Get dispatch agent tools from DefaultDispatchAgentTools
	// Only include the tools from DefaultDispatchAgentTools that are also enabled in config
	var dispatchAgentTools []string
	dispatchAgentTools = append(dispatchAgentTools, DefaultDispatchAgentTools...)

	// Build the tools parameter string
	toolsParam := strings.Join(dispatchAgentTools, ",")

	// Create command to run the same executable with the prompt and tools parameter
	cmd := exec.Command(execPath, "-q", "-n", "-tools", toolsParam, params.Prompt)

	// Set environment variables
	cmd.Env = os.Environ()

	// Capture stdout
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("error executing command: %v", err)
	}

	// Return the output (which should be just the response in quiet mode)
	fmt.Printf("Simulacrum output:\n%s\n", string(output))
	fmt.Printf("===\n")
	return string(output), nil
}
