# MCP-Prüfprotokoll Mietprodukt-Korrektur

Stand: 2026-10-08, isolierter Worktree `fix/rental-product-logic`.
Die Test-Gates liefen nacheinander, sämtliche Go-Beweisläufe mit `-count=1`.
Nur eine wegwerfbare lokale PostgreSQL-16-Datenbank mit eigenem Volume.

## Vollständige Tests

`CORES_MCP_TEST_DATABASE_URL=<lokale _test-Datenbank> go test -count=1 ./...`
— Exit 0. Echte Ausgabe:

```text
ok  	github.com/nbt4/cores-mcp/cmd/server	0.013s
ok  	github.com/nbt4/cores-mcp/cmd/smoke	0.008s
ok  	github.com/nbt4/cores-mcp/internal/authn	0.036s
ok  	github.com/nbt4/cores-mcp/internal/config	0.005s
ok  	github.com/nbt4/cores-mcp/internal/httpx	0.007s
ok  	github.com/nbt4/cores-mcp/internal/mcpserver	3.888s
?   	github.com/nbt4/cores-mcp/internal/store	[no test files]
```

Die PostgreSQL-Referenzauflösung und der normale Job-Read sind aktiviert.
Die neuen Tests prüfen Finanz-/Aktions-/Adminrechte, persönliche Delegation,
Dry-run, vollständiges Forwarding, Replay/Retry, expliziten Repair, Positions-
Quellen und kundenpreisbasierte Rental-Positionen im Jobkontext. Der OAuth-
HTTP-Test prüft auch den neuen Finanzscope-Descriptor des normalen Job-Reads.
Die tatsächliche atomare Positions-/Kostenanlage, Preisberechnung, Staleness,
Idempotenz und Reparatur werden zusätzlich im RentalCore-Integrationstest
gegen die echte Owner-API geprüft (siehe dortiges Prüfprotokoll).
Andere optional zuschaltbare Integrationstests der nicht berührten Core-
Bereiche wurden nicht zusätzlich aktiviert; keine Tests deaktiviert/geändert,
um Fehler zu verdecken.

## Vet und Build

`go vet ./...` — Exit 0, leere Ausgabe.
`go build ./cmd/server` — Exit 0, leere Ausgabe.

## Testdatenbank-Vervollständigung

Die allgemeine Abfrageprüfung benötigt neben allen Cores-Init-Migrationen
Schema-Ergänzungen aus dem Dienststart. Die anfänglichen fehlenden Tabellen/
Spalten wurden nicht durch Abschalten des Tests umgangen. Nach ausdrücklich
bestätigter Rückfrage wurden ausschließlich in der lokalen `_test`-Datenbank
bestehende Owner-DDLs ergänzt:

- WarehouseCore `internal/handlers/product_master_schema.go`: Produktspalten.
- WarehouseCore `internal/handlers/warehouse_operations_schema.go`: Casespalten.
- WarehouseCore `migrations/044_core_product_links.sql`: bestehende Verknüpfungstabelle.
- WarehouseCore `migrations/040_device_status_and_condition.sql`: `condition_status`.

Es wurden nur bestehende ADD-COLUMN-/CREATE-TABLE-Definitionen übernommen.
Keine Bestandsmigration oder fremder Dienstcode wurde geändert. Beide neuen
Rental-Kostenlink-Migrationen sind lokal geprüft; ihre Inhalte sind identisch.
Der abschließende vollständige Testlauf enthält auch den zuvor roten allgemeinen
Suite-Abfragetest. Produktionsdaten wurden nicht abgefragt oder verändert.
