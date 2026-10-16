# Personalzuordnung: MCP-Prüfprotokoll

2026-10-16. Lokaler isolierter PostgreSQL16-Compose-Stack, ausschließlich
synthetische Schemas/Daten. Die vorhandenen Warehouse-Runtime-Migrationen und
kanonische Product-Master-Initialisierung vervollständigen die gemeinsame
Testdatenbank für bestehende flexible Abfragen. Keine Produktions-Fachmutationen.

## Vollständige Tests

`go test -count=1 ./...`

```text
ok  	github.com/nbt4/cores-mcp/cmd/server	0.015s
ok  	github.com/nbt4/cores-mcp/cmd/smoke	0.012s
ok  	github.com/nbt4/cores-mcp/internal/authn	0.046s
ok  	github.com/nbt4/cores-mcp/internal/config	0.012s
ok  	github.com/nbt4/cores-mcp/internal/httpx	0.009s
ok  	github.com/nbt4/cores-mcp/internal/mcpserver	35.010s
?   	github.com/nbt4/cores-mcp/internal/store	[no test files]
```

## Race

`go test -race -count=1 ./...`

```text
ok  	github.com/nbt4/cores-mcp/cmd/server	1.056s
ok  	github.com/nbt4/cores-mcp/cmd/smoke	1.047s
ok  	github.com/nbt4/cores-mcp/internal/authn	1.270s
ok  	github.com/nbt4/cores-mcp/internal/config	1.030s
ok  	github.com/nbt4/cores-mcp/internal/httpx	1.050s
ok  	github.com/nbt4/cores-mcp/internal/mcpserver	103.547s
?   	github.com/nbt4/cores-mcp/internal/store	[no test files]
```

`go vet ./...` und `go build -o /tmp/staff-cores-mcp-server ./cmd/server`
beenden sich erfolgreich ohne Ausgabe.

Neue Regressionen prüfen echte User-/Admin-/Aktionsdelegation ohne Finanzgrant,
Dry-run ohne Bestätigung/Idempotenzheader, ownerseitigen Retry nach Rollback,
dauerhaften Replay und Rechteentzug vor Replay. Authentifizierter Streamable-
HTTP-Transport prüft OAuth-Nachfreigabe für alle acht neuen Schreib-/Vorbereitungs-
werkzeuge. Der Katalog zählt477 Tools und83 vollständige OAuth-Deskriptoren.

Synthetische PostgreSQL-Reads prüfen Archivsuche, exakte Zuordnung/Versionen,
Mitarbeiter-/Qualifikationssuche, historische Rollen und Filter aktueller
DB-Adminrechte. Private Kontakt-/Adress-/Bank-/Notizdaten und ungefilterte
Audit-JSONs werden gezielt als Leak-Canaries ausgeschlossen.

Unabhängiges Code-/Root-/Dokumentationsreview ohne verbleibende konkrete Findings.
Vertrag und Rolloutgrenzen: `RENTAL_STAFFING_MCP.md`. Owner-DB-/Kalender-
Regressionsausgaben stehen im gleichnamigen RentalCore-Prüfprotokoll.
