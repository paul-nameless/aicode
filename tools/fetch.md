# Fetch

Fetches content from a specified URL using curl and returns the HTTP response or any error messages.

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
