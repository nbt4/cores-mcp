# Finanz-Lesezugriff: Prüfprotokoll

2026-10-15, isolierter Worktree aus aktuellem GitHub-main. Die bereits
vorgefundenen uncommitteten Vorarbeiten wurden separat integriert und geprüft;
der ursprüngliche Workspace bleibt unverändert. PostgreSQL 16 läuft lokal
mit ausschließlich synthetischen Daten in einer eigenen `_test`-Datenbank.
Das öffentliche Suite-Schema wurde durch die kanonischen Root-Migrationen und
bestehende Warehouse-039/040/044-/ProductMaster-Ergänzungen initialisiert.

Alle folgenden finalen Befehle endeten mit Exit 0. Vollständige Go-Tests
setzten alle fünf `CORES_MCP_*TEST_DATABASE_URL` auf diese isolierte Datenbank;
Race setzte Auth- und Procurement-Test-URLs. Kein Produktions-Smoke oder
produktiver Finanz-/Geschäftsdatenabruf.

## go test -p 1 -count=1 ./...

```text
ok  	github.com/nbt4/cores-mcp/cmd/server	0.010s
ok  	github.com/nbt4/cores-mcp/cmd/smoke	0.010s
ok  	github.com/nbt4/cores-mcp/internal/authn	0.058s
ok  	github.com/nbt4/cores-mcp/internal/config	0.007s
ok  	github.com/nbt4/cores-mcp/internal/httpx	0.010s
ok  	github.com/nbt4/cores-mcp/internal/mcpserver	17.689s
?   	github.com/nbt4/cores-mcp/internal/store	[no test files]
```

## go vet ./...

```text
(keine Ausgabe)
```

## go build -o /tmp/mcp-financial-server ./cmd/server

```text
(keine Ausgabe)
```

## go test -race -count=1 ./internal/authn ./internal/mcpserver

```text
ok  	github.com/nbt4/cores-mcp/internal/authn	1.192s
ok  	github.com/nbt4/cores-mcp/internal/mcpserver	38.460s
```

## Browser: tatsächliche CSP und kanonische Suite-Primitives

Die Suite-Touch-Primitives wurden mit emuliertem Touchzeiger geprüft;
Desktopvarianten mit feinem Zeiger. Jede Viewportvariante startet mit einer
eigenen Browser-Seite, damit vorherige Tastaturbedienung die Pointer-Emulation
nicht verändert. Durchläufe prüfen unabhängige Read-/Finanz-Deny-Voreinstellungen,
zugängliche Labels/Hilfetexte, alle drei angefragten Dienste, keinen Overflow,
AA-Label-/Logokontrast sowie sichtbaren Tastaturfokus und Select-Bedienung.
Readonly- und No-finance-Varianten sind zusätzlich geprüft. Die generierten
Screenshots für 390/768/1280/1536px wurden geprüft; ausgewählte DE/EN-/Light-/
Dark-Bilder liegen in `docs/screenshots/financial-consent/`.

```text
PASS de dark 390px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS de dark 480px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS de dark 640px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS de dark 768px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS de dark 1024px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS de dark 1280px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS de dark 1536px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS de dark readonly
PASS de dark no-finance
PASS de light 390px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS de light 480px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS de light 640px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS de light 768px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS de light 1024px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS de light 1280px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS de light 1536px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS de light readonly
PASS de light no-finance
PASS en dark 390px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS en dark 480px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS en dark 640px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS en dark 768px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS en dark 1024px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS en dark 1280px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS en dark 1536px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS en dark readonly
PASS en dark no-finance
PASS en light 390px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS en light 480px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS en light 640px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS en light 768px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS en light 1024px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS en light 1280px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS en light 1536px: deny defaults, no overflow, keyboard focus/select, label contrast 8.13
PASS en light readonly
PASS en light no-finance
```

## Review und Nachprüfungen

Ein unabhängiger Review-Agent prüfte Rechte, Query-Inferenz, Rangfolgen,
Aliasse und OAuth. P1: sechs Procurement-Audit-Aliase konnten die Projektion
umgehen; alle Aliase sind jetzt geschützt und werden über echten HTTP-MCP
mit allen sechs Scopekombinationen geprüft. Rental-Preisfelder wurden dem
richtigen Dienst zugeordnet. Keine verbleibenden Findings nach erneuten
PostgreSQL-Tests und Review.

Die Browserprüfung deckte blockierte SVG-Inline-Styles unter der echten CSP auf.
Unveränderte Logopfade nutzen gleichwertige Präsentationsattribute; zusätzlicher
Auth-Test und Browser-Kontrastmessung sichern beide Themes ab. Auth-Tests
einschließlich Race, Vet und Build wurden danach erneut erfolgreich ausgeführt.
Keine Lockerung der CSP oder Testanforderungen.

`git diff --check HEAD`, Goimports/Gofmt und die Suite-Designprüfung sind
erfolgreich, letztere meldet `Designsystem-Prüfung erfolgreich.`.
