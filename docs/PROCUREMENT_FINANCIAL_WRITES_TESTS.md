# Prüfprotokoll: MCP Procurement-Finanzrechte

2026-10-15; frischer Worktree aus GitHub-main. Ausschließlich synthetische
Daten in isoliertem PostgreSQL 16 `_test`, initialisiert aus kanonischen
Root-Migrationen und vorhandenen Warehouse-Ergänzungen. Alle fünf MCP-
Testdatenbank-URLs gesetzt; keine produktiven Geschäftsdaten oder Tokens.
Alle folgenden Befehle endeten mit Exit 0.

## go test -p 1 -count=1 ./...

```text
ok  	github.com/nbt4/cores-mcp/cmd/server	0.014s
ok  	github.com/nbt4/cores-mcp/cmd/smoke	0.012s
ok  	github.com/nbt4/cores-mcp/internal/authn	0.062s
ok  	github.com/nbt4/cores-mcp/internal/config	0.007s
ok  	github.com/nbt4/cores-mcp/internal/httpx	0.010s
ok  	github.com/nbt4/cores-mcp/internal/mcpserver	21.620s
?   	github.com/nbt4/cores-mcp/internal/store	[no test files]
```

## go test -race -count=1 ./internal/authn ./internal/mcpserver -timeout 180s

```text
ok  	github.com/nbt4/cores-mcp/internal/authn	1.267s
ok  	github.com/nbt4/cores-mcp/internal/mcpserver	49.121s
```

## Weitere Prüfungen

- `go vet ./...`: Exit 0, keine Ausgabe.
- `go build ./cmd/server`: Exit 0, keine Ausgabe (temporäres Ausgabebinary).
- `git diff --check`: Exit 0.
- Unabhängiges Review ohne verbleibende Befunde, einschließlich finaler
  Legacy-Consent- und Owner-Klassifikationshärtung.

Echte HTTP-MCP-Tests prüfen 21 Ausführungen und 21 `prepare_*`-Partner vor
Datenzugriff mit Legacy-write, falschen Dienstfinanzgrants und Finanzgrant
ohne Aktionsrecht. Alle sieben erhöhten approve/receive/send-Aktionen fordern
auch bei Legacy-write plus Finanzgrant ihre gesonderte Aktion in OAuth an.
Produkt-URL/Lieferant/Preis erzeugen bedingte Consent-Challenges; reine
Produktmetadaten bleiben ohne Finanzgrant vorbereitet nutzbar.
Ein gespeicherter erfolgreicher Angebotsbeleg wird vor Cachezugriff nach
Finanzrechteentzug verweigert; er bleibt mit gültigem Grant unverändert.
Alle 446 Werkzeugnamen und Eingabeschemata bleiben erhalten; 61 bestehende
Werkzeuge veröffentlichen jetzt ihre einschlägigen OAuth-Deskriptoren.
Owner-Delegationstests prüfen den signierten Procurement-Finanzclaim neben
Nutzer, Adminstatus und Aktionsrecht; dauerhafte Retries prüfen weiterhin
aktuelle Owner-Rechte. PostgreSQL-Prüfungen decken bestehende Versions-,
Kontext-, Requester-, Audit-, Rollback- und Query-Schutzverträge vollständig ab.
