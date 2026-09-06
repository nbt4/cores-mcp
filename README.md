# Cores MCP

## Berechtigungen ab 1.1.1

OAuth-Access-Tokens werden bei jeder Anfrage gegen den aktiven Kontostatus geprüft;
Sperren wirken auch vor Ablauf eines bestehenden Tokens. Die Prüfung gilt ebenfalls
beim Einlösen von Autorisierungscodes und Refresh-Tokens.

Alle Planner-Tools und Planner-Anteile von Suche, Aktivitäten, Qualitätsprüfungen,
Kennzahlen und flexiblen Abfragen berücksichtigen `planner_members`. Administratoren
umgehen diese Mitgliedschaftsprüfung nicht. Statische Maschinentokens und der
anonyme Entwicklungsmodus erhalten keine privaten Planner-Daten; für Planner
ist ein persönlicher OAuth-Zugang mit passenden Mitgliedschaften erforderlich.
Andere freigegebene operative Daten bleiben über den Scope `cores:read` verfügbar.

Die Isolation wird über echten MCP-HTTP-Transport und PostgreSQL getestet:
`CORES_MCP_AUTH_TEST_DATABASE_URL=postgres://.../cores_test go test -race ./internal/mcpserver`.
Die Testdatenbank muss isoliert sein und auf `_test` enden. Der Test erzeugt und
entfernt ausschließlich sein eigenes Schema.

Cores MCP bindet die gesamte Cores Suite als sicheren MCP-Server an ChatGPT, Claude, Codex und andere MCP-fähige Agents an. Der Chat bleibt beim jeweiligen KI-Anbieter; Cores stellt nur kontrollierte Werkzeuge und Kontext bereit.

Der Server bietet 59 lesende fachliche Tools, fünf wiederverwendbare Analyse-Prompts und dokumentierbare Knowledge-Ressourcen für RentalCore, WarehouseCore, PlannerCore und ProcurementCore. Bei `MCP_ENABLE_WRITES=true` kommen vier geführte Anlage-Tools für ProcurementCore-Produkte und RentalCore-Jobs hinzu. Es gibt bewusst kein beliebiges SQL und keine Tools zum Ändern, Löschen, Bestellen oder Freigeben.

## Geführtes Anlegen mit Rückfragen

- `procurement.products.prepare_create` analysiert optional einen Produktlink, führt erkannte und explizit genannte Werte zusammen, prüft Duplikate, schlägt Kategorien/Lieferanten vor und liefert `questions_for_user` für alle fehlenden Angaben.
- `procurement.products.create` legt erst an, wenn Pflichtfelder eindeutig sind, empfohlene Lücken ausgefüllt oder ausdrücklich akzeptiert wurden und `confirm_creation=true` nach einer finalen Vorschau gesetzt ist.
- `rental.jobs.prepare_create` löst Kunden, Status, Jobkategorie und Location gegen die Live-Daten auf. Mehrdeutige Namen werden nie geraten, sondern als Auswahl zurückgegeben.
- `rental.jobs.create` verlangt vollständige Kerndaten und dieselbe ausdrückliche Bestätigung.

Produktcodes werden nicht mehr als selbsterklärend behandelt. Die Produktsuche liefert Kategorie, Beschreibung, Parameter, Attribute und Warehouse-Verknüpfung als `semantic_context`. Varianten ohne Leer- oder Sonderzeichen werden gemeinsam gefunden (`PDU 3`, `PDU3`, `PDU-3`). Bekannte Fachkürzel werden erklärt; bei `PDU 3` weist MCP beispielsweise auf „Power Distribution Unit / Stromverteiler bzw. Mehrfachsteckdose“ hin und markiert „wahrscheinlich drei Steckplätze“ ausdrücklich als zu prüfende Inferenz.

## Typische Fragen

- „Haben wir bis Ende Oktober genug Coupler? Berücksichtige parallele Jobs und 15 % Reserve.“
- „Wäge Superclamps, Selflock Hooks und Standardschellen anhand unseres Bestands, Bedarfs und der Beschaffungslage ab.“
- „Wir wollen auf ein Akkusystem festlegen. Vergleiche Einhell, Makita und Hilti im Kontext unseres Bestands, der Nutzung, Wartung und Lieferanten.“
- „Welche Jobs, Defekte, Wartungen, Aufgaben oder Lieferungen sind gerade kritisch?“
- „Welche Datenlücken verhindern eine belastbare Entscheidung?“

## Schnittstelle

| Zweck | Pfad |
|---|---|
| MCP Streamable HTTP | `/mcp` |
| OAuth Protected Resource Metadata | `/.well-known/oauth-protected-resource/mcp` |
| OAuth Authorization Server Metadata | `/.well-known/oauth-authorization-server` |
| Dynamic Client Registration | `/oauth/register` |
| Authorization / Token | `/oauth/authorize`, `/oauth/token` |
| Betriebsstatus | `/health`, `/ready` |
| Maschinenlesbare Kurzdoku | `/mcp/docs` |

## Frei kombinierbare Abfragen

Neben den festen fachlichen Tools stellt MCP eine sichere, deklarative Abfrageschicht bereit:

- `cores.query.catalog` beschreibt 19 freigegebene Entitäten, ihre Feldtypen und 20 geprüfte Cross-Core-Beziehungen.
- `cores.query.records` führt bis zu acht parametrisierte Abfragen in einem Call aus und kann die Ergebnisse über Katalogbeziehungen oder explizit freigegebene Felder verknüpfen.
- `cores.query.aggregate` gruppiert und aggregiert mit `count`, `count_distinct`, `sum`, `avg`, `min` und `max`.

Damit lassen sich beispielsweise Jobs → Materialbedarf → Lagerprodukte → Beschaffungsprodukte → Angebote oder Geräte → Defekte → Wartung in einem strukturierten Aufruf untersuchen. Entitäten, Felder, Operatoren und Sortierung werden gegen serverseitige Whitelists geprüft; Werte bleiben SQL-Parameter.

Die aktuelle Implementierung nutzt das offizielle Go SDK und den aktuellen MCP-Transport „Streamable HTTP“. Sie ist stateless und damit horizontal skalierbar; OAuth-Clientregistrierungen liegen in einem persistenten Volume.

## An ChatGPT, Claude und Agents anbinden

Die öffentliche MCP-URL ist:

```text
https://<cores-domain>/mcp
```

In ChatGPT wird sie als benutzerdefinierte MCP-App/Plugin, in Claude als Custom Connector eingetragen. Beim ersten Verbinden registriert sich der Client dynamisch. Fehlt die Cores-Sitzung, führt der Flow durch das zentrale Cores-Login und automatisch zurück in den OAuth-Dialog. Die dortige, im Suite-Design dargestellte Freigabe zeigt `cores:read` und bei aktivierter Anlagefunktion zusätzlich `cores:write` verständlich an. Bereits verbundene Clients müssen neu verbunden werden, damit sie den neuen Scope erhalten. Ihre Content Security Policy erlaubt den POST an die konfigurierte öffentliche MCP-Origin und den anschließenden Redirect ausschließlich an die bereits validierte, registrierte Callback-Origin des Clients. Der Nutzer muss ein aktives Cores-Konto besitzen.

Für CI-Agents kann alternativ `MCP_AUTH_MODE=bearer` oder zusätzlich `MCP_STATIC_TOKENS=agent-name:secret` genutzt werden. Secrets niemals in Git einchecken.

Ausführliche Anleitungen: [docs/CONNECTORS.md](docs/CONNECTORS.md). Vollständiger Werkzeugkatalog: [docs/TOOL_CATALOG.md](docs/TOOL_CATALOG.md).

## Lokal starten

Voraussetzungen: Go 1.25 und Zugriff auf die gemeinsame PostgreSQL-Datenbank.

```bash
cp .env.example .env
set -a
. ./.env
set +a
go run ./cmd/server
```

Für eine isolierte lokale Prüfung darf `MCP_AUTH_MODE=none` gesetzt werden. Das darf nicht öffentlich betrieben werden.

```bash
MCP_ENDPOINT=http://127.0.0.1:8090/mcp make smoke
```

Der Smoke-Test verbindet einen echten MCP-Client, listet alle Tools und ruft jedes Tool mit repräsentativen Eingaben auf.

## Konfiguration

| Variable | Standard | Bedeutung |
|---|---|---|
| `MCP_PUBLIC_URL` | `http://localhost:8090` | Öffentliche Basis-URL ohne `/mcp` |
| `CORES_DASHBOARD_PUBLIC_URL` | `http://localhost:8080` | Cores-Login, auf den OAuth verweist |
| `MCP_AUTH_MODE` | `oauth` | `oauth`, `bearer` oder nur lokal `none` |
| `MCP_ENABLE_WRITES` | `false` | Aktiviert die vier geführten Anlage-Tools; nur mit `MCP_AUTH_MODE=oauth` zulässig |
| `CORES_JWT_SECRET` | – | Dasselbe Signatur-Secret wie das Cores Dashboard, mindestens 32 Zeichen |
| `MCP_STATIC_TOKENS` | – | Optionale `name:secret`-Paare für Agents |
| `DATABASE_URL` | – | Komplette PostgreSQL-URL; überschreibt einzelne DB-Variablen |
| `DB_HOST`, `DB_PORT`, `DB_NAME` | `postgres`, `5432`, `rentalcore` | Datenbankziel |
| `DB_USER`, `DB_PASSWORD` | `rentalcore`, leer | Dedizierter Read-only-Login empfohlen |
| `MCP_QUERY_TIMEOUT` | `8s` | Transaktions- und Statement-Timeout |
| `MCP_MAX_ROWS` | `200` | Serverweites Ergebnislimit, maximal 1000 |
| `MCP_RATE_LIMIT_PER_MINUTE` | `120` | IP-basiertes Limit |
| `MCP_ALLOWED_ORIGINS` | leer | Zusätzliche erlaubte Browser-Origins |
| `MCP_KNOWLEDGE_DIRS` | `/knowledge` | Kommagetrennte, nur lesend eingebundene Dokumentordner |
| `MCP_OAUTH_DATA_FILE` | `/var/lib/cores-mcp/oauth/clients.json` | Persistenz für registrierte OAuth-Clients |

Die vollständige Vorlage steht in [.env.example](.env.example).

## Sicherheitsmodell

- Die 59 Abfragetools sind `readOnlyHint=true` und `idempotentHint=true`; die zwei Create-Tools sind als additive, nicht-idempotente Schreibvorgänge annotiert.
- Jede DB-Abfrage läuft in einer PostgreSQL-Transaktion mit `READ ONLY`, Timeout und Zeilenlimit.
- Produktion verwendet zusätzlich die Rolle `cores_mcp` mit ausschließlich `SELECT`-Rechten.
- Schreibtools verwenden ausschließlich validierte Endpunkte des jeweils verantwortlichen Core und ein zweiminütiges, auf den OAuth-Benutzer delegiertes Suite-Token. Rollenänderungen und Kontosperren werden dort live geprüft.
- Es existieren keine Tools für beliebiges SQL, Dateien, Shell, E-Mail, Änderungen, Löschungen, Statusänderungen, Bestellungen oder Freigaben. Die flexible Abfrageschicht arbeitet ausschließlich mit kuratierten Entitäten und Feldern.
- Kontaktinformationen, Secrets und interne private Notizen werden nicht absichtlich ausgegeben.
- Nutzertexte aus Beschreibungen/Notizen gelten als nicht vertrauenswürdige Daten, niemals als Agent-Anweisung.
- Toolantworten enthalten Zeitstempel, Quellen und fachliche Warnungen.

Details und Threat Model: [docs/SECURITY.md](docs/SECURITY.md).

## Knowledge-Dokumente

Markdown-, Text-, CSV- und JSON-Dateien unter `MCP_KNOWLEDGE_DIRS` werden als MCP Resources und über `knowledge.documents.search` angeboten. Damit lassen sich beispielsweise Equipment-Strategie, Freigaberegeln, Herstellerstandards oder Einkaufsgrundsätze neben den Live-Daten nutzen. Symlinks und Dateien über 2 MiB werden nicht geladen.

## Entwicklung und Release

```bash
make check
docker build -t nobentie/cores-mcp:1.2.1 -t nobentie/cores-mcp:latest .
```

Die Umbrella-Compose-Datei der Cores Suite bindet den Dienst intern ein. Der Cores-Dashboard-Reverse-Proxy veröffentlicht MCP und OAuth auf derselben Domain, damit der bestehende Suite-Login genutzt werden kann.
