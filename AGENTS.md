# Cores MCP Agent Rules

- The server is read-only. Never add a tool that mutates suite or external data.
- Expose business capabilities, not arbitrary SQL or unrestricted table access.
- Select fields explicitly and exclude credentials, password hashes, tokens, bank data, private document contents and unnecessary personal data.
- Every tool result must identify its data timestamp and source records.
- Keep tool names and schemas stable because external MCP clients cache them.
- Use the official Model Context Protocol Go SDK and support Streamable HTTP.
- Run `go test ./...`, `go vet ./...`, and `go build ./cmd/server` before release.
- Update `README.md` and `docs/TOOL_CATALOG.md` whenever tools or configuration change.
