# LS

Lists files and directories in a given path. The path parameter must be an absolute path, not a relative path. You should generally prefer the Glob and Grep tools, if you know which directories to search.

```json
{
  "name": "LS",
  "description": "Lists files and directories in a given path. The path parameter must be an absolute path, not a relative path.",
  "parameters": {
    "type": "object",
    "required": ["path"],
    "properties": {
      "path": {
        "type": "string",
        "description": "The absolute path to the directory to list (must be absolute, not relative)"
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
}
```