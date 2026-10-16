# Planner-Pläne und Aufgaben über MCP

PlannerCore **2.6.26**, Cores MCP **1.5.69**. Issues nbt4/cores-mcp#4 und #5.
Gesamtkatalog **492 Tools: 126 lesend / 183 vorbereitend / 183 ausführend**.

## Werkzeuge und vollständige Felder

Für `planner.plans` und `planner.tasks` bestehen jeweils die Paare
`prepare_create/create`, `prepare_update/update`, `prepare_archive/archive`
und `prepare_restore/restore`. Bestehende Create-Namen bleiben erhalten und
verwenden nun denselben besitzenden PlannerCore-Ablauf wie alle Änderungen.
`cores.entities.schema` beschreibt `planner.plans` und `planner.tasks`.

Plan: `name` (1–200 Zeichen), `description` (bis4000), `background_color`
(`#RRGGBB`), `is_favorite` (bestehende gemeinsame Markierung), `is_template`.
Task: `title` (1–500), `rich_text_notes` (bis20000), `priority`
(`urgent/important/medium/low`), `recurrence` (`none/daily/weekly/monthly`),
`bucket_id`, `position` (endlich, Betrag höchstens1e12), `start_date`, `due_date`
(RFC3339, Ende nicht vor Start), `status` (`not-started/in-progress/completed`),
`assignee_ids`, `label_ids`, `checklist`. Fortschritt und Zähler werden aus der
Checkliste abgeleitet. Aufgaben bleiben unveränderlich im ursprünglichen Plan.

Weglassen erhält bestehende Felder; leere Texte leeren optionale Texte.
JSON-null ist kein Patch. `clear_fields` leert ausschließlich `bucket_id`,
`start_date`, `due_date`, jeweils ohne gleichzeitig gesetzten Wert.
Referenzmengen und Checklisten ersetzen ausdrücklich den vollständigen Satz,
`[]` leert, Weglassen erhält. Je Satz höchstens100 Einträge. Checklisten-IDs
behalten Identität; neue IDs werden deterministisch vorgeschlagen. Fehlende
bisherige Items werden nur im ausdrücklich bestätigten vollständigen Diff
entfernt. Bucket/Labels müssen demselben Plan gehören; Assignees sind aktuell
aktive Suite-Nutzer mit tatsächlicher Planmitgliedschaft.

`planner.plans.search` und `planner.tasks.search` lesen aktive Daten, mit
`include_archived` auch Archive. `planner.plans.get` liefert erhaltene
Planmetadaten, minimale Mitglieds-ID/Rolle/Aktivität, Buckets/Labels und aktive
Aufgaben; `include_archived_tasks` ergänzt deren Archive.
`planner.tasks.get` liest eine genaue aktive/archivierte Aufgabe mit Versionen,
Notizen, Assignee-/Label-IDs und Checkliste. `planner.plans.audit_history` und
`planner.tasks.audit_history` liefern höchstens100 Ereignisse: Akteur, Ursprung,
Aktion, Zeit, Ergebnisversion, Archivstatus und geänderte Feldnamen.
Private Benutzerfelder, Kommentartexte, Dateiinhalte/-pfade und vollständige
Audit-JSONs werden durch diese MCP-Historien nicht ausgegeben.

## Rechte, endgültiger Entwurf und Wiederholung

Alle16 Vorbereitungs-/Schreibtools verlangen `cores:read` und den ausdrücklich
erteilten Scope `cores:planner:create`, `cores:planner:update` beziehungsweise
`cores:planner:archive`; Restore verwendet Archive. Legacy `cores:write` allein
reicht hierfür nicht. Reale aktive Suite-Identität und tatsächliche aktuelle
Planmitgliedschaft gelten in MCP-Leseabfragen und im Owner vor Vorschau,
Ausführung und Receipt-Replay. Adminstatus ersetzt keine Mitgliedschaft.
Plananlage setzt den aktuellen Nutzer als Owner; auch deren Replay prüft die
Mitgliedschaft am erzeugten Plan. Metadatenpflege erlaubt bestehende Mitglieder;
Mitgliederverwaltung bleibt ein eigener nativer Owner-Ablauf.

Vorschau schreibt keine Geschäftsdaten, Historien oder Receipts. Sie bindet
Datensatz, Planversion, Mitglieder/Rollen/Aktivität, Buckets/Labels,
Aufgabenversionen, erhaltene Referenzzahlen, Duplikate und vollständige Wirkung
an einen SHA-256-Kontext. Mehr als1000 Datensätze pro Referenzgruppe oder1MiB
Kontext blockieren den Ablauf; die eingehende Owner-Anfrage ist auf256KiB
begrenzt. Sichtbare gleichnamige Datensätze einschließlich Archive verlangen
nach Prüfung `allow_duplicate=true` und erneute endgültige Vorbereitung.

Ausführung übernimmt `creation_id` bei Create, sonst `expected_updated_at`,
zusätzlich `expected_plan_updated_at` für bestehende Pläne,
`expected_context` und `required_confirmation_text` als `confirmation_text`.
`confirm_creation=true` bestätigt Create, `confirm_change=true` die übrigen
Aktionen. Immer einen eindeutigen `idempotency_key` angeben. Wenn die Vorschau
`effective_at` für eine Statusänderung liefert, exakt kopieren; der Zeitanker
ist15Minuten gültig. `dry_run=true` führt ausschließlich die Vorschau aus.
Geänderte Versionen oder Kontexte erfordern eine neue finale Vorschau.

Owner und MCP prüfen Rechte vor dauerhaftem Replay. Gleicher Schlüssel und
identische Anfrage liefern den gespeicherten Erfolg, eine abweichende Anfrage
409. Fehlgeschlagene Transaktionen können mit demselben Schlüssel erneut
versucht werden. Aktuelle Rechte bleiben erforderlich. Coarse NOWAIT-Tabellen-
und Zeilensperren vermeiden umgekehrte Parent-/Child-Wartezyklen; konkurrierende
Schreibvorgänge, auch an anderen Plänen, können409 liefern und werden nach
kurzem Abstand mit neu geprüftem Kontext wiederholt.

## Archiv, Wiederholung und native Kompatibilität

Planarchivierung behält alle Aufgaben und Verknüpfungen, blendet den gesamten
Plan aus aktuellen Übersichten aus und friert Inhalte ein. Restore erhält
individuell archivierte Aufgaben weiterhin archiviert. Taskarchive behalten
Checklisten, Assignees, Labels, Kommentare, Anhänge und weitere Verknüpfungen.
Aktive abhängige Gegenaufgaben blockieren Taskarchivierung. Restore prüft
aktuelle Referenzen erneut; gegebenenfalls zuerst Plan oder Mitgliedschaft
wiederherstellen. Mitgliedschaften dürfen auch an archivierten Plänen entzogen
werden. Hard-Delete von Plänen/Aufgaben und Änderung von Identität/Ersteller
sind auf Datenbankebene gesperrt. Events sind unveränderlich.

Abschließen einer täglichen/wöchentlichen/monatlichen Aufgabe erzeugt eine
Folgeaufgabe, auch bei Anlage im abgeschlossenen Zustand. Die Vorschau zeigt
deren genaue Identität und Wirkung. Fälligkeit wird vom bisherigen Fälligkeits-
datum, sonst vom Vorschauzeitanker um1Tag/7Tage/1Kalendermonat verschoben
(Go AddDate-Kalendernormalisierung). Folgeaufgabe übernimmt Titel, Priorität,
Wiederholung, Bucket und endgültige Assignees, hat Position der abgeschlossenen
Aufgabe+1000 und leere Notizen/Labels/Checkliste ohne Kommentare oder Anhänge.
Abschluss, Folgeaufgabe/Assignees, Versionen, beide Audits, native Task-Historie
und Receipt committen gemeinsam oder gar nicht. Replay erzeugt keine zweite
Folgeaufgabe. Nach Commit werden bestehende interne UI-Events publiziert;
kein externer Nachrichtenversand.

Native Plan-/Task-Anlage, Metadatenänderung, Soft-Archivierung,
Checklisten-/Assignee-Änderung und Favoriten laufen durch dieselbe gemeinsame
Transaktion mit bekanntem UI-Akteur. Checklisten-/Assignee-Snapshots sind
versionsgebunden; native optionale Versionsangaben werden geprüft.
Kopieren bleibt die bestehende reine Metadatenkopie mit neuem Owner und
aktueller, bis Commit gesperrter aktiver Mitgliedschaft. Andere vorhandene
native Child-Writer ändern ebenfalls Datensatz-/Planversionen über DB-Trigger;
ihre vollständige Akteur-/Audit-/Replay-Abdeckung bleibt ein gesonderter offener
Prüfpunkt. Delegierte Tokens mit mcp_scope, einschließlich Cookie/Bearer, können
keine nativen Planner-Lese-/Schreibendpunkte als Ausweichweg verwenden.

## Migration und Rollout

Planner **005**, Root **055** und Startup-SQL sind identisch. Additiv werden
Task-Archivzeit, eigene Receipts/Events, Versionierungs-/Referenz-/Archivschutz
und unveränderliche Ereignisse ergänzt. Vorhandene Daten werden nicht
bereinigt oder umgeschrieben. Startup-DDL hat5s Lock-/30s Statement-Limit.
Zuerst PlannerCore veröffentlichen/deployen, danach MCP; Umgebungen, Volumes,
Netzwerke, Ports und andere Dienste erhalten. Image, OCI-Revision und Health/
Ready prüfen. MCP-Katalog neu laden und gewünschte Aktionsscopes ausdrücklich
freigeben. Keine produktiven Geschäftsdatensätze als Tests ändern.

Separate Buckets-/Labels-/Dependencies-/Sprints-/Goals-CRUD, Dokumentabläufe,
weitere Undo-Pfade und verbleibende Legacy-Writer-Prüfungen sind weiterhin offen.
Die Parent-Issues bleiben in_progress.

## Prüfprotokoll vor Veröffentlichung (2026-10-16)

Isoliertes PostgreSQL16 mit ausschließlich synthetischen Daten:
Planner `go test -count=1 ./... -timeout 180s` erfolgreich, Lifecycle25.089s;
zusätzlicher Copy-Regressionslauf1.622s. Vollständiger Planner-Race-Lauf
`go test -race -count=1 ./... -timeout 180s` erfolgreich, Lifecycle28.994s,
Copy2.900s. MCP-Volltests mit allen isolierten Datenbank-DSNs erfolgreich,
`internal/mcpserver`25.591s; gezielte Planner-/OAuth-/Read-Tests3.390s.
Vollständiger MCP-Race-Lauf ebenfalls erfolgreich (`internal/mcpserver`115.679s).
Beide Dienste: `go vet ./...` und Server-Build erfolgreich. Planner-Frontend:
`npm ci && npm run build` erfolgreich (Vite19.60s, bestehender Bundlegrößenhinweis).
Frisches Root-Volume initialisiert Migrationen bis055; Planner005 zweimal
anschließend erfolgreich angewandt, keine Mutationsevents/Receipts erzeugt.
Unabhängiges Abschlussreview ohne konkrete verbleibende Release-Blocker.
Produktive Geschäftsdatensätze werden für diese Nachweise nicht verändert.
