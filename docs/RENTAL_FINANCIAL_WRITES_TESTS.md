# Rental financial write verification

2026-10-17. Dedicated disposable local PostgreSQL16 `_test` database,
with canonical umbrella migrations and Warehouse startup schema supplements.
All CORES_MCP_*TEST_DATABASE_URL variables point to this isolated database.
No production records used or modified.

Actual tests cover all12 priced Rental actions plus12 preparations over HTTP,
separate grants and missing action scopes, commercial job fields including
zero/false, denial before SQL, revoked financial access before warm receipts,
operational job response projection and granted replays. The existing discovery
regression verifies all109 scoped descriptors against registered OAuth grants.
Full names/schemas and tool count remain unchanged (492).

## Targeted regressions

```text
ok  	github.com/nbt4/cores-mcp/internal/mcpserver	1.296s
```

## go test -count=1 ./... -timeout 180s

```text
ok  	github.com/nbt4/cores-mcp/cmd/server	0.020s
ok  	github.com/nbt4/cores-mcp/cmd/smoke	0.013s
ok  	github.com/nbt4/cores-mcp/internal/authn	0.079s
ok  	github.com/nbt4/cores-mcp/internal/config	0.014s
ok  	github.com/nbt4/cores-mcp/internal/httpx	0.011s
ok  	github.com/nbt4/cores-mcp/internal/mcpserver	27.954s
?   	github.com/nbt4/cores-mcp/internal/store	[no test files]
```

## go test -race -count=1 ./... -timeout 240s

```text
ok  	github.com/nbt4/cores-mcp/cmd/server	1.048s
ok  	github.com/nbt4/cores-mcp/cmd/smoke	1.035s
ok  	github.com/nbt4/cores-mcp/internal/authn	1.298s
ok  	github.com/nbt4/cores-mcp/internal/config	1.033s
ok  	github.com/nbt4/cores-mcp/internal/httpx	1.033s
ok  	github.com/nbt4/cores-mcp/internal/mcpserver	135.450s
?   	github.com/nbt4/cores-mcp/internal/store	[no test files]
```

go vet ./... and server build both exited0 with no output.
Independent final review found no remaining P1/P2 issues.
Owner PostgreSQL/race/financial-response/native-boundary evidence is in
RentalCore docs/RENTAL_FINANCIAL_TESTS.md.
