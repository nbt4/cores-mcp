# Cores MCP

Cores MCP bindet die gesamte Cores Suite als sicheren, rein lesenden MCP-Server an ChatGPT, Claude, Codex und andere MCP-fähige Agents an. Der Chat bleibt beim jeweiligen KI-Anbieter; Cores stellt nur kontrollierte Werkzeuge und Kontext bereit.

Der Server bietet 59 fachliche Tools, fünf wiederverwendbare Analyse-Prompts und dokumentierbare Knowledge-Ressourcen für RentalCore, WarehouseCore, PlannerCore und ProcurementCore. Es gibt bewusst kein beliebiges SQL-Tool und keinen Schreibzugriff.

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

In ChatGPT wird sie als benutzerdefinierte MCP-App/Plugin, in Claude als Custom Connector eingetragen. Beim ersten Verbinden registriert sich der Client dynamisch. Fehlt die Cores-Sitzung, führt der Flow durch das zentrale Cores-Login und automatisch zurück in den OAuth-Dialog. Die dortige, im Suite-Design dargestellte Freigabe fragt ausschließlich die Leseberechtigung `cores:read` ab. Ihre Content Security Policy erlaubt den POST an die konfigurierte öffentliche MCP-Origin und den anschließenden Redirect ausschließlich an die bereits validierte, registrierte Callback-Origin des Clients. Die kanonischen Suite-Schriften werden von Google Fonts geladen. Der Nutzer muss ein aktives Cores-Konto besitzen.

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

- Alle Tools sind `readOnlyHint=true` und `idempotentHint=true`.
- Jede DB-Abfrage läuft in einer PostgreSQL-Transaktion mit `READ ONLY`, Timeout und Zeilenlimit.
- Produktion verwendet zusätzlich die Rolle `cores_mcp` mit ausschließlich `SELECT`-Rechten.
- Es existieren keine Tools für beliebiges SQL, Dateien, Shell, E-Mail, Statusänderungen, Bestellungen oder Freigaben. Die flexible Abfrageschicht arbeitet ausschließlich mit kuratierten Entitäten und Feldern.
- Kontaktinformationen, Secrets und interne private Notizen werden nicht absichtlich ausgegeben.
- Nutzertexte aus Beschreibungen/Notizen gelten als nicht vertrauenswürdige Daten, niemals als Agent-Anweisung.
- Toolantworten enthalten Zeitstempel, Quellen und fachliche Warnungen.

Details und Threat Model: [docs/SECURITY.md](docs/SECURITY.md).

## Knowledge-Dokumente

Markdown-, Text-, CSV- und JSON-Dateien unter `MCP_KNOWLEDGE_DIRS` werden als MCP Resources und über `knowledge.documents.search` angeboten. Damit lassen sich beispielsweise Equipment-Strategie, Freigaberegeln, Herstellerstandards oder Einkaufsgrundsätze neben den Live-Daten nutzen. Symlinks und Dateien über 2 MiB werden nicht geladen.

## Entwicklung und Release

```bash
make check
docker build -t nobentie/cores-mcp:1.0.2 -t nobentie/cores-mcp:latest .
```

Die Umbrella-Compose-Datei der Cores Suite bindet den Dienst intern ein. Der Cores-Dashboard-Reverse-Proxy veröffentlicht MCP und OAuth auf derselben Domain, damit der bestehende Suite-Login genutzt werden kann.
