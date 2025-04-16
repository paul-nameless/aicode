# FindFilesTool

- Fast file pattern matching tool that works with any codebase size
- Supports glob patterns like "*.go" or "src/*.go"
- Returns matching file paths sorted by modification time
- Use this tool when you need to find files by name patterns
- When you are doing an open ended search that may require multiple rounds of globbing and grepping, use the Agent tool instead

```json
{
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
}
```
