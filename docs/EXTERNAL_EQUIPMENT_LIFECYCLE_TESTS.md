# Fremdmietkatalog-Lebenszyklus: Prüfprotokoll

2026-10-15. Ausschließlich synthetische Daten in lokalem PostgreSQL16,
Datenbank `cores_catalog_lifecycle_test`, eigener Compose-Stack und frisches
Volume mit kanonischen Root-Migrationen. Kein produktiver Fachdatentest.

## Vollständige Go-Prüfung

`go test -p 1 -count=1 ./...` mit den passenden isolierten Testdatenbankvariablen.

```text
ok  	github.com/nbt4/cores-mcp/cmd/server	0.008s
ok  	github.com/nbt4/cores-mcp/cmd/smoke	0.009s
ok  	github.com/nbt4/cores-mcp/internal/authn	0.051s
ok  	github.com/nbt4/cores-mcp/internal/config	0.005s
ok  	github.com/nbt4/cores-mcp/internal/httpx	0.020s
ok  	github.com/nbt4/cores-mcp/internal/mcpserver	19.325s
?   	github.com/nbt4/cores-mcp/internal/store	[no test files]
```

## Race

`go test -race -count=1 ./internal/authn ./internal/mcpserver` mit den passenden isolierten Testdatenbankvariablen.

```text
ok  	github.com/nbt4/cores-mcp/internal/authn	1.260s
ok  	github.com/nbt4/cores-mcp/internal/mcpserver	58.996s
```

`go vet ./...` und Serverbuild erfolgreich. Echte HTTP-OAuth-Descriptor/Challenge- und signierte Owner-Delegationsprüfungen, Retry/Replay und aktuelle Rechte vor jedem Beleg.

Die Tests prüfen Teiländerungen, Nullpreise, exakte Version/Kontextbindung,
Archivierung/Wiederherstellung, offene Jobreferenzen aus beiden Tabellen,
historische Preissnapshots, Zusatzfelder, Lieferanten/Duplikate, Rollback bei
Auditfehlern, dauerhaftes Replay und Rechteentzug. Rental führt echte Zwei-
Verbindungs-Rennen aus: Archivierung gegen beide Referenzquellen/Reopen sowie
Reopen ohne bisherige Referenzen gegen die erste neue Referenz, jeweils beide
Reihenfolgen. Die bestehende echte Child-Versionstrigger-Kette wird gegen den
Owner-Sperrplan geprüft. Schema-Reapplication ist idempotent; SQL-Kopien stimmen.

Unabhängiges Review abgeschlossen ohne verbleibende konkrete Findings.
Gefundene MVCC-Reopen-Lücke, umgekehrte Child-/Parent-Sperrreihenfolge und
Native-Editor-Zusatzfeld-Randfälle behoben und mit Regressionen gesichert.
Suite-CI führt die neuen Owner-/Native-/MCP-PostgreSQL-Tests mit Race aus.
