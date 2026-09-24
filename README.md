# Cores MCP

## Lieferantenänderung und Deaktivierung ab 1.5.3

`procurement.suppliers.prepare_update` lädt den Lieferanten nur für
Procurement-Administratoren, prüft alle geänderten Felder und zeigt den
Ist/Soll-Diff sowie die aktuelle `expected_updated_at`-Version.
`procurement.suppliers.update` verlangt danach ausdrückliche Bestätigung,
einen Idempotenzschlüssel und eine unveränderte Version. `active=false`
deaktiviert den Lieferanten mit eigenem Audit-Ereignis, sobald keine offene
Bestellung mehr vorliegt; `active=true` aktiviert ihn wieder. Angebote und
Bestellungen bleiben erhalten.

## Geführte Lieferantenanlage ab 1.5.2

`procurement.suppliers.prepare_create` prüft sämtliche Lieferantenfelder,
Lieferantencode und ähnliche Namen gegen den aktuellen Bestand und liefert
einen vollständigen Entwurf mit konkreten Rückfragen.
`procurement.suppliers.create` verlangt anschließend `confirm_creation=true`
und einen Idempotenzschlüssel. Die Anlage erfolgt über die ProcurementCore-API
mit Administratorprüfung, Audit-Herkunft `MCP/AI` und dauerhafter Deduplizierung.

## Stammdatenauflösung ab 1.5.1

`cores.master_data.resolve` sucht vorhandene Warehouse-Stammdaten sowie
Procurement-Lieferanten und -Kategorien, Rental-Kunden und Veranstaltungsorte
mit tolerantem Namens- und Codeabgleich. Exakte Treffer werden ausgewählt;
ähnliche oder mehrdeutige Treffer verlangen eine Rückfrage vor einer Anlage.
Das Tool liest ausschließlich Stammdaten ohne Kontaktangaben. Wenn die
serverweite Zeilenbegrenzung den Kandidatenpool abschneidet, kennzeichnet die
Antwort das Ergebnis als möglicherweise unvollständig.

## Procurement-Lifecycle ab 1.5.0

Vier neue, zweistufige Tools decken sicherheitskritische Procurement-Prozesse
ab: `procurement.requisitions.prepare_decide`/`.decide` genehmigen, lehnen ab
oder geben einen eingereichten Bedarf zurück;
`procurement.orders.prepare_receive`/`.receive` buchen Teil-, Voll- oder
ausdrücklich bestätigte Überlieferungen. Freigaben erzwingen das Vier-Augen-
Prinzip. Beide Prozesse prüfen `expected_updated_at`, benötigen eigene Scopes
und eine datensatzgebundene Bestätigungsphrase zusätzlich zu `confirm_*` und
`idempotency_key`.

ProcurementCore 1.0.38 speichert MCP-Idempotenzschlüssel und Ergebnis atomar mit
der Fachmutation. Wareneingänge legen bei Einzelverfolgung je Seriennummer ein
Gerät an, aktualisieren Mengenbestand und erzeugen einen offenen Putaway-Task.
Audit-Einträge kennzeichnen die Herkunft `MCP/AI`; identische Wiederholungen
bleiben auch nach MCP-Neustarts wirkungslos.

## Schreibschutz-Grundlage ab 1.4.1

Alle Ausführungstools unterstützen `dry_run` und
`idempotency_key`. Ein Dry-Run unterdrückt selbst bei versehentlich gesetztem
`confirm_*` jede Ausführung und liefert ausschließlich die validierte Vorschau.
Jeder bestätigte Schreibaufruf benötigt einen Schlüssel mit 8–128 sicheren
Zeichen. Wiederholungen mit identischem Benutzer, Tool, Schlüssel und Payload
liefern für 24 Stunden das erste Ergebnis; derselbe Schlüssel mit abweichender
Payload wird blockiert. Parallele Wiederholungen werden zusammengeführt. Der
Schlüssel wird zusätzlich als `Idempotency-Key` an den verantwortlichen Core
weitergereicht und im MCP-Audit nur als gekürzter Hash protokolliert.

Neben dem kompatiblen Sammel-Scope `cores:write` können Clients nun gezielt
`cores:<service>:create` oder `cores:<service>:update` anfordern. Der
MCP-Endpunkt selbst verlangt nur `cores:read`; dadurch funktioniert ein bewusst
read-only ausgestelltes Token auch dann, wenn Schreibtools serverseitig
aktiviert sind. Das Ausführungstool prüft anschließend seinen konkreten Scope,
und die Rollenprüfung des Ziel-Cores bleibt unverändert wirksam.

## Atomare Warehouse-Produktanlage ab 1.4.0

`warehouse.products.prepare_create` liefert jetzt das vollständige Feldschema,
prüft ähnliche Produkte und löst Hersteller, Marke sowie alle drei
Kategorieebenen tolerant gegen Schreibvarianten auf. Fehlende Stammdaten werden
nur nach einer ausdrücklichen `create_*`-Entscheidung in den finalen Entwurf
aufgenommen. `warehouse.products.create` übergibt diesen Entwurf nach der
separaten Bestätigung an WarehouseCore; Stammdaten, Produkt, Anfangsbestand und
anfängliche Devices werden dort gemeinsam committet oder vollständig
zurückgerollt und auditiert.

`cores.entities.schema` beschreibt die pflegbaren Felder aller freigegebenen
Schreibentitäten maschinenlesbar. `warehouse.master_data.resolve` unterscheidet
exakte, ähnliche, mehrdeutige und fehlende Hersteller-, Marken-, Kategorie-,
Einheiten- und Lagerplatztreffer.

## Requirement-Produktsuche ab 1.3.1

Die Vorbereitung und Anlage von Job-Produktbedarfen löst den Hersteller jetzt
über die Warehouse-Produktrelation auf. Dadurch funktionieren
`rental.requirements.prepare_create` und `rental.requirements.create` auch bei
Produkt-IDs, ohne auf eine nicht vorhandene Produktspalte zuzugreifen.

## Operative P0/P1-Workflows ab 1.3.0

Sechs neue, jeweils zweistufige Workflows schließen den operativen Weg vom Job
bis zur Lager- und Bestellbewegung: Geräte zuweisen, Jobs ändern oder
stornieren, Requirement-Mengen ändern, Bestellungen anlegen, Lagerbewegungen
buchen und Gerätezustände ändern. Jeder Workflow besitzt ein read-only
`prepare_*`-Tool mit Live-Validierung und ein getrenntes Ausführungstool, das
erst nach einer finalen Vorschau und ausdrücklicher Bestätigung schreibt.

Die Ziel-Cores protokollieren die Änderungen in Job-Historie,
Gerätestatus-Historie, Bewegungs- beziehungsweise Procurement-Aktivitätslog;
das MCP-Audit nennt zusätzlich Benutzer und Tool. Rücknahmen erfolgen bewusst
als validierte Gegenoperation statt als kaskadierendes Universal-Undo.

## Job-Produktbedarfe per MCP ab 1.2.4

`rental.requirements.prepare_create` löst Job und aktives Warehouse-Produkt auf,
prüft die positive Menge und erkennt bereits vorhandene Verknüpfungen.
`rental.requirements.create` legt nach ausdrücklicher Bestätigung genau einen
neuen Bedarf über die RentalCore-API an. Bestehende Bedarfsmengen werden bewusst
nicht überschrieben.

## Verlässliche Schreibfreigabe ab 1.2.3

Diese Version führte den getrennten Scope `cores:write` ein. Seit 1.4.1 verlangt
der MCP-Endpunkt als gemeinsame Mindestberechtigung nur noch `cores:read`, damit
bewusst read-only ausgestellte Tokens gültig bleiben. Schreibzugriffe benötigen
am jeweiligen Tool entweder `cores:write` oder den passenden granularen
Service-/Aktions-Scope.

## Geführte Schreibzugriffe ab 1.2.2

Mit `MCP_ENABLE_WRITES=true` besitzt jeder Core mindestens einen sicher geführten,
additiven Schreibpfad: ProcurementCore-Produkte, RentalCore-Jobs,
PlannerCore-Pläne und -Tasks sowie WarehouseCore-Lageraufgaben. Jeder Vorgang
benötigt einen persönlichen OAuth-Zugang mit `cores:write`, ein separates
Vorbereitungstool, vollständig validierte Live-Referenzen, eine finale Vorschau
und `confirm_creation=true` nach ausdrücklicher Benutzerbestätigung.

Seit 1.3.0 ergänzen einzelne validierte P0/P1-Capabilities diese ursprünglich
rein additive Grenze. Seit 1.5.0 sind ausschließlich die dokumentierten,
scope-getrennten Freigabe- und Wareneingangsprozesse zusätzlich erlaubt.
Beliebige Schreib-, Lösch-, SQL- oder HTTP-Werkzeuge bleiben ausgeschlossen.
Fachlogik, Rollen und Bestände können nicht durch einen MCP-Client umgangen werden.

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

Der Server bietet 62 lesende fachliche Tools, fünf wiederverwendbare Analyse-Prompts und dokumentierbare Knowledge-Ressourcen für RentalCore, WarehouseCore, PlannerCore und ProcurementCore. Bei `MCP_ENABLE_WRITES=true` kommen siebzehn vorbereitende und siebzehn bestätigte, eng begrenzte Schreibtools für alle vier Core-Services hinzu. Beliebiges SQL, generische HTTP-Aufrufe und Hard-Deletes bleiben ausgeschlossen.

## Geführte Schreibzugriffe mit Rückfragen

- `procurement.products.prepare_create` analysiert optional einen Produktlink, führt erkannte und explizit genannte Werte zusammen, prüft Duplikate, schlägt Kategorien/Lieferanten vor und liefert `questions_for_user` für alle fehlenden Angaben.
- `procurement.products.create` legt erst an, wenn Pflichtfelder eindeutig sind, empfohlene Lücken ausgefüllt oder ausdrücklich akzeptiert wurden und `confirm_creation=true` nach einer finalen Vorschau gesetzt ist.
- `procurement.suppliers.prepare_create` und `procurement.suppliers.create` prüfen Code, ähnliche Namen und alle Stammdaten vor der bestätigten Anlage.
- `procurement.suppliers.prepare_update` und `procurement.suppliers.update` zeigen jede Lieferantenänderung samt Diff und Version vor der Bestätigung; `active=false` deaktiviert den Datensatz.
- `rental.jobs.prepare_create` löst Kunden, Status, Jobkategorie und Location gegen die Live-Daten auf. Mehrdeutige Namen werden nie geraten, sondern als Auswahl zurückgegeben.
- `rental.jobs.create` verlangt vollständige Kerndaten und dieselbe ausdrückliche Bestätigung.
- `rental.requirements.prepare_create` und `rental.requirements.create` lösen Job und Produkt eindeutig auf, prüfen die Menge und verweigern das Überschreiben eines vorhandenen Bedarfs.
- `rental.jobs.prepare_assign_device` und `rental.jobs.assign_device` prüfen Job, Gerät und bestehende Zuordnungen vor der Zuweisung.
- `rental.jobs.prepare_update` und `rental.jobs.update` zeigen Alt-/Neuzustand und verwenden für Stornos den konfigurierten Storno-Status statt Löschung.
- `rental.requirements.prepare_update` und `rental.requirements.update` ändern nur die Menge einer einzelnen Bedarfszeile.
- `procurement.orders.prepare_create` und `procurement.orders.create` validieren Lieferant, Positionen, Währung, Termine, Duplikate und Gesamtwert; die Ausführung bleibt auf Procurement-Administratoren begrenzt.
- `procurement.requisitions.prepare_decide` und `procurement.requisitions.decide` prüfen Status, Version und Vier-Augen-Trennung und verlangen eine ID-gebundene Bestätigungsphrase.
- `procurement.orders.prepare_receive` und `procurement.orders.receive` prüfen offene Menge, Produktverknüpfung, Seriennummern und Zielzone; die atomare Buchung erzeugt Bestand, Geräte und Putaway-Task.
- `warehouse.movements.prepare_create` und `warehouse.movements.create` buchen bestätigte Einlagerung, Ausgabe oder Transfer über den auditierten Scannerprozess.
- `warehouse.devices.prepare_update_status` und `warehouse.devices.update_status` trennen physischen Lagerstatus und Betriebszustand; Jobbewegungen können nicht umgangen werden.
- `planner.plans.prepare_create` und `planner.plans.create` prüfen Namen und Duplikate, bevor ein Plan angelegt wird.
- `planner.tasks.prepare_create` und `planner.tasks.create` erzwingen Planmitgliedschaft, Bucket-Zugehörigkeit und eine bewusste Duplikatentscheidung.
- `warehouse.tasks.prepare_create` und `warehouse.tasks.create` prüfen Aufgabentyp, Priorität, Fälligkeit sowie jede referenzierte Zone, jedes Case, Gerät, Produkt und jeden Job live.
- `warehouse.products.prepare_create` und `warehouse.products.create` prüfen das vollständige Produkt, lösen Stammdaten fuzzy auf und legen ausdrücklich freigegebene Hersteller, Marken oder Kategorieebenen zusammen mit Produkt, Anfangsbestand und Devices atomar an.
- `cores.entities.schema` liefert Feldtypen und Validierungsbeschreibungen für jede freigegebene Schreibentität; `warehouse.master_data.resolve` und `cores.master_data.resolve` klassifizieren Stammdatentreffer, ohne Daten zu verändern.

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

In ChatGPT wird sie als benutzerdefinierte MCP-App/Plugin, in Claude als Custom Connector eingetragen. Beim ersten Verbinden registriert sich der Client dynamisch. Fehlt die Cores-Sitzung, führt der Flow durch das zentrale Cores-Login und automatisch zurück in den OAuth-Dialog. Die dortige, im Suite-Design dargestellte Freigabe zeigt `cores:read` und angeforderte Schreibrechte verständlich an. Ohne Schreibscope bleibt dieselbe Verbindung vollständig read-only. Bereits verbundene Clients müssen für neu benötigte Scopes neu autorisiert werden. Ihre Content Security Policy erlaubt den POST an die konfigurierte öffentliche MCP-Origin und den anschließenden Redirect ausschließlich an die bereits validierte, registrierte Callback-Origin des Clients. Der Nutzer muss ein aktives Cores-Konto besitzen.

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

Mit einem interaktiven OAuth-Token inklusive `cores:write` prüft der sichere
Full-Smoke auch alle Vorbereitungs- und Ausführungstools. Er setzt keine
Bestätigungsfelder und führt daher keine Mutation aus:

```bash
go run ./cmd/smoke -endpoint "$MCP_ENDPOINT" -token "$MCP_TOKEN" -include-writes
```

Der Smoke-Test verbindet einen echten MCP-Client, listet alle Tools und ruft jedes Tool mit repräsentativen Eingaben auf.

## Konfiguration

| Variable | Standard | Bedeutung |
|---|---|---|
| `MCP_PUBLIC_URL` | `http://localhost:8090` | Öffentliche Basis-URL ohne `/mcp` |
| `CORES_DASHBOARD_PUBLIC_URL` | `http://localhost:8080` | Cores-Login, auf den OAuth verweist |
| `MCP_AUTH_MODE` | `oauth` | `oauth`, `bearer` oder nur lokal `none` |
| `MCP_ENABLE_WRITES` | `false` | Aktiviert 30 geführte Schreibtools (fünfzehn Vorschauen plus fünfzehn Ausführungen); nur mit `MCP_AUTH_MODE=oauth` zulässig |
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

- Die 62 Abfragetools und siebzehn Vorbereitungstools sind `readOnlyHint=true`; Ausführungstools erzwingen einen Idempotenzschlüssel und sind `idempotentHint=true`. Zustandsänderungen sind zusätzlich als destruktiv annotiert.
- Jede DB-Abfrage läuft in einer PostgreSQL-Transaktion mit `READ ONLY`, Timeout und Zeilenlimit.
- Produktion verwendet zusätzlich die Rolle `cores_mcp` mit ausschließlich `SELECT`-Rechten.
- Schreibtools verwenden ausschließlich validierte Endpunkte des jeweils verantwortlichen Core und ein zweiminütiges, auf den OAuth-Benutzer delegiertes Suite-Token. Rollenänderungen und Kontosperren werden dort live geprüft.
- Es existieren keine Tools für beliebiges SQL, Dateien, Shell, E-Mail, generische Änderungen oder Hard-Deletes. Zulässige Änderungen, Statuswechsel, Bewegungen, Bestellungen, Procurement-Freigaben und Wareneingänge sind einzelne fachliche Capabilities mit Live-Validierung, Ziel-Core-Berechtigung, finaler Vorschau und ausdrücklicher Bestätigung.
- Lesende Tools geben keine unnötigen Kontaktinformationen, Secrets oder internen privaten Notizen aus. Geführte Anlagen zeigen die vom Benutzer eingegebenen Felder im Entwurf und im Ergebnis.
- Nutzertexte aus Beschreibungen/Notizen gelten als nicht vertrauenswürdige Daten, niemals als Agent-Anweisung.
- Toolantworten enthalten Zeitstempel, Quellen und fachliche Warnungen.

Details und Threat Model: [docs/SECURITY.md](docs/SECURITY.md).

## Knowledge-Dokumente

Markdown-, Text-, CSV- und JSON-Dateien unter `MCP_KNOWLEDGE_DIRS` werden als MCP Resources und über `knowledge.documents.search` angeboten. Damit lassen sich beispielsweise Equipment-Strategie, Freigaberegeln, Herstellerstandards oder Einkaufsgrundsätze neben den Live-Daten nutzen. Symlinks und Dateien über 2 MiB werden nicht geladen.

## Entwicklung und Release

```bash
make check
docker build -t nobentie/cores-mcp:1.5.3 -t nobentie/cores-mcp:latest .
```

Die Umbrella-Compose-Datei der Cores Suite bindet den Dienst intern ein. Der Cores-Dashboard-Reverse-Proxy veröffentlicht MCP und OAuth auf derselben Domain, damit der bestehende Suite-Login genutzt werden kann.
