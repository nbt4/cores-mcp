# Cores MCP Agent Rules

- Keep database access read-only. Mutations are limited to documented, guided
  business tools that call the owning Core API with `cores:write`, a real suite
  user, complete validation, a final preview, and explicit user confirmation.
  Additive creation is the default; the named P0/P1 and Issue #5 lifecycle
  workflows documented in `docs/TOOL_CATALOG.md` are the only allowed
  update/order/approval/receipt exceptions.
- Never add arbitrary mutation, hard-delete, shell, file, email, or unrestricted
  HTTP tools. Named approval and receipt tools require dedicated scopes,
  optimistic locking, target-Core audit, elevated confirmation and idempotency.
- Expose business capabilities, not arbitrary SQL or unrestricted table access.
- Select fields explicitly and exclude credentials, password hashes, tokens, bank data, private document contents and unnecessary personal data.
- Every tool result must identify its data timestamp and source records.
- Keep tool names and schemas stable because external MCP clients cache them.
- Use the official Model Context Protocol Go SDK and support Streamable HTTP.
- Run `go test ./...`, `go vet ./...`, and `go build ./cmd/server` before release.
- Update `README.md` and `docs/TOOL_CATALOG.md` whenever tools or configuration change.
