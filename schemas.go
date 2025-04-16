package main

import (
	_ "embed"
)

//go:embed tools/view.md
var viewToolFile string

//go:embed tools/replace.md
var replaceToolFile string

//go:embed tools/edit.md
var editToolFile string

//go:embed tools/bash.md
var bashToolFile string

//go:embed tools/ls.md
var lsToolFile string

//go:embed tools/find_files.md
var findFilesToolFile string

//go:embed tools/dispatch_agent.md
var dispatchAgentFile string

//go:embed tools/fetch.md
var fetchToolFile string

//go:embed tools/grep.md
var grepToolFile string


// Tool descriptions loaded from markdown files
var (
	ViewToolDescription      = viewToolFile
	ReplaceToolDescription   = replaceToolFile
	EditToolDescription      = editToolFile
	BashToolDescription      = bashToolFile
	LSToolDescription        = lsToolFile
	FindFilesToolDescription = findFilesToolFile
	DispatchAgentDescription = dispatchAgentFile
	FetchToolDescription     = fetchToolFile
	GrepToolDescription      = grepToolFile
)

// JSON schemas for tools
const (
	ViewToolSchema = `{
  "name": "View",
  "description": "Reads a file from the local filesystem. You can access any file directly by using this tool.",
  "parameters": {
    "type": "object",
    "required": ["file_path"],
    "properties": {
      "file_path": {
        "type": "string",
        "description": "The absolute path to the file to read"
      },
      "offset": {
        "type": "number",
        "description": "The line number to start reading from. Only provide if the file is too large to read at once"
      },
      "limit": {
        "type": "number",
        "description": "The number of lines to read. Only provide if the file is too large to read at once."
      }
    }
  }
}`

	ReplaceToolSchema = `{
  "name": "Replace",
  "description": "Write a file to the local filesystem. Overwrites the existing file if there is one.",
  "parameters": {
    "type": "object",
    "required": ["file_path", "content"],
    "properties": {
      "file_path": {
        "type": "string",
        "description": "The absolute path to the file to write (must be absolute, not relative)"
      },
      "content": {
        "type": "string",
        "description": "The content to write to the file"
      }
    }
  }
}`

	EditToolSchema = `{
  "name": "Edit",
  "description": "This is a tool for editing files. For moving or renaming files, use Bash with mv. For larger edits, use Replace.",
  "parameters": {
    "type": "object",
    "required": ["file_path", "old_string", "new_string"],
    "properties": {
      "file_path": {
        "type": "string",
        "description": "The absolute path to the file to modify"
      },
      "old_string": {
        "type": "string",
        "description": "The text to replace"
      },
      "new_string": {
        "type": "string",
        "description": "The text to replace it with"
      },
      "expected_replacements": {
        "type": "number",
        "description": "The expected number of replacements to perform. Defaults to 1 if not specified.",
        "default": 1
      }
    }
  }
}`

	BashToolSchema = `{
  "name": "Bash",
  "description": "Executes a given bash command in a persistent shell session with optional timeout, ensuring proper handling and security measures.",
  "parameters": {
    "type": "object",
    "required": ["command"],
    "properties": {
      "command": {
        "type": "string",
        "description": "The command to execute"
      },
      "timeout": {
        "type": "number",
        "description": "Optional timeout in milliseconds (max 600000)"
      },
      "description": {
        "type": "string",
        "description": "Clear, concise description of what this command does in 5-10 words"
      }
    }
  }
}`

	LSToolSchema = `{
  "name": "LS",
  "description": "Lists files and directories in a given path. The path parameter must be an absolute path, not a relative path.",
  "parameters": {
    "type": "object",
    "required": ["path"],
    "properties": {
      "path": {
        "type": "string",
        "description": "The absolute path to the directory to list (must be absolute, not relative), by default should be current path"
      },
      "ignore": {
        "type": "array",
        "description": "List of glob patterns to ignore",
        "items": {
          "type": "string"
        }
      }
    }
  }
}`

	FindFilesToolSchema = `{
  "name": "FindFilesTool",
  "description": "Fast file pattern matching tool that works with any codebase size.",
  "parameters": {
    "type": "object",
    "required": ["pattern"],
    "properties": {
      "pattern": {
        "type": "string",
        "description": "The glob pattern to match files against"
      },
      "path": {
        "type": "string",
        "description": "The directory to search in. If not specified, the current working directory will be used."
      }
    }
  }
}`

	DispatchAgentSchema = `{
  "name": "dispatch_agent",
  "description": "Launch a new agent that has access to the following tools: View, GlobTool, GrepTool, LS.",
  "parameters": {
    "type": "object",
    "required": ["prompt"],
    "properties": {
      "prompt": {
        "type": "string",
        "description": "The task for the agent to perform"
      }
    }
  }
}`

	FetchToolSchema = `{
  "name": "Fetch",
  "description": "Fetches content from a URL and returns the HTTP response or error message.",
  "parameters": {
    "type": "object",
    "required": ["url"],
    "properties": {
      "url": {
        "type": "string",
        "description": "The URL to fetch content from"
      },
      "headers": {
        "type": "object",
        "description": "Optional HTTP headers to include in the request"
      },
      "method": {
        "type": "string",
        "description": "HTTP method to use (GET, POST, PUT, DELETE, etc.). Defaults to GET if not specified.",
        "enum": ["GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"]
      },
      "data": {
        "type": "string",
        "description": "Optional data to send with the request (for POST, PUT, etc.)"
      }
    }
  }
}`

	GrepToolSchema = `{
  "name": "GrepTool",
  "description": "Fast content search tool that works with any codebase size.",
  "parameters": {
    "type": "object",
    "required": ["pattern"],
    "properties": {
      "pattern": {
        "type": "string",
        "description": "The regular expression pattern to search for in file contents"
      },
      "path": {
        "type": "string",
        "description": "The directory to search in. Defaults to the current working directory."
      },
      "include": {
        "type": "string",
        "description": "File pattern to include in the search (e.g. \"*.js\", \"*.{ts,tsx}\")"
      }
    }
  }
}`
)
