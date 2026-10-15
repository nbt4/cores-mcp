# Prüfprotokoll: Fremdmietkatalog MCP

2026-10-15. Isoliertes lokales PostgreSQL 16, synthetische Daten. Alle fünf
`CORES_MCP_*TEST_DATABASE_URL` sind auf die lokale `_test`-Datenbank gesetzt.
Das Suite-Schema wurde mit den kanonischen Umbrella-Migrationen und den
bestehenden Warehouse-Ergänzungen 039/040/044 sowie ProductMaster-Schema aufgebaut.
Der erste Versuch ohne Suite-Schema scheiterte erwartbar an fehlenden Tabellen;
nach Initialisierung sind sämtliche vorhandenen Querytests ausgeführt und grün.
Die OAuth-Discovery-Erwartung berücksichtigt die zwei zusätzlichen Werkzeuge.

Finale Befehle jeweils Exit 0:

## go test -p 1 -count=1 ./...

```text
ok  	github.com/nbt4/cores-mcp/cmd/server	0.013s
ok  	github.com/nbt4/cores-mcp/cmd/smoke	0.010s
ok  	github.com/nbt4/cores-mcp/internal/authn	0.054s
ok  	github.com/nbt4/cores-mcp/internal/config	0.006s
ok  	github.com/nbt4/cores-mcp/internal/httpx	0.009s
ok  	github.com/nbt4/cores-mcp/internal/mcpserver	10.904s
?   	github.com/nbt4/cores-mcp/internal/store	[no test files]
```

## go vet ./...

```text
(keine Ausgabe)
```

## go build -o /tmp/external-catalog-mcp-server ./cmd/server

```text
(keine Ausgabe)
```

## go test -race -count=1 ./internal/authn ./internal/mcpserver

```text
ok  	github.com/nbt4/cores-mcp/internal/authn	1.252s
ok  	github.com/nbt4/cores-mcp/internal/mcpserver	41.515s
```

## Neue Prüfungen

`TestExternalRentalCatalogDelegationDryRunRetryAndRights` prüft signierte
Benutzer-/Create-/Finanzdelegation, explizite Nullpreise, Weitergabe beider
Preise und Katalogfelder, reine Dry-run-Vorschauen, wiederholbaren Ownerfehler,
dauerhaften Replay mit erneuter Rechteprüfung, abgewiesene Dienstkonten und
fehlende Admin-/Aktions-/Finanzrechte sowie Zeitstempel und Quellen.
`TestExternalRentalCatalogScopesAndSchema` prüft Schema und Scopes.
`TestPositionOAuthDiscoveryAndChallengeAcrossHTTP` prüft die beiden neuen
Werkzeuge über echten HTTP-Transport mit Challenge und beiden Descriptormetadaten.

Unabhängiges Review ohne verbleibenden Blocker. `git diff --check` und
Formatprüfung der geänderten Go-Dateien: Exit 0, keine Ausgabe. Keine neuen
Dependencies, Datenbankmigrationen oder Konfigurationswerte.
