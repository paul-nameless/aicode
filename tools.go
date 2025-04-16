package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Import the toolCall type from openai.go
type toolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function toolCallFunction `json:"function"`
}

// BashToolParams represents the parameters for the BashTool
type BashToolParams struct {
	Command     string `json:"command"`
	Timeout     int    `json:"timeout,omitempty"`
	Description string `json:"description,omitempty"`
}

type toolCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// GrepToolParams represents the parameters for the GrepTool
type GrepToolParams struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
	Include string `json:"include,omitempty"`
}

// GlobToolParams represents the parameters for the GlobTool
type GlobToolParams struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

// LsToolParams represents the parameters for the LsTool
type LsToolParams struct {
	Path   string   `json:"path"`
	Ignore []string `json:"ignore,omitempty"`
}

// GrepResult represents a single file match result
type GrepResult struct {
	FilePath string    `json:"file_path"`
	Matches  []string  `json:"matches"`
	ModTime  time.Time `json:"-"` // Used for sorting, not exported in JSON
}

// ExecuteGrepTool performs a grep-like search in files using ripgrep (rg)
func ExecuteGrepTool(paramsJSON json.RawMessage) (string, error) {
	fmt.Printf("DEBUG - Raw params received: %s\n", string(paramsJSON))

	// Try multiple approaches to handle potential JSON format issues

	// 1. Try direct unmarshaling first
	var params GrepToolParams
	err := json.Unmarshal(paramsJSON, &params)

	// 2. If that fails, try to handle string-encoded JSON
	if err != nil {
		var strArg string
		if err2 := json.Unmarshal(paramsJSON, &strArg); err2 == nil {
			// We got a string, check if it's JSON
			if strings.HasPrefix(strArg, "{") && strings.HasSuffix(strArg, "}") {
				fmt.Printf("DEBUG - Found string-encoded JSON: %s\n", strArg)
				if err3 := json.Unmarshal([]byte(strArg), &params); err3 == nil {
					// Successfully parsed
					fmt.Printf("DEBUG - Successfully parsed string-encoded JSON\n")
				} else {
					// Both approaches failed
					return "", fmt.Errorf("failed to parse grep tool parameters: %v (from string: %v)", err, err3)
				}
			} else {
				// It's a simple string, assume it's just the pattern
				params.Pattern = strArg
				fmt.Printf("DEBUG - Treating as simple pattern: %s\n", strArg)
			}
		} else {
			// Both approaches failed
			return "", fmt.Errorf("failed to parse grep tool parameters: %v", err)
		}
	}

	// Validate parameters
	if params.Pattern == "" {
		return "", fmt.Errorf("pattern parameter is required")
	}

	fmt.Printf("DEBUG - Using pattern: %s, path: %s, include: %s\n",
		params.Pattern, params.Path, params.Include)

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

	// Execute the ripgrep command
	result, err := executeCommand(rgCmd, 0)
	return result, nil
}

// searchFiles recursively searches files matching the include pattern for content matching the regex pattern
func searchFiles(rootPath string, pattern *regexp.Regexp, includePattern string) ([]GrepResult, error) {
	var results []GrepResult

	// Ensure rootPath exists
	if _, err := os.Stat(rootPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("path does not exist: %s", rootPath)
	}

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip this file/directory but continue walking
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check if file matches include pattern
		if includePattern != "" {
			// Handle glob patterns like "*.{js,ts}"
			if strings.Contains(includePattern, "{") && strings.Contains(includePattern, "}") {
				// Extract patterns between braces
				startIdx := strings.Index(includePattern, "{")
				endIdx := strings.Index(includePattern, "}")

				if startIdx != -1 && endIdx != -1 && startIdx < endIdx {
					prefix := includePattern[:startIdx]
					patterns := strings.Split(includePattern[startIdx+1:endIdx], ",")
					suffix := includePattern[endIdx+1:]

					matched := false
					for _, p := range patterns {
						fullPattern := prefix + p + suffix
						if m, err := filepath.Match(fullPattern, filepath.Base(path)); err == nil && m {
							matched = true
							break
						}
					}

					if !matched {
						return nil // Skip this file
					}
				}
			} else {
				// Simple pattern matching
				match, err := filepath.Match(includePattern, filepath.Base(path))
				if err != nil {
					return nil // Invalid pattern, skip but continue
				}
				if !match {
					return nil // File doesn't match pattern, skip
				}
			}
		}

		// Skip binary files or very large files
		if info.Size() > 10*1024*1024 { // Skip files larger than 10MB
			return nil
		}

		// Read file content
		content, err := os.ReadFile(path)
		if err != nil {
			return nil // Skip this file but continue walking
		}

		// Convert to string and search for matches
		contentStr := string(content)
		matches := pattern.FindAllString(contentStr, -1)

		if len(matches) > 0 {
			// Deduplicate matches
			uniqueMatches := make(map[string]bool)
			for _, match := range matches {
				uniqueMatches[match] = true
			}

			// Convert back to slice
			matchesSlice := make([]string, 0, len(uniqueMatches))
			for match := range uniqueMatches {
				matchesSlice = append(matchesSlice, match)
			}

			// Add to results
			results = append(results, GrepResult{
				FilePath: path,
				Matches:  matchesSlice,
				ModTime:  info.ModTime(),
			})
		}

		return nil
	})

	return results, err
}

// ToolCallResult represents the result of a tool call
type ToolCallResult struct {
	CallID string
	Output string
}

// HandleToolCalls processes tool calls from the LLM response (original function kept for backward compatibility)
func HandleToolCalls(toolCalls []toolCall) (string, error) {
	response, _, err := HandleToolCallsWithResults(toolCalls)
	return response, err
}

// HandleToolCallsWithResults processes tool calls and returns both formatted response and structured results
func HandleToolCallsWithResults(toolCalls []toolCall) (string, []ToolCallResult, error) {
	var toolResponse strings.Builder
	toolResponse.WriteString("Tool calls detected:\n\n")

	var results []ToolCallResult

	for _, toolCall := range toolCalls {
		toolName := toolCall.Function.Name

		toolResponse.WriteString(fmt.Sprintf("Tool: %s\nArguments: %s\n",
			toolName,
			string(toolCall.Function.Arguments)))

		// Execute the tool based on the name
		var result string
		var err error

		switch toolName {
		case "GrepTool":
			result, err = ExecuteGrepTool(toolCall.Function.Arguments)
			if err != nil {
				result = fmt.Sprintf("Error executing GrepTool: %v", err)
			}
		case "FindFilesTool":
			result, err = ExecuteFindFilesTool(toolCall.Function.Arguments)
			if err != nil {
				result = fmt.Sprintf("Error executing GlobTool: %v", err)
			}
		case "Bash":
			result, err = ExecuteBashTool(toolCall.Function.Arguments)
			if err != nil {
				result = fmt.Sprintf("Error executing Bash: %v", err)
			}
		case "LS":
			result, err = ExecuteLsTool(toolCall.Function.Arguments)
			if err != nil {
				result = fmt.Sprintf("Error executing LS: %v", err)
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

		toolResponse.WriteString(fmt.Sprintf("\nResult:\n%s\n\n", result))
	}

	// Print the results to stdout for debugging
	fmt.Println(toolResponse.String())

	return toolResponse.String(), results, nil
}

// executeCommand runs a shell command and returns its output
func executeCommand(command string, timeout int) (string, error) {
	fmt.Printf("DEBUG - Executing command: %s, timeout: %d\n", command, timeout)

	// Set default timeout if not provided
	// Note: Currently we don't implement timeouts in this version
	// but log the value for future implementation
	if timeout > 0 {
		// Ensure timeout doesn't exceed max allowed (10 minutes)
		if timeout > 600000 {
			timeout = 600000
		}
		fmt.Printf("DEBUG - Using timeout value: %d ms\n", timeout)
	}

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

// ExecuteFindFilesTool performs file pattern matching using the fd command with path patterns
func ExecuteFindFilesTool(paramsJSON json.RawMessage) (string, error) {
	fmt.Printf("DEBUG - Raw glob params received: %s\n", string(paramsJSON))

	// Try multiple approaches to handle potential JSON format issues
	var params GlobToolParams
	err := json.Unmarshal(paramsJSON, &params)

	// Try to handle string-encoded JSON if direct unmarshaling fails
	if err != nil {
		var strArg string
		if err2 := json.Unmarshal(paramsJSON, &strArg); err2 == nil {
			// We got a string, check if it's JSON
			if strings.HasPrefix(strArg, "{") && strings.HasSuffix(strArg, "}") {
				fmt.Printf("DEBUG - Found string-encoded JSON: %s\n", strArg)
				if err3 := json.Unmarshal([]byte(strArg), &params); err3 == nil {
					// Successfully parsed
					fmt.Printf("DEBUG - Successfully parsed string-encoded JSON\n")
				} else {
					// Both approaches failed
					return "", fmt.Errorf("failed to parse glob tool parameters: %v (from string: %v)", err, err3)
				}
			} else {
				// It's a simple string, assume it's just the pattern
				params.Pattern = strArg
				fmt.Printf("DEBUG - Treating as simple pattern: %s\n", strArg)
			}
		} else {
			// Both approaches failed
			return "", fmt.Errorf("failed to parse glob tool parameters: %v", err)
		}
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
	result, err := executeCommand(cmd, 0)
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
	fmt.Printf("DEBUG - Raw ls params received: %s\n", string(paramsJSON))

	// Try multiple approaches to handle potential JSON format issues
	var params LsToolParams
	err := json.Unmarshal(paramsJSON, &params)

	// Try to handle string-encoded JSON if direct unmarshaling fails
	if err != nil {
		var strArg string
		if err2 := json.Unmarshal(paramsJSON, &strArg); err2 == nil {
			// We got a string, check if it's JSON
			if strings.HasPrefix(strArg, "{") && strings.HasSuffix(strArg, "}") {
				fmt.Printf("DEBUG - Found string-encoded JSON: %s\n", strArg)
				if err3 := json.Unmarshal([]byte(strArg), &params); err3 == nil {
					// Successfully parsed
					fmt.Printf("DEBUG - Successfully parsed string-encoded JSON\n")
				} else {
					// Both approaches failed
					return "", fmt.Errorf("failed to parse ls tool parameters: %v (from string: %v)", err, err3)
				}
			} else {
				// It's a simple string, assume it's just the path
				params.Path = strArg
				fmt.Printf("DEBUG - Treating as simple path: %s\n", strArg)
			}
		} else {
			// Both approaches failed
			return "", fmt.Errorf("failed to parse ls tool parameters: %v", err)
		}
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
	lsCmd := fmt.Sprintf("ls -la '%s'", strings.ReplaceAll(params.Path, "'", "'\\''"))

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
	result, err := executeCommand(lsCmd, 0)
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
	fmt.Printf("DEBUG - Raw bash params received: %s\n", string(paramsJSON))

	// Try multiple approaches to handle potential JSON format issues
	var params BashToolParams
	err := json.Unmarshal(paramsJSON, &params)

	// Try to handle string-encoded JSON if direct unmarshaling fails
	if err != nil {
		var strArg string
		if err2 := json.Unmarshal(paramsJSON, &strArg); err2 == nil {
			// We got a string, check if it's JSON
			if strings.HasPrefix(strArg, "{") && strings.HasSuffix(strArg, "}") {
				fmt.Printf("DEBUG - Found string-encoded JSON: %s\n", strArg)
				if err3 := json.Unmarshal([]byte(strArg), &params); err3 == nil {
					// Successfully parsed
					fmt.Printf("DEBUG - Successfully parsed string-encoded JSON\n")
				} else {
					// Both approaches failed
					return "", fmt.Errorf("failed to parse bash tool parameters: %v (from string: %v)", err, err3)
				}
			} else {
				// It's a simple string, assume it's just the command
				params.Command = strArg
				fmt.Printf("DEBUG - Treating as simple command: %s\n", strArg)
			}
		} else {
			// Both approaches failed
			return "", fmt.Errorf("failed to parse bash tool parameters: %v", err)
		}
	}

	// Validate parameters
	if params.Command == "" {
		return "", fmt.Errorf("command parameter is required")
	}

	// Execute the command using the extracted function
	return executeCommand(params.Command, params.Timeout)
}

// formatResults formats the grep results as a string
func formatResults(results []GrepResult) string {
	if len(results) == 0 {
		return "No matches found."
	}

	var sb strings.Builder
	totalMatches := 0
	for _, result := range results {
		totalMatches += len(result.Matches)
	}

	sb.WriteString(fmt.Sprintf("Found %d matches in %d files:\n\n", totalMatches, len(results)))

	// Limit the number of files and matches to display to avoid overwhelming output
	maxFilesToShow := 20
	// maxMatchesPerFile := 5

	for i, result := range results {
		if i >= maxFilesToShow {
			remaining := len(results) - maxFilesToShow
			sb.WriteString(fmt.Sprintf("\n... and %d more files not shown\n", remaining))
			break
		}

		sb.WriteString(fmt.Sprintf("%s\n", result.FilePath))

		// if len(result.Matches) <= maxMatchesPerFile {
		// 	// Show all matches
		// 	sb.WriteString(fmt.Sprintf("Matches: %s\n\n", strings.Join(result.Matches, ", ")))
		// } else {
		// 	// Show limited matches with a count
		// 	matches := result.Matches[:maxMatchesPerFile]
		// 	sb.WriteString(fmt.Sprintf("Matches (%d total): %s, ...\n\n",
		// 		len(result.Matches),
		// 		strings.Join(matches, ", ")))
		// }
	}

	return sb.String()
}
