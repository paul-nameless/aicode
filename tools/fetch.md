# Fetch

Fetches content from a specified URL using curl and returns the HTTP response or any error messages.

```json
{
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
}
```

## Usage notes:

- The url parameter is required and must be a properly formatted URL
- Optional parameters:
  - headers: Key-value pairs of HTTP headers to include in the request
  - method: HTTP method to use (defaults to GET)
  - data: Request body data to send (for POST, PUT, etc.)
- The tool returns the raw HTTP response body as received from the server
- If an error occurs, the error message is returned instead
- Network timeouts are set to 30 seconds by default
- Maximum response size is limited to prevent excessive output
- This tool is read-only and does not modify any files