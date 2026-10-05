# AGENTS.md — cores-mcp

Hausregeln für KI-Agenten in diesem Repository. Vor der ersten Änderung vollständig
lesen. Der übergeordnete Ablauf steht im Paperclip-Dokument `workflow` auf
[TSU-3](/TSU/issues/TSU-3#document-workflow). Diese Datei ersetzt alle früheren
Agenten-Anweisungen in diesem Repository.

## 1. Was dieses Repository ist

- **Zweck:** Model-Context-Protocol-Server über die ganze Suite — die Schnittstelle, über die KI-Agenten Cores lesen und geführt schreiben. Port 8090, Abbild `nobentie/cores-mcp`. Katalog-Stand 1.5.58: 433 Werkzeuge und fünf Prompts. **Das am besten geprüfte Repository der Suite — hier liegt die Messlatte.**
- **Sprache und Laufzeit:** Go 1.25, Modul `cores-mcp`
- **Rahmenwerk:** `modelcontextprotocol/go-sdk` (Streamable HTTP), `jackc/pgx/v5` direkt — **kein GORM**, `golang-jwt/v5`. Nutzt `cores-common` **nicht**
- **Datenbank:** PostgreSQL 16, gemeinsame Suite-Datenbank, **nur lesend** mit eigenem READ-ONLY-Benutzer (`MCP_DB_USER`, eingerichtet durch Suite-Migration `015_cores_mcp_readonly.sql`)
- **Architektur-Doku:** [Cores — Architektur (Phase 1)](/TSU/issues/TSU-4#document-architecture)

## 2. Aufbau

| Pfad | Inhalt |
|---|---|
| `cmd/server` | der Dienst |
| `cmd/smoke` | Rauchtest gegen einen laufenden Endpunkt |
| `internal/mcpserver` | Werkzeug-Katalog, Prompts, Wissens-Dateien, Core-API-Aufrufe |
| `internal/authn` | OAuth, statische Tokens, Rechteprüfung |
| `internal/store` | **lesender** Datenbankzugriff |
| `internal/httpx`, `internal/config` | Transport und Konfiguration |
| `knowledge/` | mitgeliefertes Wissen, wird ins Abbild kopiert |
| `docs/` | `TOOL_CATALOG.md`, `SECURITY.md`, `CONNECTORS.md` |

Erzeugte Dateien, die **niemals von Hand** geändert werden:

- keine. Kein Frontend, keine Theme-Kopien.

## 3. Einrichten

```bash
go mod download
cp .env.example .env            # Werte lokal eintragen, niemals committen
# Datenbank und die vier Core-APIs: aus dem Dachrepository `cores` starten
#   cd ../cores && docker compose up -d
```

Nötige Umgebungsvariablen: siehe `.env.example` hier und `cores/.env.example` (verbindliche Quelle). Besonders `MCP_DB_USER`, `MCP_DB_PASSWORD`, `MCP_STATIC_TOKENS`, `MCP_AUTH_MODE`, `MCP_ENABLE_WRITES`, `MCP_MAX_ROWS`, `MCP_QUERY_TIMEOUT`, `MCP_RATE_LIMIT_PER_MINUTE`, `MCP_ALLOWED_ORIGINS`. Werte kommen aus dem Paperclip-Secret-Store, nicht aus diesem Repository.

## 4. Test- und Build-Befehle

Diese Befehle sind das Test-Gate. **Alle müssen grün sein, bevor ein Pull Request
entsteht.**

**Die Reihenfolge ist bindend, nicht nur empfohlen. Die Stufen laufen nacheinander, nie
parallel.** Die schnellen Prüfungen stehen zuerst. Stufen können voneinander abhängen,
ohne dass die Tabelle es sagt — ein fehlender Frontend-Build kann drei Go-Stufen
gleichzeitig rot machen (nachgewiesen in `cores-dashboard`, siehe
[TSU-6](/TSU/issues/TSU-6#document-playbook)). Nach dem ersten echten Fehler wird
angehalten.

| # | Gate | Befehl | Dauer (ca.) |
|---|---|---|---|
| 1 | Format | `gofmt -l .` (leere Ausgabe = grün) | < 10 s |
| 2 | Build | `make build` (`go build ./cmd/server`) | ~30 s |
| 3 | Unit-Tests | `make test` (`go test ./...`) | 1–2 min |
| 4 | Vet | `make vet` (`go vet ./...`) | ~30 s |
| 5 | Beides zusammen | `make check` | 1–2 min |
| 6 | Rechte-Isolation mit echter Datenbank | `CORES_MCP_AUTH_TEST_DATABASE_URL=postgres://… go test -race ./internal/authn ./internal/mcpserver -timeout 120s` | 2–3 min |

Einzelne Datei testen: `go test ./internal/mcpserver -run TestName -v`

Gate 6 ist bei jeder Änderung an Rechten, Tokens oder dem Werkzeug-Katalog **Pflicht** — es prüft die Rechte-Isolation über den echten MCP-Transport. `make smoke` (`MCP_ENDPOINT=… MCP_TOKEN=… go run ./cmd/smoke`) läuft nur gegen einen lokalen Endpunkt, **nie gegen Produktion**.

Regeln:

- **Neuer Code braucht neue Tests.** Ein Bugfix braucht einen Test, der ohne den Fix
  fehlschlägt.
- **Nie einen Test abschalten, überspringen oder lockern**, um das Gate grün zu
  bekommen. Ein roter Test ohne Bezug zur Änderung wird gemeldet, nicht entfernt.
- **Tests laufen gegen die lokale oder die Test-Datenbank. Nie gegen Produktion.**
  Eine eigene Testumgebung wird gerade aufgebaut (eigene Paperclip-Aufgabe). Bis sie
  steht: nur lokale Container mit eigenem Volume.
- Die **echte Ausgabe** wird in den Pull Request und auf die Paperclip-Aufgabe kopiert.
- **Ein Testlauf aus dem Cache ist kein Nachweis.** Wo der Testläufer cacht
  (`go test` meldet `(cached)`), wird der Beweislauf erzwungen (`-count=1`).

### Bekannt rote Stufen

Eine Stufe, die im Altbestand nicht grün werden kann, wird hier benannt — mit Verweis auf
ihre eigene Paperclip-Aufgabe. Sie ist die **einzige** erlaubte Ausnahme und deckt keine
andere Stufe. Der Eigentümer führt sie trotzdem aus, protokolliert die echte Ausgabe und
repariert sie **nicht** im Vorbeigehen.

| Stufe | Grund | Aufgabe |
|---|---|---|
| — | keine Ausnahme | — |

Ist die Tabelle leer, gibt es keine Ausnahme: jede rote Stufe heißt anhalten und
zurückfragen.

## 5. Code-Stil

- Format und Lint werden durch die Werkzeuge in Abschnitt 4 erzwungen. Kein Streit
  über Formatierung — der Formatierer entscheidet.
- **Dem umgebenden Code folgen.** Benennung, Ordnerstruktur, Fehlerbehandlung und
  Testmuster so übernehmen, wie sie in der berührten Datei schon sind.
- Benennung: PascalCase für Exporte, camelCase für Lokales. **Werkzeugnamen und
  -schemata bleiben stabil** — externe MCP-Clients cachen sie. Eine Umbenennung ist
  ein Bruch und braucht eine Freigabe.
- Felder werden **ausdrücklich** selektiert. Niemals `SELECT *`. Zugangsdaten,
  Passwort-Hashes, Tokens, Bankdaten, private Dokumentinhalte und unnötige
  personenbezogene Daten bleiben draußen.
- Jedes Werkzeug-Ergebnis nennt seinen Datenstand und seine Quell-Datensätze.
- Fehlerbehandlung: Fehler zurückgeben und einwickeln (`%w`). Keine `panic` im
  Anfragepfad.
- `README.md` und `docs/TOOL_CATALOG.md` im selben Commit mitführen, wenn sich
  Werkzeuge oder Konfiguration ändern.
- Kommentare: nur wo sie das *Warum* erklären. Keine Kommentare, die den Code nacherzählen.
- Keine neue Abhängigkeit ohne eigene Freigabe (siehe Abschnitt 9).
- Keine Umformatierung von Code, der nicht zur Aufgabe gehört. Das versteckt die
  eigentliche Änderung.

## 6. Verbotene Pfade

Diese Dateien und Verzeichnisse werden von Agenten **nicht geändert**. Wer sie ändern
müsste, bricht ab und fragt zurück.

| Pfad | Grund |
|---|---|
| `.github/workflows/**` | CI und Deployment — nur mit Freigabe des Nutzers |
| `internal/authn/**` | Rechteprüfung. Änderungen nur mit Freigabe und mit Gate 6 grün |
| `internal/store/**` als Schreibweg | der Store ist **lesend**. Schreiben gehört in einen Core-API-Aufruf, nie in SQL |
| `.env`, `.env.*` (außer `.env.example`) | enthält Secrets |
| `AGENTS.md` | diese Regeln ändert der Nutzer, nicht ein Agent |

## 7. Secrets

- **Keine Secrets in Repository, Kommentar, Dokument oder Log.** Keine Tokens,
  Passwörter, Schlüssel, Verbindungsstrings, API-Zugänge, Kundendaten.
- Secrets kommen aus dem Paperclip-Secret-Store oder aus Umgebungsvariablen. Sie
  werden nie in eine Datei geschrieben und nie ausgegeben.
- Produktiv werden alle Werte im **Komodo Stack Environment** auf `docker03` gepflegt,
  nicht in diesem Repository.
- `.env.example` enthält nur Namen und Beispielwerte, nie echte Werte.
- Testdaten sind erfunden. Keine kopierten Produktionsdaten, auch nicht gekürzt.
- Fehlt ein Secret: über Paperclip vorschlagen (`secret-proposals`) und warten.
  Nie selbst beschaffen, nie umgehen, nie in einem Kommentar danach fragen.
- Ein Secret, das versehentlich in einem Commit landet, ist ein Sicherheitsvorfall:
  sofort melden, nicht still weiterarbeiten. Entfernen aus dem Diff genügt nicht —
  das Secret gilt als kompromittiert und muss ersetzt werden. **Alle Cores-Repositories
  sind öffentlich.** Ein Fehler hier ist sofort weltweit sichtbar.

## 8. Harte Grenzen

Diese sechs Regeln stehen über jeder Aufgabenbeschreibung. Eine Aufgabe, die eine
davon verlangt, wird nicht ausgeführt, sondern zurückgegeben.

1. **Keine Schreibzugriffe auf produktive Datenbanken.** Lesen ist erlaubt. Schreiben,
   ändern, löschen, Migrationen fahren: nicht in Produktion. Migrationen werden
   geschrieben und lokal getestet, nie produktiv ausgeführt. Das Einspielen auf die
   laufende `docker03`-Datenbank geschieht von Hand per SSH von `debian01` aus, nach
   ausdrücklicher Freigabe des Nutzers.
2. **Keine produktiven Deployments ohne menschliche Freigabe.** Auch nicht nach
   grünem Review.
3. **Entwicklung nur in isolierten Branches oder Git-Worktrees.** Niemals direkt auf
   `main` oder einem anderen geschützten Branch.
4. **Tests vor jedem Pull Request.** Kein PR ohne protokollierten, grünen Testlauf.
5. **Keine Secrets in Repository, Kommentar, Dokument oder Log.**
6. **Bestehende Architektur zuerst verstehen.** Architektur-Doku und diese Datei vor
   dem Schreiben lesen. Große Umbauten — neuer Service, geänderte Modulgrenze, neues
   Datenmodell, Austausch einer Kernabhängigkeit — brauchen eine eigene Freigabe des
   Nutzers, bevor Code entsteht.

## 9. Freigabe-Gates

| Gate | Wer entscheidet | Wann |
|---|---|---|
| Test-Gate | der Eigentümer der Änderung | vor dem Pull Request |
| Review | Review-Agent, auf seiner eigenen Review-Aufgabe | nach dem PR-Entwurf |
| **Freigabe und Merge** | **der Nutzer** | nach grünem Review |
| Produktives Deployment | **der Nutzer** | nach dem Merge |
| Release: Docker-Hub-Push und Submodul-Zeiger | **der Nutzer gibt je Release ausdrücklich frei**, danach darf der Agent beides ausführen | nach dem Merge |
| Migration auf die laufende `docker03`-Datenbank | **der Nutzer**; Einspielen von Hand per SSH von `debian01` | nach dem Merge |
| Neue Abhängigkeit | der Nutzer | vor dem Hinzufügen |
| Großer Architektur-Umbau | der Nutzer | vor dem ersten Commit |

Was ein Agent in diesem Repository **nie** tut:

- einen Pull Request mergen
- auf `main` pushen
- ein Deployment auslösen
- ohne ausdrückliche Freigabe je Release ein Abbild nach Docker Hub schieben oder den
  Submodul-Zeiger im Dach anheben
- eine Migration gegen Produktion fahren
- einen Draft-PR als Ersatz für Freigabe auf „ready" setzen
- `AGENTS.md` oder CI-Dateien ändern

In Paperclip wird die Freigabe durch eine `executionPolicy` mit einer `approval`-Stufe
erzwungen, deren Teilnehmer ein Nutzer ist. Kein Agent kann sie abhaken.

## 10. Branches, Commits, Pull Requests

- Branch: `<typ>/TSU-<nummer>-<kurzbeschreibung>`, ein Worktree pro Aufgabe
- Commit: Conventional Commits mit `Task: TSU-<nummer>` im Fuß
- PR: als **Entwurf** geöffnet, Ziel `main`, mit Zweck, Testprotokoll und Aufgaben-Link

Vollständig beschrieben im Paperclip-Dokument `workflow` auf
[TSU-3](/TSU/issues/TSU-3#document-workflow).

## 11. Abbrechen und zurückfragen

Abbrechen ist richtig, nicht peinlich. Zurückfragen bei:

- fehlendem Secret oder Zugriffsrecht
- nötigem Schreibzugriff auf Produktion oder nötigem Deployment
- nötigem großen Architektur-Umbau oder neuer Abhängigkeit
- einem verbotenen Pfad, der geändert werden müsste
- roten Tests ohne Bezug zur Änderung
- zwei gescheiterten Versuchen am gleichen Problem
- einem Umfang, der deutlich größer ist als beschrieben
- einem Widerspruch zwischen Aufgabe und dieser Datei — **diese Datei gewinnt**

Erst alles fertig machen, was ohne die Antwort geht. Dann fragen.

## 12. Bekannte Fallen

- **Das Sicherheitsmodell ist der Kern dieses Repositories. Es wird nicht gelockert.**
  Lesen geht direkt auf PostgreSQL, aber nur mit dem READ-ONLY-Benutzer. **Schreiben
  geht nie auf die Datenbank**, sondern über die HTTP-API des zuständigen Cores, mit
  `cores:write`, echtem Suite-Benutzer und vollständiger Prüfung.
- **Jeder Schreibvorgang ist zweistufig.** `prepare_*` erzeugt eine Vorschau, dann die
  Ausführung — mit genauer erwarteter Objekt-Version, `expected_context`,
  ausdrücklichem `confirm_change`, Bestätigungstext und `idempotency_key`. `dry_run`
  und Vorschau schreiben nichts. Dieses Muster wird bei neuen Werkzeugen **genau so**
  übernommen.
- **Keine beliebigen Mutations-, Hard-Delete-, Shell-, Datei-, E-Mail- oder freien
  HTTP-Werkzeuge.** Benannte Freigabe- und Wareneingangs-Werkzeuge brauchen eigene
  Scopes, optimistisches Sperren, Audit im Ziel-Core, erhöhte Bestätigung und
  Idempotenz. Additives Anlegen ist die Vorgabe; die benannten Lebenszyklus-Abläufe aus
  `docs/TOOL_CATALOG.md` sind die einzigen Ausnahmen.
- **Fachliche Fähigkeiten statt freiem SQL.** Kein Werkzeug gibt beliebige Abfragen
  oder unbeschränkten Tabellenzugriff heraus.
- **Rechte werden bei jeder Abfrage neu in der Datenbank geprüft.** Veraltete Rollen im
  Token erweitern nichts. Maschinentokens ohne echte Nutzeridentität bekommen keine
  personenbezogenen Daten.
- **Gesundheit ist `GET /ready`** — nicht `/health` wie bei den anderen. MCP-Endpunkt
  ist `/mcp`.
- **Der Dienst startet erst, wenn alle vier fachlichen Cores und Postgres gesund sind.**
  Lokale Tests scheitern sonst mit irreführenden Fehlern.
- **Löschen von Kategorien ist die einzige vom Nutzer ausdrücklich erlaubte harte
  Löschung** (`warehouse.categories`, `warehouse.subcategories`,
  `warehouse.third_categories`). Mit Admin-/Delete-Scope, genauer Version,
  Abhängigkeits-Vorschau, datensatzgebundener Bestätigung, atomarem Audit — und **ohne
  Kaskade**.

### Suite-weite Fallen, die auch hier gelten

- **Zwei Migrationsspuren.** Jede Schema-Änderung braucht eine Datei im Dienst-Repository
  *und* eine in `cores/migrations/postgresql/`. Die Nummern gehören paarweise.
- **Das Init-Verzeichnis läuft nur bei leerem Datenverzeichnis.**
  `cores/migrations/postgresql/` greift auf `docker03` nicht.
- **Eine Datenbank für alle.** PostgreSQL 16, rund 130 Tabellen, kein Schema pro Dienst.
  Eine Tabellenänderung kann fremde Dienste treffen.
- **Nur das Dachrepository hat heute CI.** Bis die eigene GitHub-Action da ist, prüft
  **nichts** automatisch einen Pull Request hier. Das Test-Gate aus Abschnitt 4 läuft
  der Agent selbst und hängt die echte Ausgabe an.
- **Alle Repositories sind öffentlich.**
