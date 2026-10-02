# Tool-Katalog

## Vollständiger Wareneingang — Procurement 1.0.68 / MCP 1.5.43

`procurement.orders.prepare_receive/receive` delegieren an
`POST /api/v1/mcp/orders/receive`. Die Vorschau bindet Bestellung, alle Positionen,
Mengen, aktive Produktzuordnung, Bestand und Lagerverteilung, Seriennummern,
Zielplatz sowie Amazon-Bestätigung an `expected_context`. `expected_updated_at`,
`confirm_receipt`, Idempotenzschlüssel und die exakte mengen-/kontextgebundene
Bestätigungsphrase sind erforderlich. Überlieferung verlangt zusätzlich
`allow_overdelivery`; Teilbestätigungen dürfen Amazons bestätigte Menge nicht
überschreiten. Einzelgeräte benötigen genau eine unbenutzte Seriennummer je Gerät
(maximal 1000); Mengenbestand erhält höchstens drei Nachkommastellen. Lagergrenzen
werden vor Ausführung geprüft. Vorschau und Dry-Run verändern keine Daten.

Bestellposition/-status, Bestand oder Geräte, Einlagerungsaufgabe/-ereignis,
Vorher-/Nachher-Audits, native Aktivität und dauerhafter Erfolgsbeleg werden atomar
verbucht. Wiederholungen prüfen die aktuellen Rechte im Eigentümer-Core. Bereits
vor dem Upgrade erfolgreiche Wareneingänge behalten ihren ursprünglichen Beleg
und erzeugen keinen zweiten Bestand. Der alte MCP-Pfad `/orders/{id}/receipt`
spielt ausschließlich solche gespeicherten Belege wieder; noch nicht ausgeführte
alte Vorschauen müssen neu vorbereitet werden. Preise und externe Nachrichten
bleiben außerhalb dieser Aktion.

Wareneingang braucht ausdrücklich `cores:procurement:receive`; Bedarfsentscheidungen
und Bestellstatus brauchen ausdrücklich `cores:procurement:approve`. Allgemeines
`cores:write` erteilt diese Freigaben nicht. Bestehende Verbindungen müssen die
benötigten Scopes mit neuer Einwilligung anfordern. `cores.entities.schema`
enthält unter `workflow_fields` die separaten Empfangs- und Statusfelder.
Der Katalog bleibt bei 373 Werkzeugen (103 Abfragen / 135 Vorschauen /
135 Ausführungen). Andere offene Punkte der Issues #4/#5 bleiben offen.


## Procurement category lifecycle — 1.5.42

373 tools: 103 reads / 135 preparations / 135 executions.
`procurement.categories` adds `prepare_archive`, `archive`, `prepare_restore`,
`restore`, `audit_history` with the same `ProcurementMasterLifecycleInput` controls
and signed current-admin/archive owner delegation as other catalog lifecycle tools.
Complete original parameter definitions, description and identity are retained.
Active products block archives; active product creation/restoration requires an active
category. Master resolution retains inactive categories and suppliers as
`restoration_required`; exact historical identity must be restored separately.

## Procurement catalog lifecycle — 1.5.41

368 tools: 102 read tools, 133 preparations, 133 executions.
For each of `procurement.suppliers`, `procurement.products`, `procurement.offers`:

| Tool suffix | Input and behavior |
| --- | --- |
| `prepare_archive` | Exact `id`; complete retained record, dependencies, version/context and confirmation phrase without mutation |
| `archive` | `id`, `expected_updated_at`, `expected_context`, `confirmation_text`, `confirm_change`, `idempotency_key`; confirmed owner execution |
| `prepare_restore` | Exact inactive `id`; reviews original fields and current parents |
| `restore` | Same lifecycle controls; preserves original business fields |
| `audit_history` | Numeric `id`, optional `limit` (max 100); current administrator, redacted action/actor/time/changes/result version |

Lifecycle requires real current administrator and `cores:procurement:archive`
(or explicitly selected legacy writes). Open orders/requisitions block affected
archives; offer restoration requires active existing parents. Native owner
rechecks rights on durable replay. Lifecycle, audit, native activity and receipt
commit atomically. History remains available with writes disabled. OAuth consent
advertises Rental/Procurement archive rights and requested Rental financial access;
existing tokens need renewed consent for added scopes.

## Frischer Stack und numerische Beschaffungsabfragen — MCP 1.5.40

Der unveränderte Katalog umfasst 353 Werkzeuge. Angebotspreise pro Packeinheit
und Wareneingangsprozente werden vor PostgreSQL-Rundung ausdrücklich als
`numeric` berechnet. Das unterstützt sowohl SQL- als auch GORM-Schemata mit
Gleitkomma-Mengen und erhält NULL bei einem Nenner 0.
Root `036` initialisiert das bestehende Planner-Schema samt Wiederholungen auf
einer leeren Umbrella-Datenbank. Procurement 1.0.65 erhält SQL-UNIQUE-Constraints
beim Start. Ein vollständiger frischer Stack und der ganze lesende Katalog
gehören zur Release-Prüfung; neue Tools oder Scopes kommen dabei nicht hinzu.


## Materialanforderungen — Rental 5.3.120 / Warehouse 5.9.106 / MCP 1.5.39

`rental.requirements.prepare_create/create` und `prepare_update/update` nutzen
nun eine geschlossene Rental-Owner-API mit vollständigem Mengenvorschlag.
Neu sind `get`, `search`, `audit_history` und `prepare_archive/archive`,
`prepare_restore/restore`: 353 Werkzeuge (99 Abfragen / 127 Vorschauen /
127 Ausführungen).

`quantity` ist die Gesamtmenge, `manual_quantity` die zusätzlich geplante
manuelle Menge. Positionsanteile werden aus den bestehenden Produktpositionen
berechnet und bleiben servergesteuert. Entweder Gesamt- oder manuelle Menge
angeben; bei beiden müssen die Werte übereinstimmen. Eine manuelle Menge 0
ist erlaubt, wenn eine positive Positionsmenge bestehen bleibt. Job-/Produkt-
Identität ist unveränderlich; andere Identitäten erhalten separate geprüfte Zeilen.
Exakte Zeilen-, Job- und Kontextversion, aktuelle Admin-/Aktionsrechte sowie
Vorschau und deren Bestätigungsphrase sind Pflicht. Aktive Bearbeiter und
Änderungen an Positionen, Geräten oder Produktreferenzen stoppen die Ausführung.
Native Job-Historie, Audit und dauerhafter Beleg werden atomar gespeichert;
Wiederholungen prüfen aktuelle Rechte im Zielservice.

Archive erhalten die ursprünglichen Mengen und die Zeilen-ID. Positionen und
noch zugeordnete Geräte blockieren sie. Archivierte Anforderungen zählen nicht
mehr für aktive Bedarfe, Packlisten und Produkt-/Beziehungs-Abhängigkeiten.
Restore erhält alle Felder und prüft Job, aktives Produkt und Positionsquellen.
Es gibt keine Bestandsbewegungen, Preisänderungen oder externen Nachrichten.
Native manuelle/Positions-Workflows archivieren entfernte Zeilen ebenfalls;
späteres Auswählen stellt dieselbe Identität innerhalb der Geschäftstransaktion
wieder her. Packlisten berücksichtigen zusätzliche manuelle Mengen auch neben
kommerziellen Positionen und erweitern deren Zubehör mit der gesamten Menge.

Rental `049` / Root `035` ergänzt Archivzeitpunkt, exakte monotone Zeilenversionen,
Schutz aller Schreiber und einen Index für aktive Anforderungen. Root `032` und
die Warehouse-Initialisierung behandeln archivierte Materialanforderungen als
historische Referenzen. Rental zuerst ausrollen, danach Warehouse und MCP;
frische/aktualisierte Datenbank und tatsächlichen Streamable-HTTP-Verkehr prüfen.

## Vollständige Job-Workflows — Rental 5.3.119 / MCP 1.5.38

Die bestehenden `rental.jobs.prepare_create/create` und
`rental.jobs.prepare_update/update` nutzen eine gemeinsame Rental-Owner-API.
Neu sind `prepare_archive/archive`, `prepare_restore/restore` und
`audit_history`: 346 Werkzeuge (96 Abfragen / 125 Vorschauen / 125 Ausführungen).

Alle Jobfelder sind verfügbar: Titel, Kunde, Status, Kategorie, Venue, Zeitraum,
Umsatz, Rabatt, Rabattart, Tagesmultiplikator und Steuerdarstellung. Ausgelassene
Felder bleiben erhalten; `clear_fields` leert ausschließlich nullable Zeitraum-,
Kategorie- und Venue-Felder. Planung erlaubt einen fehlenden Zeitraum; bestätigte
und abgeschlossene Jobs brauchen ein gültiges Datumspaar. Jobcode, Identität,
Revision, Sync-IDs und Endumsatz bleiben servergesteuert. Vorhandene Positionen
bestimmen den Umsatz mit derselben Berechnung wie die normale Anwendung.

Aktuelle Administrator-/Aktionsrechte, exakte Job- und Kontextversion sowie die
vollständige Vorschau und deren Bestätigungsphrase sind verpflichtend. Finanzfelder
und Änderungen berechneter Summen brauchen ausdrücklich `cores:rental:financial`;
`cores:write` ersetzt diese Freigabe nicht. Wiederholungen prüfen aktuelle Rechte
im Zielservice und geben dessen gespeicherten Beleg zurück. Aktive fremde
Bearbeitungssitzungen, neue Geräte-Zeitkonflikte oder veränderte Abhängigkeiten
stoppen Änderungen. Gleichnamige Jobs brauchen eine geprüfte, explizite
Duplikatfreigabe; archivierte Treffer werden über Restore erhalten.

Archivierung erhält alle Inhalte und beendet offene Planung als Storniert.
Ausgegebene Geräte, aktive Cases und offene Warehouse-Aufgaben blockieren sie.
Restore erhält den historischen Status und alle Inhalte; ein Wiederöffnen folgt
als eigene geprüfte Statusänderung. Abgeschlossene/stornierte Jobs schicken
noch ausgegebene Geräte in den bestehenden physischen Rückgabeprozess.
Job, native Historie, Audit und dauerhafter Wiederholungsbeleg werden atomar
verbucht. Es werden keine externen Nachrichten oder Kalendereinladungen versandt.
Die Job-Detailabfrage zeigt auch Archive und exakte Versionen; die separate
Audit-Abfrage enthält weder rohe Audits noch Sync-IDs.

Rental-Migration 048 / Umbrella-Migration 034 installieren monoton steigende
Jobversionen für alle Schreiber, Schutz archivierter Inhalte einschließlich
Positionsgeräten und Paketreservierungen sowie die vorhandenen Personal-
Zuordnungstabellen auf frischen Installationen. Rental zuerst, MCP danach deployen.

## Kunden-/Venue-Feldänderung zurücknehmen — Rental 5.3.118 / MCP 1.5.37

`rental.customers` und `rental.venues` ergänzen
`prepare_revert_update` / `revert_update`. Der Katalog enthält 341 Werkzeuge
(95 Abfragen / 123 Vorschauen / 123 Ausführungen).

Zurücknehmbar ist ausschließlich die eigene letzte MCP-Feldänderung am noch
unveränderten aktiven Datensatz. Vorschauen zeigen Quellaudit, sämtliche
fachlichen Vorher-/Nachher-Felder und Diff. Ausführung benötigt den update-Scope,
aktuelle Adminrechte, `audit_id` aus `expected_audit_id`, genaue Datensatz- und
Kontextversion, die Vorschauphrase, Bestätigung und Idempotenz. Fremde, ältere,
zwischenzeitlich bearbeitete oder bereits rückgängig gemachte Änderungen bleiben
gesperrt. IDs, Lifecycle und Sync-Referenzen werden nicht zurückgeschrieben;
zusätzliche Feldänderungen im Undo-Aufruf sind nicht erlaubt.

Felder, neuer Audit mit `reverted_audit_id` und dauerhafter Beleg werden atomar
geschrieben. Historien zeigen den Auditverweis ohne private Feldinhalte.
Erfolgreiche Belege aus 5.3.117 bleiben über den Versionswechsel identisch
wiederholbar; offene Vorschauen müssen nach dem Upgrade neu erstellt werden.

Absichtlich verschiedene gleichnamige archivierte Datensätze lassen sich jeweils
nach expliziter Duplikatprüfung mit `allow_duplicate` unter ihrer ursprünglichen
ID wiederherstellen. Neuanlage aus einem exakten Archivtreffer bleibt gesperrt.

## Kunden und Venues — RentalCore 5.3.117 / MCP 1.5.36

`rental.customers` und `rental.venues` bieten `resolve`, `get`, redigierte
`audit_history` sowie Vorschau-/Ausführungspaare für `create`, `update`, `archive`
und `restore`. Der MCP-Katalog enthält 337 Werkzeuge
(95 Abfragen / 121 Vorschauen / 121 Ausführungen).

Anlage/Pflege umfasst alle fachlichen Namens-, Rollen-, Adress-, Kontakt- und
Notizfelder. Kunden benötigen eine Identität (Firma/Name oder Vor-/Nachname)
und mindestens eine Kunden-/Lieferantenrolle; Standard ist Kunde ohne
Lieferantenrolle. Kundentypen sind Unternehmen/Privat oder leer. Postleitzahlen
bleiben Strings; E-Mail-Adressen müssen gültige reine Adressen sein.
Schema/Werkzeugparameter dokumentieren alle Feldgrenzen. Teilupdates erhalten
weggelassene Felder; explizite leere optionale Strings leeren auf null.
Sync-IDs, Bankdaten, Benutzerkonten und private Mitarbeiterdaten sind keine
schreibbaren Stammdatenfelder.

Normale Abfragen/Resolver zeigen Identität, Ort, Lebenszyklus und Version ohne
Kontakte, Anschrift, Notizen oder Sync-IDs. Berechtigte Änderungsvorschauen
zeigen sämtliche fachlichen Felder, den Diff, passende Kandidaten und aktive
Jobreferenzen. Administratorrechte und der passende Rental create/update/archive-
Scope sind erforderlich. Der MCP delegiert den ausgewählten Scope in einem
kurzlebig signierten Suite-Token; Rental prüft diesen und die tatsächlichen
aktuellen Adminrechte auch vor einem historischen Replay erneut.

Ausführung benötigt `confirm_change`, Idempotenz, genaue `expected_updated_at`
bei bestehenden Datensätzen, vollständigen SHA-256-`expected_context` und die
record-/draftgebundene Vorschauphrase. Gleiche aktive Identitäten verlangen
explizite Prüfung und `allow_duplicate`; exakte archivierte Kandidaten müssen
wiederhergestellt werden. Keine automatische Neuanlage aus Archivtreffern.

Archivierung erhält sämtliche fachlichen Felder, IDs, Sync-Referenzen und
Historie; aktive Jobs blockieren sie. Wiederherstellung erhält alle Felder,
prüft Identität/Rollen/Duplikate erneut und ist von Metadatenpflege getrennt.
Rental `047` / Root `033` schützt auch bestehende Core-Schreiber: monotone
Mikrosekundenversionen, unveränderliche IDs, keine Archivbearbeitung oder
kombinierte Lifecycle-/Feldänderung und keine physische Löschung. Aktive Jobs
benötigen aktive Kunden/Venues; historische Jobs behalten ihre Referenzen.
Venue-Listen und Jobvorschläge zeigen aktive Datensätze. Die Root-Migration
enthält das Venue-Grundschema auch für eine leere gemeinsame Installation.

Datensatz, vollständiger Vorher-/Nachher-Audit und dauerhafter Replay sind eine
Transaktion. Auditfehler hinterlassen keine Teiländerung; derselbe Schlüssel
kann erneut versucht werden. Ein erfolgreicher Replay bleibt auch nach Neustart
identisch. Historien zeigen ausschließlich Aktion, Akteur, Zeitpunkt, Version
und Lebenszyklusänderung. Die MCP-Aktion versendet keine externen Nachrichten.
Issues #4/#5 bleiben bis zum Abschluss der übrigen Completion-Bereiche offen.

## Produktbeziehungen — WarehouseCore 5.9.105 / MCP 1.5.35

`warehouse.product_relations` ergänzt `search`, `get`, redigierte
`audit_history` und Vorschau-/Ausführungspaare für `create`, `update`, `archive`
und `restore`. Der Katalog enthält 315 Werkzeuge
(89 Abfragen / 113 Vorschauen / 113 Ausführungen).

Der Eigentümer bietet vollständige Feldpflege für `relation_type` (`required`,
`recommended`, `compatible`, `consumes`, `alternative`, `included`),
`assignment_scope` (`product`, `device`, `case`), `default_quantity` und `notes`.
Anlage standardisiert recommended/product/ein Stück; Teilupdates erhalten
weggelassene Felder, ein expliziter leerer Notizstring leert auf null.
Mengen sind positiv, höchstens 99999999.99, mit maximal zwei Nachkommastellen.
`is_optional` wird bei Anlage/Pflege aus der Beziehungsart abgeleitet.
Produktendpunkte und Beziehungs-ID bleiben unveränderlich. Eine weitere
Beziehungsart desselben Produktpaars ist eine geprüfte Änderung desselben
Datensatzes; identische Duplikate werden nicht angelegt.

Vorschauen zeigen sämtliche Beziehungsfelder, beide Produktversionen, den
vollständigen Diff, betroffene aktive Jobs und Geschäftseffekte. Ausführung
benötigt tatsächliche Adminrechte, create/update/archive-Scope, `confirm_change`,
Idempotenz, vollständigen SHA-256-`expected_context`, bei bestehenden Beziehungen
die genaue `expected_updated_at` sowie die recordgebundene Vorschauphrase.
Archiv/Restore erhält sämtliche Felder und Historie; Restore verlangt aktive
Produkte. Pflichtbeziehungen dürfen keine Zyklen bilden; reziproke
Kompatibilität/Alternativen bleiben möglich. Aktive Jobs des Quellprodukts oder
seiner Vorfahren in der Packlisten-Hierarchie blockieren Änderungen.

`warehouse.products.prepare_link_relation/link_relation` bleibt erhalten und
verwendet denselben Eigentümer. `expected_updated_at` bleibt die Quellprodukt-
Version; die neue Vorschau liefert zusätzlich `expected_relation_updated_at`
und `expected_context`, die bei Bestätigung ebenfalls zu übernehmen sind.
Archivierte Beziehungen ausdrücklich wiederherstellen. Lebenszyklusaktionen
ersetzen keine separate Metadatenpflege.

Warehouse `059` / Umbrella `032` erhalten Historie auch bei alten Core-Schreibern,
versionieren jede Beziehungsänderung und beide Produktendpunkte, und schützen
alle Produkt-Metadatenupdates mit monotonen Mikrosekundenversionen. Auch das
Archivieren eines in aktiven Jobs indirekt benötigten Produkts ist blockiert.
Alte UI-DELETE-Aktionen für Beziehungen sind gesperrt; stattdessen den neuen
geführten Archivpfad verwenden. Ein Archiv wird nicht physisch gelöscht.
Normale Warehouse-/Rental-Vorschläge,
Scanner und rekursive Packlisten verwenden ausschließlich aktive Beziehungen
und Produkte. RentalCore 5.3.116 integriert diesen Filter; neue gemeinsame
Installationen und Upgrades benötigen die aktuelle Warehouse-Schema-Version.

Beziehung, Produktversionen, vollständiger Vorher/Nachher-Audit und dauerhafter
Replay sind atomar. Auditfehler lassen keine Teiländerung zurück; derselbe
Schlüssel kann erneut versucht werden. Erfolgreiche Wiederholung ist auch nach
Neustart identisch. Geschäftsbestand und bestehende Jobanforderungen bleiben
unverändert; die Beziehung steuert Vorschläge und zukünftige Packlisten-
Expansion. Historien schließen alte `product.relation.link`-Audits ein und
geben weder Notizen noch rohe Audit-JSONs aus. Issues #4/#5 bleiben bis zum
Abschluss aller Bereiche der Completion-Liste offen.


## Kategorie-Lifecycle — WarehouseCore 5.9.104 / Cores MCP 1.5.34

Alle drei Ebenen (`warehouse.categories`, `warehouse.subcategories`,
`warehouse.third_categories`) bieten `prepare_archive/archive`,
`prepare_restore/restore` und redigierte `audit_history`. Der Katalog enthält
304 Werkzeuge (86 Abfragen / 109 Vorschauen / 109 Ausführungen).

Archivierung erhält IDs, Namen, Abkürzungen, Elternzuordnung und historische
Produktbeziehungen. Aktive Produkte oder Unterkategorien blockieren sie,
auch über die gesamte untergeordnete Hierarchie. Zuerst Produkte und untere
Ebenen archivieren. Restore verlangt aktive Eltern und passende, eindeutige
Stammdaten; zuerst die Hauptkategorie, dann Unterkategorie und dritte Ebene
wiederherstellen. Restore ändert ausschließlich den Lifecycle. Archivierte
Datensätze vor Metadatenpflege wiederherstellen. Normale Core-Auswahllisten
bieten aktive Hierarchien; MCP-Auflösung zeigt Archive und verlangt deren
Wiederherstellung statt stiller Neuanlage.

Vorschauen zeigen sämtliche gespeicherten Felder, Lifecycle-Diff, Eltern und
aktive/historische Abhängigkeiten. Ausführung benötigt tatsächliche aktuelle
Warehouse-Adminrechte, archive-Scope, `confirm_lifecycle`, Idempotenz, die genaue
`expected_updated_at` und `expected_dependencies` aus der letzten Vorschau sowie
`ARCHIVE|RESTORE WAREHOUSE CATEGORY|SUBCATEGORY|THIRD_CATEGORY <ID>`.
Hauptkategorie-IDs sind kanonische positive Integer-Strings; beide unteren
Ebenen behalten ihre exakten String-IDs (höchstens 50 Zeichen).

Der SHA-256-Abhängigkeitskontext bindet auch archivierte Produkte,
Unterkategorien und Elternversionen. Änderungen nach der Vorschau verlangen
neue Prüfung. Eigentümer-API `/api/v1/admin/mcp/{entity}/{archive|restore}` friert
alle beteiligten Tabellen während der Validierung ein. Lifecycle, vollständiger
Vorher/Nachher-MCP/AI-Audit und dauerhafter Replay sind eine Transaktion.
Auditfehler rollen alles zurück; derselbe Schlüssel kann anschließend erneut
versucht werden. Erfolgreiche Wiederholung bleibt auch nach Neustart identisch.

Warehouse `058` / Umbrella `031` schützen auch bestehende UI-Schreiber gegen
Archivbearbeitung, Referenzen auf inaktive Hierarchien, widersprüchliche aktive
Produktpfade und das Löschen referenzierter Historie. Die ausdrücklich erlaubte
Löschung unbenutzter Kategorien bleibt als getrennte delete-Scope-Aktion mit
Version, Abhängigkeitsprüfung und recordgebundener Bestätigung erhalten.
Die neuen Historien liefern ausschließlich ausgewählte Metadaten und keine
rohen Audit-JSONs. Vollständige Race-/PostgreSQL-Tests, Vet/Build und frische
Streamable-HTTP-Prüfung gehören zur Release-Verifikation. Issues #4/#5 bleiben
bis zum Abschluss sämtlicher Bereiche der Completion-Liste offen.


## Geführte MCP-Inventur — WarehouseCore 5.9.103 / Cores MCP 1.5.33

Die Implementierung ergänzt `warehouse.inventory_counts` mit `search`,
`get`, redigierter `audit_history` und neun benannten Vorschau-/Ausführungspaaren:
`create`, `update`, `set_lines`, `review`, `return_for_counting`, `approve`, `cancel`,
`archive`, `restore`. Der Katalog enthält damit 289 Werkzeuge
(83 Abfragen / 103 Vorschauen / 103 Ausführungen).

Alle Änderungen erfordern aktuelle Adminrechte, passenden Aktionsscope,
`confirm_change`, Idempotenz und den vollständigen `expected_context` aus der
Vorschau. Bestehende Zählungen zusätzlich die exakte `expected_updated_at`;
Zeilen und Ereignisse versionieren ihre Zählung bei allen Schreibern.
Anlage benötigt create, Archiv/Restore archive, übrige Pflege update.
**Freigabe benötigt ausdrücklich `cores:warehouse:approve`; create/update und
Legacy `cores:write` erteilen diese Berechtigung nicht.** Die bestehende
OAuth-Freigabe muss diesen Scope ausdrücklich anfordern und gewähren.
Review, Freigabe, Storno und Lifecycle benötigen außerdem die recordgebundene
Phrase `<OPERATION> WAREHOUSE INVENTORY COUNT <ID>`.

`set_lines` ersetzt 1–100 eindeutige Mengen statt Scans zu addieren. Eine Zeile
nennt `item_type`, exakten `item_key` und entweder `counted_quantity` oder
`clear_counted`. Mengen sind 0–9999999.999 mit maximal drei Nachkommastellen;
Geräte und Cases ausschließlich 0 oder 1. Eine Zählung enthält maximal 1000
Zeilen und einen vollständigen Kontext unter zwei MiB. Der Lagerplatz bleibt
unveränderlich. `blind_count` (Standard true) und Arbeitsnotizen sind Teilupdates;
Notizen werden durch einen expliziten leeren String geleert.

Blinde Zählungen verbergen Sollmengen und Differenzen bis zur bestätigten
Prüfphase, einschließlich der bisherigen Varianzabfrage und verfrühter
Freigabeversuche. Review setzt fehlende Zeilen nur nach ausdrücklicher
`mark_uncounted_zero`-Bestätigung auf null Stück; ansonsten müssen alle Zeilen
gezählt sein. `return_for_counting` erlaubt Korrekturen vor der Freigabe und
benötigt einen Grund. Zählen/Review/Korrektur buchen keinen Bestand.

Die Freigabe prüft den unveränderten Startbestand und den aktuellen vollständigen
Kontext, aktive Artikel/Case-Inhalte, verfügbare Ziel-/Quellhierarchien, Aufträge,
Reservierungen, Packzuordnungen, Wartung/Defekte und Lagerprofile. Ihre Vorschau
zeigt jede Differenz, Geräte-/Case-Lageränderung und projizierte Stück-, Gewichts-
und Volumenbelegung. Produkt-/Case-Maße sind Zentimeter, Gewicht Kilogramm.
Gepackte Cases belegen ihren äußeren Raum; Gewichte enthalten alle verschachtelten
Cases, Geräte und Mengenartikel. Fehlende Maße/Gewichte blockieren gesetzte
physische Limits. Nur das bestehende Kapazitätsmodell `item_count` ist erlaubt.
Ein geänderter Startbestand verlangt Storno und eine neue Zählung.

Erst die gesonderte bestätigte Freigabe schreibt Mengenbestände, Gerätebewegungen,
Case-Ereignisse und ein Differenzjournal. Gepackte Inhalte bleiben gepackt und
folgen der Wurzelposition; Gerätezustände (`condition_status`) sowie Pack-/Jobzuordnungen bleiben erhalten.
Ein unerwarteter Artikel mit null gezählten Stück bleibt an seinem Quellort.
Bestand, globale Mengensummen, Zählung, Lagertermin, Ereignisse, Vorher/Nachher-
Audits und dauerhafter Replay sind eine Transaktion. Storno benötigt einen Grund
und löst ausschließlich den Zählstatus. Nur abgeschlossene/stornierte Zählungen
können archiviert werden. Restore erhält den terminalen Status und sämtliche
Historie; eine erneute Inventur ist eine neue Zählung.

Warehouse-Startup/Migration `057` und Umbrella `030` ergänzen das kanonische
Schema und schützen Versionen, Zeilen, Archive und unveränderliche Startbestände.
Die bisherige UI kann eine MCP-Zählung lesen und zählen; ihre alte, ungeprüfte
Freigabe ist für solche Zählungen blockiert. Dafür den neuen geprüften MCP-Pfad
verwenden. Bestehende UI-Zählungen ohne Startbaseline benötigen für MCP-Abgleich
Storno und Neuanlage. Normale Zähllisten blenden Archive aus.

Gezielte Race-Tests prüfen Autorisierung, Feldweiterleitung, Dry-run und
Wiederholung nach Auditfehlern. Der PostgreSQL-Integrationstest prüft Konflikte,
blinde Zählung, physische Freigabefolgen, vollständigen Rollback bei der letzten
Auditbuchung, identische Wiederholung und Archive/Restore. Vollständige Go-Tests,
Vet/Build und eine neue Datenbank mit dem echten MCP-Endpunkt gehören zur
Release-Prüfung. Die Parent-Issues #4 und #5 bleiben für weitere Bereiche offen.

## Atomare MCP-Lageraufgaben — WarehouseCore 5.9.102 / Cores MCP 1.5.32

`warehouse.tasks` unterstützt vollständige Anlage und Teilupdates, `start`,
`complete`, `cancel`, `reopen`, `archive`, `restore` samt `prepare_*`, `search`
und redigierter `audit_history`. Das Schema beschreibt Typ, Priorität 0–100
(Standard 50), Quelle/Ziel, Case, Gerät, Produkt/Menge, Job, Zuständigkeit,
Termin und Arbeitsnotizen. `clear_fields` leert optionale Werte ausdrücklich;
mindestens eine fachliche Referenz bleibt erforderlich. Geräte-/Produktbezug
muss zusammenpassen; Seriengeräte haben bei angegebener Menge genau ein Stück.
Mengen besitzen höchstens drei Nachkommastellen, Termine eine explizite Zeitzone.

Admin und create/update/archive-Scope, vollständige Vorschau/Diff und Idempotenz
sind erforderlich. Die Anlage behält `confirm_creation`; übrige Aktionen verwenden
`confirm_change` und die genaue `expected_updated_at`. Alle bestätigten Aktionen
verlangen die vollständige `expected_references` aus der Vorschau, einschließlich
ersetzter Verknüpfungen. Aktive Arbeit prüft aktive Geräte/Produkte/Cases/Zonen,
offene Jobs und aktive Zuständigkeiten. Änderungen an referenzierten Datensätzen
oder Ereignissen lassen eine ältere Vorschau scheitern.

Abschluss, Storno, Wiederöffnung und Lifecycle benötigen die Phrase
`COMPLETE|CANCEL|REOPEN|ARCHIVE|RESTORE WAREHOUSE TASK <ID>`; Storno/Wiederöffnung
zusätzlich einen Grund. Nur terminale Aufgaben können archiviert werden. Restore
erhält terminalen Status und Historie, auch wenn Stammdaten inzwischen archiviert
sind; Wiederöffnung prüft aktive Referenzen erneut. Normale Aufgabenlisten blenden
Archive aus. Arbeitsnotizen und Ereignisgründe werden in der Historienabfrage
redigiert; Actor, Herkunft, Zeitpunkt, Zustände und Versionen bleiben abrufbar.

Aufgabe, Ereignis, Referenzversionen, Vorher/Nachher-Audits und dauerhafter Replay
sind atomar. **Aufgabenabschluss quittiert Arbeit und bucht keinen Lagerbestand.**
Physische Geräte-/Case-/Mengenbewegungen bleiben eigene bestätigte Werkzeuge.
Migration Warehouse `056` / Umbrella `029` schützt Archive und versioniert alle
Aufgaben-/Ereignisschreiber sowie betroffene Geräte, Cases, Produkte, Zonen und
Jobs. Umbrella-Neuinstallationen erhalten das kanonische Aufgabenschema.
Fehlgeschlagene Aufgaben-/Wartungsaufrufe lassen sich mit demselben Schlüssel
wiederholen; der atomare Owner-Replay schützt auch nach Transportfehlern.

268 Tools: 80 Abfragen, 94 Vorschauen, 94 Ausführungen. Keine neue Konfiguration.
Inventur und weitere Anforderungen von #4/#5 bleiben im Abschlusscheck offen.

## MCP-Wartungsaufträge und Defekte — WarehouseCore 5.9.101 / Cores MCP 1.5.31

`warehouse.maintenance_orders` und `warehouse.defects` unterstützen `search`,
`prepare_create`/`create`, `prepare_update`/`update`, `prepare_transition`/`transition`,
`prepare_complete`/`complete`, `prepare_cancel`/`cancel`, `prepare_reopen`/`reopen`,
`prepare_archive`/`archive`, `prepare_restore`/`restore` und redigierte
`audit_history` einschließlich Ereignissen. Defektaktionen verwenden kanonische
`order_id`; `legacy_defect_id` ist eine getrennte historische Referenz. Schemas
beschreiben Gerät/Plan, Typ, Priorität, Titel, Beschreibung, Termin/Zeit,
Zuständigkeit, Ergebnis/Abschlussbericht und optionale genaue Dezimalkosten.
Teilupdates erhalten ausgelassene Felder; `clear_fields` leert optionale Werte.
Gerät, Typ und Planbezug bleiben unveränderlich.

Admin/create/update/archive-Scope, vollständige Vorschau mit Diff, genaue
Auftrags-/Geräteversion sowie bei Planbezug die genaue Planversion,
`confirm_change` und Idempotenz sind erforderlich. `transition` bildet den
Core-Statusgraphen ab; Abschluss setzt begonnene Arbeit, Ergebnis und Bericht
voraus. Abschluss, Storno, Wiederöffnung und Lifecycle verlangen zusätzlich
`COMPLETE|CANCEL|REOPEN|ARCHIVE|RESTORE WAREHOUSE MAINTENANCE ORDER <ID>`.
Storno/Wiederöffnung benötigen einen Grund. Nur terminale Aufträge lassen sich
archivieren; Restore erhält Status und Historie, Wiederöffnung erfolgt separat.

Die Vorschau zeigt Gerätezustand/-termine, Planfortschreibung, andere offene
Aufträge und migrierte Defekte. Auftrag, Ereignis, alle Geräte-/Plan-/Legacy-
Folgen, Vorher/Nachher-Audits und dauerhafter Replay bilden eine Transaktion.
Storno eines wiederkehrenden Auftrags überspringt den Zyklus; Abschluss setzt
den nächsten Plantermin auf das gewählte Datum oder heute plus Intervall.
Manuelle Sperre/Ausmusterung und physischer Lagerstatus bleiben erhalten.
Migration Warehouse `055` / Umbrella `028` schützt Archive und versioniert
Ereignisse und Abhängigkeiten bei allen Schreibern; die normale Auftragsliste
blendet Archive aus. Historische Legacy-Zeilen und IDs werden erhalten.

Wartungskosten erfordern **zusätzlich ausdrücklich** `cores:warehouse:financial`.
Legacy `cores:write` erteilt diesen Scope nicht. `cost_amount` ist eine genaue
Dezimalzeichenfolge bis `9999999999.99`; Kostenlesen erfolgt über
`warehouse.maintenance_orders.financial_get`. Ohne Financial-Scope enthalten
Vorschauen, Ergebnisse und Wiederholungen keine Kosten. Flexible Projektionen,
Filter, Sortierung und Aggregate auf Wartungskosten prüfen denselben Scope;
Standardabfragen und Geräte-Wartungshistorien lassen Kosten weg.
OAuth bietet angefragte Wartungskosten als unabhängigen, standardmäßig gesperrten
Select an, auch bei Read-only-Konfiguration. Ein Clientwunsch allein gewährt
keinen Kostenzugriff. Änderungen benötigen weiterhin den Aktionsscope und
Schreibmodus. Bestehende Tokens erhalten keinen zusätzlichen Scope automatisch. Aktuelle
Adminrechte werden vor jeder authentifizierten MCP-Anfrage erneut gelesen;
ein Rechteentzug sperrt auch zuvor gecachte Antworten.

252 Tools: 78 Abfragen, 87 Vorschauen, 87 Ausführungen. Keine neue Umgebungsvariable.
Read-only-Zugriff bleibt unverändert schreibfrei. Race-Tests, saubere Datenbank,
MCP-HTTP-Kontrollen sowie DE/EN, Light/Dark, responsive Tastaturbedienung sind geprüft.
Weitere Anforderungen von #4/#5 bleiben im Abschlusscheck offen.

Seit 1.1.1 sind alle Planner-Ergebnisse auf die aktuellen Planmitgliedschaften des
OAuth-Benutzers beschränkt. Das gilt auch für `cores.search`,
`cores.operations.overview`, `cores.activity.recent`, `cores.data.quality`,
`cores.query.records` und `cores.query.aggregate`. Administratorstatus allein
berechtigt nicht zum Lesen fremder Pläne. Maschinentokens liefern keine Planner-Daten.
Toolnamen und Eingabeschemas bleiben unverändert.

Die 67 Abfragetools sind read-only und idempotent. Bei `MCP_ENABLE_WRITES=true` werden zusätzlich 58 vorbereitende und 58 bestätigte Schreibtools für alle vier Core-Services registriert. Suchtools akzeptieren üblicherweise `query`, `limit` und `offset`; Zeitfenster `from`, `to` und `limit`; Detailtools `id`. Datumswerte sind `YYYY-MM-DD` oder RFC3339.

Der MCP-Endpunkt verlangt immer `cores:read`. Für ein Vorbereitung- oder
Ausführungstool ist zusätzlich entweder der kompatible Sammel-Scope
`cores:write` oder der passende granulare Scope erforderlich:

| Scope | Freigegebene Operationen |
|---|---|
| `cores:rental:create` | Jobs und Requirements anlegen |
| `cores:rental:update` | Job/Requirement ändern und Gerät zuweisen |
| `cores:warehouse:create` | Tasks, Produkte, Hersteller, Marken, Kategorien und Lagerplätze anlegen |
| `cores:warehouse:update` | Bewegungen und Gerätezustände buchen sowie Produkte, ihre Beziehungen, Hersteller, Marken, alle Kategorieebenen und aktive Lagerplätze ändern |
| `cores:warehouse:delete` | Ungenutzte Warehouse-Kategorien aller drei Ebenen dauerhaft entfernen; Adminrechte und zusätzliche datensatzgebundene Bestätigung erforderlich |
| `cores:warehouse:archive` | Produkte, Geräte, Produktpakete und Lagerplätze nach Abhängigkeitsprüfung archivieren oder wiederherstellen |
| `cores:planner:create` | Pläne und Tasks anlegen |
| `cores:procurement:create` | Produkte, Angebote, Lieferanten, Kategorien und Bestellungen anlegen |
| `cores:procurement:update` | Produkte, Angebote, Lieferanten, Kategorien, Bedarfs- und Bestellentwürfe ändern sowie Produkte verknüpfen |
| `cores:procurement:approve` | Eingereichte Bedarfe im Vier-Augen-Prinzip entscheiden und Bestellstatus mit erhöhter Bestätigung ändern |
| `cores:procurement:submit` | Eigenen Bedarfsentwurf zur Entscheidung einreichen |
| `cores:procurement:receive` | Bestätigten Wareneingang mit Lagerwirkung buchen |

Ohne Schreibscope bleibt das Token read-only, auch wenn
`MCP_ENABLE_WRITES=true` gesetzt ist. Die Rechte des interaktiven Cores-Nutzers
im Zielservice gelten zusätzlich und werden nicht erweitert.

## Suiteweit, flexible Abfragen und Wissen (13)

| Tool | Zweck |
|---|---|
| `cores.operations.overview` | Aktuelles Lagebild über Jobs, Bestand, Defekte, Wartung, Aufgaben und Beschaffung |
| `cores.search` | Gemeinsame Suche über Produkte, Geräte, Jobs, Tasks, Lieferanten, Bestellungen, Orte und Cases |
| `cores.activity.recent` | Letzte Änderungen aus allen Cores in einem Zeitfenster |
| `cores.services.health` | Live-Healthchecks aller Suite-Dienste |
| `cores.data.dictionary` | Tabellen- und Spalteninventar der freigegebenen Cores-Daten |
| `cores.entities.schema` | Pflegbare Felder, Typen, Validierungen und geführte Operationen pro Schreibentität |
| `cores.master_data.resolve` | Hersteller, Marken, Kategorien, Zonen, Procurement-Lieferanten und Rental-Kunden/Orte fuzzy und lesend auflösen |
| `cores.data.quality` | Entscheidungsrelevante Erfassungs- und Verknüpfungslücken |
| `cores.query.catalog` | Freigegebene Entitäten, Felder, Typen, Operatoren und Cross-Core-Beziehungen |
| `cores.query.records` | Bis zu acht gefilterte Entitätsabfragen ausführen und Ergebnisse sicher verknüpfen |
| `cores.query.aggregate` | Eine freigegebene Entität gruppieren und mit Count, Summe, Durchschnitt, Min oder Max aggregieren |
| `knowledge.documents.list` | Verfügbare Strategie- und Referenzdokumente |
| `knowledge.documents.search` | Volltextsuche in freigegebenen Knowledge-Dokumenten |

Bei `cores.entities.schema` kennzeichnet `required_one_of` alternative
Pflichtangaben wie `manufacturer_id` oder `manufacturer_name`; die einzelnen
Felder sind dabei nicht jeweils für sich erforderlich.

### Flexible Query-Syntax

`cores.query.records` unterstützt 19 kuratierte Entitäten aus allen vier Cores. Jede Query besitzt einen Alias, eine Entität sowie optional `search`, `fields`, `filters`, `sort`, `limit` und `offset`. Filter werden mit AND kombiniert. Unterstützte Operatoren sind `eq`, `ne`, `contains`, `not_contains`, `prefix`, `gt`, `gte`, `lt`, `lte`, `in`, `not_in`, `between`, `is_null` und `is_not_null`.

Joins referenzieren zwei Query-Aliasse. Mit `relationship` wird eine Beziehung aus `cores.query.catalog` verwendet; alternativ dürfen zwei im Katalog freigegebene Felder mit `left_field` und `right_field` verbunden werden. `inner` und `left` werden unterstützt. Join-Felder werden automatisch in die Projektion aufgenommen.

Beispiel für Jobs samt Anforderungen und Lagerprodukt:

```json
{
  "queries": [
    {
      "alias": "jobs",
      "entity": "rental.jobs",
      "filters": [{"field": "start_date", "operator": "between", "values": ["2026-10-01", "2026-10-31"]}],
      "sort": [{"field": "start_date", "direction": "asc"}]
    },
    {
      "alias": "needs",
      "entity": "rental.requirements",
      "filters": [{"field": "start_date", "operator": "between", "values": ["2026-10-01", "2026-10-31"]}],
      "limit": 200
    },
    {"alias": "products", "entity": "warehouse.products", "limit": 200}
  ],
  "joins": [
    {"alias": "job_needs", "left": "jobs", "right": "needs", "relationship": "rental.job_requirements", "type": "left"},
    {"alias": "need_products", "left": "needs", "right": "products", "relationship": "inventory.requirement_product"}
  ]
}
```

`cores.query.aggregate` verwendet dieselben Such- und Filterregeln. `group_by` akzeptiert bis zu vier Felder; `metrics` bis zu acht Ausdrücke mit optionalem Alias. `sum` und `avg` sind nur für numerische Felder erlaubt. Ohne `metrics` wird `record_count` ausgegeben.

Alle Query-Namen und Felder stammen aus einer festen Registry. Nutzwerte werden ausschließlich als PostgreSQL-Parameter gebunden. Resultate bleiben durch das globale Zeilenlimit, Query-Limits, Read-only-Transaktionen und Statement-Timeouts begrenzt.

## RentalCore (9 + 10 geführte Schreibtools)

| Tool | Zweck |
|---|---|
| `rental.jobs.search` | Jobs nach Code, Beschreibung, Kunde, Ort oder Status suchen |
| `rental.jobs.get` | Vollständigen Jobkontext mit Material, Geräten, Paketen, Fremdmiete und Personal lesen |
| `rental.jobs.upcoming` | Überlappende Jobs in einem Zeitfenster |
| `rental.requirements.list` | Produktmengen pro Job und Zeitraum |
| `rental.customers.search` | Kundenstammdaten ohne private Kontaktdaten |
| `rental.venues.search` | Veranstaltungsorte und Nutzung |
| `rental.revenue.summary` | Geplanter und finaler Umsatz nach Monat und Status |
| `rental.staffing.requirements` | Personalzuordnung und Skills für Jobs |
| `rental.external_equipment.list` | Fremdmietkatalog, Nutzung und historische Kosten |
| `rental.jobs.prepare_create` | Kunde, Status, Kategorie, Ort und Datumswerte auflösen; konkrete Rückfragen liefern |
| `rental.jobs.create` | Einen bestätigten, vollständig aufgelösten Job über die RentalCore-API anlegen |
| `rental.requirements.prepare_create` | Job/Produkt auflösen, Gesamt-/manuelle/Positionsmengen, Duplikate und exakte Job-/Kontextversion prüfen |
| `rental.requirements.create` | Einen bestätigten Bedarf mit Owner-Rechten, Versionen, nativer Historie, Audit und dauerhaftem Beleg atomar anlegen |
| `rental.jobs.prepare_assign_device` | Job und aktives Gerät auflösen sowie vorhandene Zuweisung prüfen |
| `rental.jobs.assign_device` | Ein bestätigtes Gerät einem Job zuweisen |
| `rental.jobs.prepare_update` | Aktuellen Job laden, neue Referenzen/Termine prüfen und Stornofolgen anzeigen |
| `rental.jobs.update` | Jobdaten oder Status bestätigt ändern; Storno ersetzt keine Löschung |
| `rental.requirements.prepare_update` | Gesamte/manuelle Menge, geschützte Positionsanteile, Geräte und genaue Zeilen-/Job-/Kontextversion prüfen |
| `rental.requirements.update` | Bestätigte Gesamt-/manuelle Menge mit unveränderlicher Identität und atomarer Historie/Audit/Replay ändern |
| `rental.requirements.get/search` | Vollständige Mengen, Identität und Versionen lesen; Archive ausdrücklich kennzeichnen |
| `rental.requirements.prepare_archive/archive` | Bedarfszeile unter Erhalt der Mengen archivieren; Positionen/zugeordnete Geräte blockieren |
| `rental.requirements.prepare_restore/restore` | Originalzeile nach Prüfung von Job, Produkt und Positionsquellen wiederherstellen |
| `rental.requirements.audit_history` | Redigierte Mengen-/Lifecycle-Historie der konkreten Zeile lesen |

## WarehouseCore (18 + 30 geführte Schreibtools)

| Tool | Zweck |
|---|---|
| `warehouse.products.search` | Produktstamm, Hersteller, Kategorie, Stock und Geräteanzahl suchen |
| `warehouse.products.get` | Produktdetail mit Geräten, Orten, Relationen, Bedarf und Beschaffungslink |
| `warehouse.audit.history` | Redigierte Produkt-Historie für Warehouse-Administratoren mit Aktion, Herkunft und ausgewählten Vorher-/Nachher-Feldern |
| `warehouse.products.relations` | Alternativen, Zubehör und Abhängigkeiten |
| `warehouse.master_data.resolve` | Hersteller, Marken, Kategorien, Einheiten und Lagerplätze fuzzy auflösen und Trefferart klassifizieren |
| `warehouse.manufacturers.prepare_create` | Herstellername, Website, identische und ähnliche Stammdaten prüfen |
| `warehouse.manufacturers.create` | Hersteller eigenständig mit Audit und Idempotenz anlegen; ID für ein bestehendes Produkt zurückgeben |
| `warehouse.brands.prepare_create` | Marke, vorhandenen Hersteller, identische und ähnliche Namen prüfen |
| `warehouse.brands.create` | Marke eigenständig mit Audit und Idempotenz anlegen; ID für ein bestehendes Produkt zurückgeben |
| `warehouse.categories.prepare_create` | Hauptkategorie mit Abkürzung, identische und ähnliche Namen prüfen |
| `warehouse.categories.create` | Bestätigte Hauptkategorie mit Audit und Idempotenz anlegen |
| `warehouse.subcategories.prepare_create` | Elternkategorie und Namenskonflikte innerhalb der Hauptkategorie prüfen |
| `warehouse.subcategories.create` | Bestätigte Unterkategorie mit Audit und Idempotenz anlegen |
| `warehouse.third_categories.prepare_create` | Eltern-Unterkategorie und Namenskonflikte der dritten Ebene prüfen |
| `warehouse.third_categories.create` | Bestätigte dritte Kategorieebene mit Audit und Idempotenz anlegen |
| `warehouse.categories.prepare_update` | Hauptkategorie: vollständigen Diff, Version, Dubletten und Produktzuordnungen prüfen |
| `warehouse.categories.update` | Bestätigte Änderung versionsgesichert, auditiert und idempotent speichern |
| `warehouse.categories.prepare_delete` | Hauptkategorie: ganzen Datensatz, Produkt-/Kinderanzahl und genaue Löschphrase zeigen |
| `warehouse.categories.delete` | Nur ungenutzten Datensatz ohne Kinder dauerhaft entfernen; kein Cascade, Audit bleibt erhalten |
| `warehouse.subcategories.prepare_update` | Unterkategorie: vollständigen Diff, Version, Dubletten und Produktzuordnungen prüfen |
| `warehouse.subcategories.update` | Bestätigte Änderung versionsgesichert, auditiert und idempotent speichern |
| `warehouse.subcategories.prepare_delete` | Unterkategorie: ganzen Datensatz, Produkt-/Kinderanzahl und genaue Löschphrase zeigen |
| `warehouse.subcategories.delete` | Nur ungenutzten Datensatz ohne Kinder dauerhaft entfernen; kein Cascade, Audit bleibt erhalten |
| `warehouse.third_categories.prepare_update` | Dritte Kategorieebene: vollständigen Diff, Version, Dubletten und Produktzuordnungen prüfen |
| `warehouse.third_categories.update` | Bestätigte Änderung versionsgesichert, auditiert und idempotent speichern |
| `warehouse.third_categories.prepare_delete` | Dritte Kategorieebene: ganzen Datensatz, Produkt-/Kinderanzahl und genaue Löschphrase zeigen |
| `warehouse.third_categories.delete` | Nur ungenutzten Datensatz ohne Kinder dauerhaft entfernen; kein Cascade, Audit bleibt erhalten |
| `warehouse.locations.prepare_create` | Lagerplatzfelder, aktiven Elternknoten, Code, Scan-Code und ähnliche Orte prüfen |
| `warehouse.locations.create` | Bestätigten Lagerplatz mit Audit und dauerhaftem Idempotenzbeleg anlegen |
| `warehouse.manufacturers.prepare_update` | Namen und Website mit vollständigem Diff, Version, Dubletten und verknüpften Datensatzanzahlen prüfen |
| `warehouse.manufacturers.update` | Bestätigte Herstellerpflege versionsgesichert, auditiert und idempotent speichern |
| `warehouse.brands.prepare_update` | Namen und Herstellerzuordnung samt Produktabhängigkeiten prüfen |
| `warehouse.brands.update` | Bestätigte Markenpflege speichern; widersprüchliche Produktzuordnungen blockieren den Herstellerwechsel |
| `warehouse.locations.prepare_update` | Vollständigen Lagerplatz-Diff, exakte Version, Duplikate, Elternhierarchie und Belegung prüfen |
| `warehouse.locations.update` | Aktiven Lagerplatz nach Bestätigung versionsgesichert, auditiert und dauerhaft idempotent ändern |
| `warehouse.devices.search` | Geräte nach ID, Seriennummer, Barcode, Produkt oder Zustand suchen |
| `warehouse.devices.get` | Gerätehistorie, Jobs, Bewegungen, Defekte und Wartung |
| `warehouse.stock.shortages` | Mindestbestands- und Verfügbarkeitsengpässe |
| `warehouse.defects.open` | Offene Defekte nach Schwere und Status |
| `warehouse.maintenance.due` | Überfällige und anstehende Wartungen |
| `warehouse.locations.list` | Lagerzonen, Kapazität, Bestand und Zählplan |
| `warehouse.cases.search` | Cases/Handling Units samt Inhalt und Workflow |
| `warehouse.tasks.open` | Offene Lageraufgaben |
| `warehouse.inventory.variances` | Inventurdifferenzen |
| `warehouse.movements.recent` | Gerätebewegungen im Zeitfenster |
| `warehouse.cables.search` | Kabelbestand nach Steckern, Typ und Länge |
| `warehouse.packages.search` | Produktpakete, Mengen, Preise und Jobnutzung |
| `warehouse.utilization.summary` | Gerätenutzung, Umsatz, Ausfälle und Defekte nach Produkt |
| `warehouse.tasks.prepare_create` | Aufgabentyp, Priorität, Fälligkeit und referenzierte Live-Datensätze prüfen |
| `warehouse.tasks.create` | Eine bestätigte Lageraufgabe über die WarehouseCore-API anlegen |
| `warehouse.products.prepare_create` | Vollständigen Produktentwurf, Stammdatenplan, ähnliche Artikel und offene Pflicht-/Empfehlungsfelder liefern |
| `warehouse.products.create` | Bestätigte Stammdaten, Produkt, Anfangsbestand und Devices atomar über WarehouseCore anlegen |
| `warehouse.products.prepare_update` | Alle bearbeitbaren Produktfelder, Beziehungen, Duplikate und Version prüfen; vollständigen Ist/Soll-Diff zeigen |
| `warehouse.products.update` | Bestätigte Produktänderung mit Versionsprüfung, Audit und dauerhaftem Idempotenzbeleg speichern |
| `warehouse.products.prepare_archive` | Produktversion, betroffene Geräte und aktive Jobverwendungen vor der Archivierung prüfen |
| `warehouse.products.archive` | Produkt und Geräte nach Abhängigkeitsprüfung mit erhöht bestätigtem, idempotentem API-Aufruf archivieren |
| `warehouse.products.prepare_restore` | Archiviertes Produkt, Version und wiederherstellbare Geräte prüfen |
| `warehouse.products.restore` | Produkt und zuvor mitarchivierte Geräte mit erhöht bestätigtem, idempotentem API-Aufruf wiederherstellen |
| `warehouse.products.prepare_link_relation` | Zwei aktive Produkte, bestehende Beziehung, vollständigen Diff und genaue Version prüfen |
| `warehouse.products.link_relation` | Typisierte Produktbeziehung nach Bestätigung versionsgesichert und auditiert anlegen oder ändern |
| `warehouse.movements.prepare_create` | Gerät/Mengenartikel sowie Einlagerung, Ausgabe oder Transfer mit Ziel prüfen |
| `warehouse.movements.create` | Bestätigte physische Bewegung über den auditierten Scannerprozess buchen |
| `warehouse.devices.prepare_update_status` | Aktuellen physischen und betrieblichen Gerätezustand samt Prozessgrenzen prüfen |
| `warehouse.devices.update_status` | Bestätigten, manuell zulässigen Gerätezustand ändern |

## PlannerCore (8 + 4 geführte Anlage-Tools)

| Tool | Zweck |
|---|---|
| `planner.plans.search` | Aktive Pläne suchen |
| `planner.plans.get` | Plan mit Buckets, Tasks, Abhängigkeiten, Zielen und Sprints |
| `planner.tasks.search` | Aufgaben nach Inhalt, Plan, Bucket, Status oder Priorität |
| `planner.tasks.overdue` | Überfällige offene Aufgaben |
| `planner.dependencies.list` | Blockierende Aufgabenabhängigkeiten |
| `planner.goals.list` | Ziele, Fortschritt und Termin |
| `planner.sprints.list` | Sprints samt Taskfortschritt |
| `planner.workload.summary` | Offene Arbeit nach Zuständigem, Plan und Priorität |
| `planner.plans.prepare_create` | Planname und mögliche Duplikate prüfen |
| `planner.plans.create` | Einen bestätigten Plan über die PlannerCore-API anlegen |
| `planner.tasks.prepare_create` | Planmitgliedschaft, Bucket-Zugehörigkeit, Titel und Duplikate prüfen |
| `planner.tasks.create` | Eine bestätigte Aufgabe im freigegebenen Plan anlegen |

## ProcurementCore (12 + 34 geführte Schreibtools)

| Tool | Zweck |
|---|---|
| `procurement.products.search` | Beschaffungsprodukte tolerant suchen und mit fachlichem Kontext statt nur Codes erklären |
| `procurement.products.get` | Produkt mit Angeboten, Historie, Bedarf, Orders und Lagerlink |
| `procurement.products.prepare_update` | Produktfelder, Kategorie, Duplikate, offene Referenzen und exakte Version prüfen; vollständigen Diff zeigen |
| `procurement.products.update` | Bestätigte Produktänderung oder Archivierung mit Versionsprüfung, Audit und dauerhafter Idempotenz speichern |
| `procurement.offers.prepare_create` | Produkt, Lieferant, Preis, Packung, Währung, Ablaufdatum und vorhandene Angebote prüfen |
| `procurement.offers.create` | Bestätigtes Lieferantenangebot mit Preishistorie, Audit und Idempotenz anlegen |
| `procurement.offers.prepare_update` | Aktuelles Angebot, vollständigen Ist/Soll-Diff und exakte Version zeigen |
| `procurement.offers.update` | Bestätigte Angebotsänderung oder Archivierung mit Versionsprüfung und Audit speichern |
| `procurement.offers.compare` | Angebote normalisiert nach Stückpreis, Packung, Mindestmenge und Lieferzeit |
| `procurement.suppliers.search` | Lieferantenleistung, Risiko, Angebote und Bestellvolumen |
| `procurement.requisitions.list` | Bedarfsanforderungen, Status, Begründung und Wert |
| `procurement.audit.history` | Redigierte Historie eines eigenen Bedarfs oder als Admin von Bestellungen, Produkten, Produktlinks, Lieferanten, Kategorien und Angeboten |
| `procurement.orders.list` | Bestellungen und Wareneingangsfortschritt |
| `procurement.deliveries.expected` | Offene Lieferpositionen im Zeitfenster |
| `procurement.prices.history` | Preisverlauf und Änderung zum vorherigen Wert |
| `procurement.reorder.candidates` | Produkte unter Meldebestand samt bestem Angebot |
| `procurement.spend.summary` | Bestellvolumen nach Monat, Lieferant, Status und Währung |
| `procurement.risks.list` | Lieferanten-, Angebots-, Preis- und Lieferrisiken |
| `procurement.products.prepare_create` | Produktlink analysieren, Daten zusammenführen, Duplikate prüfen und konkrete Rückfragen liefern |
| `procurement.products.create` | Bestätigtes Produkt und optional eine Bezugsquelle über die ProcurementCore-API anlegen |
| `procurement.product_links.prepare_link` | Aktive Procurement- und Warehouse-Produkte, vorhandene Links, Namensabweichung und offene Referenzen prüfen |
| `procurement.product_links.link` | Eindeutige Produktverknüpfung mit Versionsprüfung, Audit und erhöhter Bestätigung anlegen oder ändern |
| `procurement.suppliers.prepare_create` | Alle Lieferantenfelder, eindeutigen Code und ähnliche Bestandsnamen prüfen; vollständigen Entwurf und Rückfragen liefern |
| `procurement.suppliers.create` | Lieferanten nach finaler Bestätigung über die ProcurementCore-API atomar und auditiert anlegen |
| `procurement.suppliers.prepare_update` | Administratorrechte, alle Feldänderungen, ähnliche Namen und exakte Version prüfen; Ist/Soll-Diff zeigen |
| `procurement.suppliers.update` | Bestätigte Änderung oder Deaktivierung mit Versionsprüfung und transaktionalem Audit speichern |
| `procurement.categories.prepare_create` | Namen und vollständige Parameterdefinitionen validieren; gleiche und ähnliche Kategorien prüfen |
| `procurement.categories.create` | Bestätigte Kategorie samt Parameterschema, Audit und Idempotenz anlegen |
| `procurement.categories.prepare_update` | Administratorrechte, vollständigen Diff, ähnliche Namen und exakte Version prüfen |
| `procurement.categories.update` | Bestätigte Kategorieänderung mit Versionsprüfung und transaktionalem Audit speichern |
| `procurement.orders.prepare_create` | Lieferant, Positionen, Termine, Währung, Duplikate und Gesamtwert prüfen |
| `procurement.orders.create` | Bestätigte Bestellung mit Procurement-Administratorrechten anlegen |
| `procurement.orders.prepare_update` | Entwurf, alle Positionen, aktive Referenzen, Gesamtwert, Ist/Soll-Diff und exakte Version prüfen |
| `procurement.orders.update` | Bestätigten Bestellentwurf samt Positionen versionsgesichert und auditiert ersetzen |
| `procurement.orders.prepare_transition` | Erlaubten Statuswechsel, Positionen, Diff, Version und Bestätigungsphrase für eine Bestellung zeigen |
| `procurement.orders.transition` | Bestellung als gesendet oder bestätigt markieren oder mit Grund stornieren; keine externe Bestellung verschicken |
| `procurement.requisitions.prepare_create` | Bedarfstitel, Datum, Positionen und aktive Produkt-/Lieferantenreferenzen prüfen; vollständigen Entwurf zeigen |
| `procurement.requisitions.create` | Bestätigten Bedarfsentwurf mit transaktionalem Audit und Idempotenz anlegen |
| `procurement.requisitions.prepare_update` | Eigenen Entwurf mit allen Positionen, Ist/Soll-Diff und exakter Version laden |
| `procurement.requisitions.update` | Bestätigte Änderung eines eigenen Entwurfs mit Version, Audit und Idempotenz speichern |
| `procurement.requisitions.prepare_submit` | Eigenen Entwurf samt Positionen, Wert, Version und Bestätigungsphrase vor Einreichung prüfen |
| `procurement.requisitions.submit` | Bedarf mit eigenem Submit-Scope, Versionsprüfung und Bestätigungsphrase einreichen |
| `procurement.requisitions.prepare_decide` | Eingereichten Bedarf, Version und Vier-Augen-Trennung für Genehmigung, Ablehnung oder Rückgabe prüfen |
| `procurement.requisitions.decide` | Bedarf mit eigenem Approval-Scope, Versionsprüfung und datensatzgebundener Bestätigungsphrase entscheiden |
| `procurement.orders.prepare_receive` | Offene Bestellmenge, Überlieferung, Produktlink, Seriennummern und Zielzone prüfen |
| `procurement.orders.receive` | Wareneingang atomar mit Bestand/Geräten und offenem Putaway-Task buchen |

### Schreibvertrag

Vor jeder Schreiboperation wird das zugehörige `prepare_*`-Tool aufgerufen. Es liefert einen strukturierten Entwurf, aktuelle Werte, `required_missing_fields`, `questions_for_user`, Risiken und `ready_to_execute` beziehungsweise bei älteren Create-Tools `ready_to_create`. Der Client fragt offene Angaben ab, zeigt den finalen Entwurf und setzt das passende `confirm_*` erst nach ausdrücklicher Zustimmung. Empfohlene Produktlücken dürfen nur mit `accept_incomplete=true` und ausdrücklicher Nutzerentscheidung offen bleiben. Duplikate, ungültige IDs, fremde Planner-Pläne, unzulässige Zustände und mehrdeutige Referenzen blockieren die Ausführung.

Jedes Ausführungstool akzeptiert zusätzlich:

- `dry_run=true`: Bestätigung wird serverseitig unterdrückt; zurückgegeben wird
  die exakte validierte Vorschau unter `data.would_execute`. Es werden keine
  Daten geändert.
- `idempotency_key`: Für jeden Aufruf mit aktivem `confirm_*` verpflichtend.
  Zulässig sind 8–128 Zeichen aus Buchstaben, Ziffern, `.`, `_`, `:` und `-`.
Der Client verwendet pro fachlichem Vorgang einen stabilen, neuen Schlüssel.

Kritische Procurement-Aktionen verlangen zusätzlich den unverändert aus der
Vorschau übernommenen Wert `expected_updated_at` und die dort ausgegebene
`confirmation_text_required`-Phrase. Freigaben dürfen nicht durch den
Anforderer selbst erfolgen. Überlieferungen benötigen
`allow_overdelivery=true` und eine abweichende, ausdrücklich auf
„OVERDELIVERY“ lautende Phrase.

Identische Wiederholungen desselben Benutzers werden pro MCP-Instanz 24 Stunden
lang ohne zweite Mutation beantwortet; parallele Aufrufe werden
zusammengeführt. Eine abweichende Payload mit demselben Schlüssel wird
abgewiesen. Der Schlüssel wird als `Idempotency-Key` an den Ziel-Core
weitergegeben. Nach einem MCP-Neustart oder über mehrere Replikate hinweg ist
die dauerhafte Deduplizierung erst garantiert, sobald auch der jeweilige
Ziel-Core diesen Header persistent verarbeitet. ProcurementCore verarbeitet
ihn für Produkt-, Angebots-, Lieferanten- und Kategorieanlagen und -änderungen sowie
Bedarfsentscheidungen und Wareneingänge transaktional und speichert das
Ergebnis zusammen mit der Fachmutation.

## Cross-Core-Entscheidungen (4)

| Tool | Zweck |
|---|---|
| `inventory.coverage.check` | Verfügbarkeit gegen parallelen Jobbedarf und Sicherheitsbestand prüfen |
| `inventory.alternatives.compare` | Bestand, Bedarf, Alternativen, Defekte, Nutzung und Angebote vergleichen |
| `inventory.procurement.recommendations` | Faktische Nachbeschaffungskandidaten mit Inbound und Angebot |
| `equipment.strategy.context` | Portfolio-, Hersteller- und Systemstrategie mit Nutzung, TCO-Indikatoren und Beschaffung |

## MCP-Prompts (5)

| Prompt | Zweck |
|---|---|
| `inventory-coverage` | Geführte Bestands- und Bedarfsprüfung |
| `equipment-strategy` | System-/Herstellerentscheidung vorbereiten |
| `operations-briefing` | Priorisiertes operatives Briefing |
| `procurement-decision` | Nachvollziehbarer Angebots- und Risikovergleich |
| `data-quality-review` | Datenlücken nach Entscheidungswirkung priorisieren |

Prompts führen nicht selbstständig Aktionen aus. Sie geben dem Client eine bewährte Reihenfolge und Bewertungsstruktur für die Tools vor.

### Lagerplatzänderungen ab 1.5.20

`warehouse.locations.prepare_update` und `.update` sind die freigegebene
Capability für Änderungen aktiver Lagerplätze mit `cores:warehouse:update`
(oder `cores:write`) und Warehouse-Administratorrechten. Ausführung benötigt
`confirm_update=true`, die exakte `expected_updated_at`-Version aus der Vorschau
und einen Idempotenzschlüssel. `dry_run=true` unterdrückt jede Bestätigung.
Code, Scan-Code und Name unter demselben Elternknoten sind eindeutig; ähnliche
Namen müssen vor einer Umbenennung ausdrücklich geprüft werden. Elternknoten
und alle Vorfahren müssen aktiv sein, eine Zuordnung zu sich selbst oder einem
Nachfahren ist gesperrt. Die Vorschau zeigt aktive Geräte im Lager, Cases und
Mengenbestand entsprechend der Warehouse-Lagerplatzansicht;
Kapazitätsabsenkungen unter die Belegung und nicht belegbare Plätze mit Bestand
werden blockiert. Nullable Felder können über `clear_fields` geleert werden;
gleichzeitiges Setzen und Leeren ist unzulässig. Betriebsstatus, Archivierung
und Inventurabschluss sind eigene Workflows und werden nicht geändert.
WarehouseCore wiederholt diese Prüfungen unter Sperre und schreibt Änderung,
Vorher/Nachher-Audit (`MCP/AI`) und Wiederholungsbeleg in einer Transaktion.
Alle Datenbankänderungen an Lagerplätzen erhöhen durch Migration 046 ihre
Version, einschließlich Frontend- und Inventuränderungen. Unveränderte
Inventurintervalle erhalten den bestehenden nächsten Zähltermin.

### Hersteller- und Markenänderungen ab 1.5.21

Diese benannten Update-Capabilities sind für Issue #4/#5 freigegeben und nutzen
`cores:warehouse:update` (oder `cores:write`) sowie Warehouse-Administratorrechte.
Vor dem Aufruf sind vollständiger Diff, verknüpfte Datensatzanzahlen und ähnliche
Namen zu prüfen. `expected_updated_at` stammt unverändert aus der Vorschau;
`confirm_update=true` und ein eindeutiger `idempotency_key` sind erforderlich.
`dry_run=true` verändert nichts.

Hersteller unterstützen `name` und `website`; eine leere Website wird NULL.
Marken unterstützen `name`, `manufacturer_id` und `clear_manufacturer=true` zum
expliziten Entfernen der Zuordnung. Hersteller-ID setzen und gleichzeitig
entfernen ist verboten. Name und Herstellerpaar dürfen keine Dublette ergeben;
auch Marken ohne Hersteller werden auf Dubletten geprüft. Ein Wechsel oder
Entfernen der Herstellerzuordnung ist gesperrt, wenn verknüpfte Produkte nicht
bereits dieselbe Herstellerzuordnung haben. Bestehende Produkte werden nicht
automatisch umgeschrieben. Namensänderungen wirken auf alle verknüpften Anzeigen.

WarehouseCore installiert `047_warehouse_master_version` beim Start. Trigger
versionieren auch bestehende Oberflächen- und Import-Schreibpfade. Update,
Vorher/Nachher-Audit mit `MCP/AI`-Herkunft und dauerhafter Idempotenzbeleg werden
zusammen gespeichert; Fehler rollen alle drei zurück. Parallel laufende
Stammdaten- und Produktänderungen werden bei der abschließenden Prüfung gesperrt.

### Kategoriepflege und explizit autorisiertes Entfernen ab 1.5.22

Die benannten Kategorie-Update- und Löschwerkzeuge sind für Issue #4/#5
freigegeben. Der Nutzer hat das Entfernen von Kategorien ausdrücklich angefordert.
Ein Update benötigt Warehouse-Adminrechte, `cores:warehouse:update` oder
`cores:write`, vollständigen Diff, unveränderte Mikrosekunden-Version,
`confirm_update=true` und Idempotenz. Name: 1–100 Zeichen; Abkürzung: höchstens
10 Zeichen, auf Hauptebene verpflichtend. `category_id` im Unterkategorie-Update
und `subcategory_id` im Dritt-Kategorie-Update ändern den Elternknoten;
Primärschlüssel bleiben unverändert. Produktzuordnungen einschließlich
Nachfahren dürfen dadurch nicht widersprüchlich werden.

Löschen ist ausschließlich mit Warehouse-Adminrechten und `cores:warehouse:delete`
oder `cores:write` möglich. `prepare_delete` muss vor der ausdrücklichen
Bestätigung den ganzen Datensatz und die Produkt-/Kinderanzahl zeigen. Es darf
keine Produktverwendung und keine Kinder geben. Auch zusätzliche Fremdschlüssel
von Erweiterungstabellen sperren das Entfernen bis zur gesonderten Prüfung.
`confirm_delete=true`, exakte `expected_updated_at`, die unveränderte Phrase
`DELETE WAREHOUSE CATEGORY <id>`, `DELETE WAREHOUSE SUBCATEGORY <id>` bzw.
`DELETE WAREHOUSE THIRD_CATEGORY <id>` und `idempotency_key` sind erforderlich.
Keine kaskadierenden Löschungen oder automatischen Umzuordnungen. Dauerhaftes
Entfernen besitzt kein MCP-Undo; die Audit-Historie bleibt erhalten.

`cores.entities.schema` veröffentlicht `update_fields` und `delete_fields`.
WarehouseCore-Migration `049` bzw. Umbrella-Migration `022` installieren
Versionstrigger für alle drei Tabellen. Der Ziel-Core prüft die Daten unter
Sperren erneut und speichert Änderung/Löschung, Audit und dauerhaften
Wiederholungsbeleg zusammen. Ein Audit-Fehler rollt alles zurück.

### Warehouse-Produktpakete ab 1.5.23

| Tool | Funktion |
|---|---|
| `warehouse.packages.prepare_create` | Vollständige Metadaten und 1–200 aktive Produktzeilen samt Mengen/Optionalität prüfen; Dubletten und ähnliche Pakete zeigen |
| `warehouse.packages.create` | Paket, Inhalte, Audit und dauerhaften Wiederholungsbeleg nach Bestätigung atomar anlegen |
| `warehouse.packages.prepare_update` | Alle Felder/Inhalte, exakte Version, vollständigen Diff und Jobanzahl anzeigen |
| `warehouse.packages.update` | Versionsgesicherte Änderung nach Bestätigung; Preis/Inhalt bei vorhandener Jobnutzung sperren |

Schema-Discovery: `warehouse.packages` enthält `fields`, `update_fields` sowie
`item_fields` für `product_id`, `quantity` (1–1000000) und `is_optional`.
Preis ist EUR, nichtnegativ, höchstens zwei Nachkommastellen und 99999999.99.
Namen 1–255, Beschreibung höchstens 4000, Kategorie höchstens 100 Zeichen;
maximal 50 Aliasse mit je höchstens 160 Zeichen. Blank-/Dublettenaliasse werden
normalisiert; Produkt-IDs müssen verschieden und aktiv sein.

Ausgelassene Updatefelder bleiben erhalten; `items` ersetzt die komplette
Produktliste, `aliases: []` leert Aliasse. `clear_fields` unterstützt nur
`description`, `price`, `category`; gleichzeitiges Setzen und Leeren ist ungültig.
Jobnutzung schützt Preis und Zusammensetzung einschließlich Optionalität auch
für vergangene Jobs. Andere Metadaten bleiben bearbeitbar. Keine Lagerbuchung,
keine neuen Produkte, keine Datei-/Bildoperationen und keine Paketlöschung.
Website-Sichtbarkeit wird samt Hinweis auf öffentliche Daten vorbereitet.

Warehouse-Admin und Create-/Update-Scope (oder Legacy Write), `confirm_creation`
bzw. `confirm_update`, Idempotenzschlüssel und beim Update exakte Version sind
nötig. Ziel-Core prüft erneut und committet Audit und Wiederholungsbeleg atomar;
Migration 050/Umbrella 023 versioniert auch Änderungen einzelner Produktzeilen.
Metadatenänderungen ersetzen keine Zeilen-IDs; `dry_run=true` verändert nichts.

### Warehouse-Geräte ab 1.5.24

| Tools | Verhalten |
|---|---|
| `warehouse.devices.prepare_create` / `create` | Ein Einzelgerät mit vollständigen Metadaten und optionalem Lagerplatz prüfen/anlegen; aktives individual-Produkt, eindeutige reservierte Kennungen, Kapazität |
| `warehouse.devices.prepare_update` / `update` | Vollständiger Felddiff, exakte Geräteversion, nullable `clear_fields`; Identitätsänderungen bei aktiven Abhängigkeiten, Produktwechsel bei Historie sperren |
| `warehouse.devices.prepare_archive` / `archive` | Abhängigkeiten und Version prüfen; alle Scan-Kennungen deaktivieren, Historie bewahren; keine endgültige Löschung |
| `warehouse.devices.prepare_restore` / `restore` | Abhängigkeiten, aktives Produkt, Kennungen und Kapazität erneut prüfen; Scan-Kennungen reaktivieren, Betriebszustand bewahren |
| `warehouse.devices.audit_history` | Administrator-Abfrage ohne Notizinhalte, Roh-JSON, IP oder User-Agent; Audit-ID, Version und `can_prepare_revert` |
| `warehouse.devices.prepare_revert_update` / `revert_update` | Nur eigene letzte unveränderte MCP-`device.update` anhand Audit-ID umkehren; erneute Referenzprüfung, eigener Audit und Replay-Beleg |

Schema `warehouse.devices`: `fields`, `update_fields`, `lifecycle_fields`,
`revert_fields`. Schreibrechte: create für Anlage, update für Feldänderung/Revert,
archive für Archivierung/Restore; Legacy `cores:write` bleibt kompatibel.
Ausführungen brauchen jeweils `idempotency_key`, explizite Bestätigung und bei
bestehenden Geräten die exakte `expected_updated_at`-Version. Lebenszyklus:
`confirm_lifecycle` plus `ARCHIVE|RESTORE WAREHOUSE DEVICE <ID>`. Revert:
`confirm_revert` plus `REVERT WAREHOUSE DEVICE <ID> UPDATE <AUDIT-ID>`.

Aktive Jobs (auch gepacktes/ausgegebenes Material geschlossener Jobs), Picklisten,
Reservierungen, Cases, Komponenten, Aufgaben, offene Defekte, Wartungsaufträge
und aktive Wartungspläne sperren Archivierung/Restore und Identitätsänderungen.
Seriennummern und Scan-Kennungen bleiben gegenüber archivierten Geräten reserviert.
Metadaten-Updates verändern weder Bewegungen noch Betriebszustand. Wartungsdaten
schließen keine Aufträge ab und ändern keine Pläne. Physische Etiketten werden
bei Kennungsänderungen nicht automatisch erzeugt.

WarehouseCore `5.9.94` committet Mutation, Audit und Replay-Beleg atomar.
Migration `051` / Umbrella `024` versioniert sämtliche Geräte-Schreiber.
Revert ist ausdrücklich nur der beschriebene Feld-Rückweg; spätere Writes oder
Audits, fremde Akteure und andere Aktionsarten sperren ihn.

### Warehouse-Paket-Lebenszyklus ab 1.5.25

| Tools | Verhalten |
|---|---|
| `warehouse.packages.prepare_archive` / `archive` | Vollständiges Paket, Metadaten-/Inhaltsversion, Website-Diff, aktive Jobs und Reservierungen prüfen; Paket deaktivieren, Historie bewahren |
| `warehouse.packages.prepare_restore` / `restore` | Zusätzlich aktive Produktreferenzen und gültige Bestandteile prüfen; Paket reaktivieren, Website-Sichtbarkeit deaktiviert lassen |
| `warehouse.packages.audit_history` | Administrator-Abfrage von redigierten Paket-Ereignissen und neuen Ergebnisversionen, ohne Beschreibungen, Roh-JSON, IP oder User-Agent |

Schema `warehouse.packages` enthält `lifecycle_fields`. Scope für beide Aktionen:
`cores:warehouse:archive` oder Legacy `cores:write`, plus Warehouse-Admin.
Ausführung braucht `expected_updated_at`, `idempotency_key`, `confirm_lifecycle`
und exakt `ARCHIVE|RESTORE WAREHOUSE PACKAGE <ID>`. Dry-run ist möglich.

Aktive Jobs, Jobs ohne Status und offene Reservierungen blockieren beide Aktionen;
historische geschlossene Jobs bleiben erhalten. Restore prüft alle Bestandteile,
aktive Produkte und den eindeutigen Namen. Beide Aktionen deaktivieren Website-
Sichtbarkeit; spätere Veröffentlichung verlangt einen gesonderten bestätigten
Update. ID, Code, Preis und Inhaltszeilen bleiben erhalten, ohne Bestandsbewegung
oder endgültiges Löschen. WarehouseCore 5.9.95 speichert Mutation, Audit und
Replay atomar und prüft alle Abhängigkeiten erneut. Bestehende Versionstrigger
reichen aus; keine zusätzliche Migration. Geräte-Abhängigkeiten behandeln
fehlenden Jobstatus ebenfalls konservativ als offen.

### Warehouse-Lagerplatz-Lebenszyklus ab 1.5.26

Lagerplätze können über `warehouse.locations.prepare_archive`/`archive` und
`prepare_restore`/`restore` archiviert und wiederhergestellt werden. Die Vorschau
zeigt alle Metadaten, exakte Version, Betriebsstatus-Diff und Abhängigkeiten.
Warehouse-Admin und `cores:warehouse:archive` (oder Legacy `cores:write`) sind
nötig. Ausführung verlangt `expected_updated_at`, `idempotency_key`,
`confirm_lifecycle=true` und exakt `ARCHIVE|RESTORE WAREHOUSE LOCATION <ID>`.
Dry-run ist möglich.

Aktive Geräte (auch auf Jobs), aktive Nachfahren, Cases am Platz oder mit diesem
Heimatplatz, jede Mengenzeile mit Bestand sowie offene Aufgaben und Inventuren
sperren beide Aktionen. Unbekannte Aufgaben-/Inventurstatus sperren ebenfalls;
entgegengesetzte Bestandszeilen heben die Sperre nicht auf. Inventur-Betriebsstatus
sperrt Archivierung. Historische abgeschlossene Vorgänge und archivierte Geräte
bleiben erhalten. Keine Bestandsbewegung, Löschung oder Kaskade auf Kinder.

Restore prüft alle gespeicherten Felder, Identität und die gesamte Elternhierarchie
auf Aktivität, fehlende Knoten und Zyklen. Bei einer unveränderten, protokollierten
MCP-Archivierung wird `available`, `blocked` oder `maintenance` aus dem Vorzustand
wiederhergestellt. Ältere Archive ohne passenden Versionsbeleg oder später
bearbeitete Archive werden als `blocked` aktiviert und benötigen eine bewusste
Betriebsfreigabe im WarehouseCore. Beide Zustandswerte werden im Diff gezeigt.
ID, Code, Scan-Code, Metadaten, Hierarchie, Inventurplanung und Historie bleiben.

`warehouse.locations.audit_history` liefert Administratoren redigierte Ereignisse
mit Nutzer, Herkunft, Zeitpunkt, Ergebnisversion und Statuswechsel. Beschreibungen,
Roh-JSON, IP und User-Agent werden ausgeschlossen. Anlage und Bearbeitung schreiben
jetzt ebenfalls die Ergebnisversion in den Audit. `cores.entities.schema` für
`warehouse.locations` enthält `lifecycle_fields`. WarehouseCore prüft erneut und
speichert Mutation, Audit und Replay atomar. Migration `046` / Umbrella `019`
versioniert bereits alle Lagerplatz-Schreiber; keine neue Migration erforderlich.


### Warehouse-Cases ab 1.5.27

| Werkzeug | Vertrag |
| --- | --- |
| `warehouse.case_models.search` | Vorhandene Modelle anhand Name/ID auflösen |
| `warehouse.cases.prepare_create` / `create` | Leeres Case vollständig anlegen, ähnlich benannte Cases ausdrücklich bestätigen |
| `warehouse.cases.prepare_update` / `update` | Metadaten mit exakter Inhalts-/Metadatenversion und vollständigem Diff pflegen |
| `warehouse.cases.prepare_archive` / `archive` | Leeres, ungebundenes Case mit datensatzgebundener Phrase archivieren |
| `warehouse.cases.prepare_restore` / `restore` | Referenzen, Scanneridentität, Vorlagen und Lagerhierarchie erneut prüfen und wiederherstellen |
| `warehouse.cases.audit_history` | Redigierte Audit-Metadaten ohne Beschreibungen und Roh-JSON |

Schema: `cores.entities.schema(entity="warehouse.cases")`. Eingaben sind Name,
Beschreibung, Typ (dynamic/fixed/hybrid), vorhandenes Modell, Maße in cm, Gewicht
und maximales Gesamtgewicht in kg, initialer Lagerplatz, Heimatplatz, Barcode
und RFID. Ausgelassene Updatefelder bleiben erhalten; `clear_fields` leert
nullable Felder ausdrücklich. Ein Update bewegt kein Case und verändert keine
Inhalte, Vorlagen oder Prozesszustände. Identitäts-/Geometrieänderungen sind
bei aktiven Abhängigkeiten gesperrt.

Admin/create-update-archive-Scope, `confirm_change`, Idempotenz und beim Update/
Lifecycle exakte Version sind erforderlich. Dry-run und Prepare führen auch
bei gesetzter Bestätigung keinen Schreibvorgang aus. Lifecycle benötigt die
exakte Phrase `ARCHIVE|RESTORE WAREHOUSE CASE <ID>` und blockiert aktive Inhalte,
Verschachtelung, Aufgaben, Jobs und Prozesse. Alle Alias-Kennungen folgen dem
Lifecycle; archivierte IDs bleiben reserviert. Restore validiert aktive
Template-Produkte, Modell, Heimatplatz, sämtliche Lagereltern, Profil und
Kapazität. API-Validierung, Case, Audit und Replay erfolgen in einer Transaktion.

Der [Abschlusscheck](ISSUE_COMPLETION.md) dokumentiert den verbleibenden Umfang
von #4 und #5; die Issues sind mit diesem Teilrelease noch nicht abschließbar.

Schema-Discovery umfasst außerdem eingebettete `dry_run`-/Idempotenzfelder und
verschachtelte Objekt-/Zeilenfelder (`fields` bzw. `item_fields`), einschließlich
Bestellpositionen. Bestehende Rental-Job-/Requirement-Updates und Warehouse-
Produkt-Lifecycle liefern ihre jeweiligen Eingabefelder ebenfalls ausdrücklich.

### Atomare Gerätestapel ab 1.5.28

`warehouse.devices.prepare_bulk_create` → vollständige Liste und Auswirkungen
zeigen → Warehouse-Admin bestätigt → `warehouse.devices.bulk_create`.
1–100 Items mit allen Feldern von `WarehouseDeviceBulkItem`; Schema
`warehouse.device_batches`. Passender create-Scope, `confirm_creation=true`,
`idempotency_key` und exakte batchgebundene `confirmation_text` aus der Vorschau.
Die Phrase bindet alle normalisierten Felder und die Anzahl. Keine Teilanlage:
Geräte, Scan-Kennungen, pro Gerät `device.bulk_create`-Audit und dauerhafter Replay
werden im WarehouseCore gemeinsam gespeichert. Vorschau und `dry_run` schreiben
nichts. Seriennummern und Scan-Codes müssen innerhalb des Stapels und gegenüber
aktiven/archivierten Beständen eindeutig sein. Kombinierte Lagerkapazität,
Lagerprofile und zyklusfreie aktive Hierarchie werden erneut geprüft. IDs und
fehlende Kennungen entstehen bei Ausführung; keine Etiketten oder Anhänge.
`warehouse.devices.audit_history` liefert die redigierten Stapelereignisse.

### Hersteller-/Marken-Lebenszyklus ab 1.5.29

`warehouse.manufacturers` und `warehouse.brands` besitzen
`prepare_archive`/`archive`, `prepare_restore`/`restore` und `audit_history`.
Lifecycle-Input: `id`, exaktes `expected_updated_at`, `confirm_lifecycle`,
`confirmation_text`, `idempotency_key`; `dry_run` schreibt nichts.
Warehouse-Admin/archive-Scope und exakt
`ARCHIVE|RESTORE WAREHOUSE MANUFACTURER|BRAND <ID>` sind erforderlich.
Vorschau zeigt alle Felder, Version, Lebenszyklus-Diff und aktive/historische
Produkt-/Markenzahlen. Aktive Referenzen sperren Archivierung, keine Kaskade.
Restore prüft reservierte Identität und aktiven Hersteller der Marke.
Historische Produkte und Stammdaten bleiben erhalten. Alle bestehenden
Schreibpfade schützen aktive Zuordnungen per Datenbankregeln; archivierte
Metadaten benötigen Restore vor Bearbeitung. Die neuen MCP-Aktionen speichern
Änderung, Vorher/Nachher-Audit und dauerhaften Replay gemeinsam. Audit-Historie
ist administratorgeschützt und redigiert; keine Rohwerte, Website oder IP.
Auflösung enthält `lifecycle_status`; archivierte Treffer zuerst restaurieren.

### Wartungspläne ab 1.5.30

`warehouse.maintenance_plans.search` löst vorhandene Pläne inklusive inaktiver
Historie auf. `prepare_create`/`create`, `prepare_update`/`update`,
`prepare_archive`/`archive`, `prepare_restore`/`restore` und `audit_history`
verwenden das vollständige Schema `warehouse.maintenance_plans`.

Warehouse-Admin plus create/update/archive-Scope, vollständige Vorschau,
`confirm_change=true`, `idempotency_key`, exakte `expected_device_updated_at`
und für bestehende Pläne `expected_updated_at` sind nötig. Lifecycle verlangt
zusätzlich `ARCHIVE|RESTORE WAREHOUSE MAINTENANCE PLAN <ID>`; offene Aufträge
sperren beide Aktionen. Gerätezuordnung ist unveränderlich; Restore prüft
aktives Produkt/Gerät und Ausmusterung. Intervalle 1–3650 Tage, Vorlauf 0–365
(Default 14), Typ preventive/inspection/calibration (Default preventive),
Planname 1–160 Zeichen, Arbeitsanweisung maximal 4000 und gültiges Datum.

Vorschau zeigt alle Felder, Diff, Duplikate, Referenzzahlen, den neuen
Gerätetermin und einen gegebenenfalls fälligen geplanten Auftrag. Plan,
Gerätetermin, automatisch fälliger Auftrag, Ereignis, Audits und dauerhafter
Replay sind atomar. Physischer Ort und Zustand bleiben. Updates ergänzen nur
angegebene Felder; `clear_fields` unterstützt instructions. `dry_run` schreibt
nichts. Alle Core-Schreiber versionieren Pläne/Aufträge; Auftragsänderungen
versionieren auch den referenzierten Plan. Audit-Ausgabe schließt Anweisungen
und Roh-JSON aus. Manuelle Auftrags- und Defektabläufe folgen getrennt.
