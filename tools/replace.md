# Replace

Write a file to the local filesystem. Overwrites the existing file if there is one.

```json
{
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
}
```

Before using this tool:

1. Use the ReadFile tool to understand the file's contents and context

2. Directory Verification (only applicable when creating new files):
   - Use the LS tool to verify the parent directory exists and is the correct location