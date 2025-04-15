# View

Reads a file from the local filesystem. The file_path parameter must be an absolute path, not a relative path. By default, it reads up to 2000 lines starting from the beginning of the file. You can optionally specify a line offset and limit (especially handy for long files), but it's recommended to read the whole file by not providing these parameters. Any lines longer than 2000 characters will be truncated. For image files, the tool will display the image for you. For Jupyter notebooks (.ipynb files), use the ReadNotebook instead.

```json
{
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
}
```