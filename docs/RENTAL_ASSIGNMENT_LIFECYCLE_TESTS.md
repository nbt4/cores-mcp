# Fremdmiet-Zuweisungen: Prüfprotokoll

2026-10-16. Isolierter PostgreSQL16-Compose-Stack mit frischem Volume,
kanonischen Root-Migrationen einschließlich053 und ausschließlich synthetischen
Daten in `cores_assignment_lifecycle_test`. Keine produktiven Fachdatentests.

## Vollständige Go-Tests

`go test -p 1 -count=1 ./...` mit den passenden isolierten Testdatenbankvariablen.

```text
ok  	github.com/nbt4/cores-mcp/cmd/server	0.017s
ok  	github.com/nbt4/cores-mcp/cmd/smoke	0.014s
ok  	github.com/nbt4/cores-mcp/internal/authn	0.059s
ok  	github.com/nbt4/cores-mcp/internal/config	0.007s
ok  	github.com/nbt4/cores-mcp/internal/httpx	0.011s
ok  	github.com/nbt4/cores-mcp/internal/mcpserver	21.324s
?   	github.com/nbt4/cores-mcp/internal/store	[no test files]
```

## Race

`go test -race -count=1 ./internal/authn ./internal/mcpserver` mit den passenden isolierten Testdatenbankvariablen.

```text
ok  	github.com/nbt4/cores-mcp/internal/authn	1.284s
ok  	github.com/nbt4/cores-mcp/internal/mcpserver	87.008s
```

`go vet ./...` und Serverbuild erfolgreich. Unabhängiges Code-/Dachrepo-
Review abgeschlossen ohne verbleibende konkrete Findings. Gefundene Wartezyklen
bei nativen Job-Zeilensperren, nicht atomarer Legacy-Linkprüfung, Unicode-
Requestgrenze und Create/Repair-Konfliktklassifikation behoben und abgesichert.

Die Regressionen prüfen erhaltene Einkaufs-/Kundenpreise trotz Katalogänderung,
Nullpreise, alle drei Versionen, reine Vorschau, gebundene Bestätigung,
Rechteentzug vor dauerhaftem Replay, Archiv-Freeze auch bei Job-Tageswechsel
und Timestamp-Update, explizite Restore-Neuberechnung, präzise NUMERIC-Snapshots,
letzten Joberlös null, Netto-/Bruttogoldens mit Job-/Positionsrabatt und Steuer,
10.000 Emoji, Editorkontext sowie native und MCP-Audit-Rollbacks.

Echte Zwei-Verbindungs-Rennen laufen mit den vorhandenen Job-/Child-Version-
Triggern: Archive gegen Mengenänderung in beiden Reihenfolgen, gehaltene native
Jobrow gegen Owner-Preview, Legacy-Writer gegen explizites Repair und alter
nativer HTTP-PUT über Preisänderung/Archive/Restore. Native delegierte Positions-,
Settings-, Jobformular- und PDF-Finalize-Aufrufe werden vor Datenbankzugriff
verweigert. Startup-/Standalone-/Root-SQL stimmen überein und sind wiederholbar.

MCP prüft signierte Delegation, Dry-run, Fehler/Retry/Replay, fehlende/falsche
Scopes und aktuelle Ownerrechte. Echte HTTP-MCP-Descriptor/Challenge-Tests
sowie DB-Adminentzug, archivierte Get/Search-Discovery und minimale finanzfreie
History-Projektion sind enthalten. Der Katalog umfasst464Tools.
