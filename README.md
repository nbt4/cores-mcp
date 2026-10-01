# Cores MCP

## Atomare MCP-Gerätestapel — WarehouseCore 5.9.98 / Cores MCP 1.5.28

`warehouse.devices.prepare_bulk_create` und `warehouse.devices.bulk_create`
verarbeiten 1–100 vollständige Geräteentwürfe. Die Vorschau prüft aktive,
einzeln geführte Produkte, alle Metadaten, reservierte Serien-/Scan-Kennungen,
Duplikate innerhalb des Stapels sowie die gesamte Belegung jedes Lagerplatzes.
Lagerprofil und vollständige Hierarchie werden geprüft. Vorschauen erzeugen
keine Geräte, Kennungen, Audits oder Replay-Belege.

Ausführung benötigt Warehouse-Admin, `cores:warehouse:create` (oder Legacy
`cores:write`), `confirm_creation`, `idempotency_key` und die exakte
`confirmation_text` aus der Vorschau. Die zusätzliche Phrase enthält Anzahl
und Fingerprint aller normalisierten Gerätefelder; Änderungen benötigen eine
neue Vorschau und Bestätigung. `dry_run` verhindert die Ausführung.
Geräte, Kennungen, ein Vorher/Nachher-Audit pro Gerät und dauerhafter Replay-Beleg
werden gemeinsam gespeichert oder vollständig zurückgerollt. Keine Etiketten-
oder Dateierzeugung. Automatische IDs werden erst bei Ausführung vergeben.
`cores.entities.schema` liefert das Schema `warehouse.device_batches` inklusive
aller Gerätefelder. `warehouse.devices.audit_history` zeigt auch Stapelereignisse.

195 Tools: 69 Abfragen, 63 Vorschauen, 63 Ausführungen. Keine neue Konfiguration.
Die Issues #4/#5 bleiben bis zum vollständigen [Abschlusscheck](https://github.com/nbt4/cores-mcp/blob/main/docs/ISSUE_COMPLETION.md) offen.

## MCP-Cases — WarehouseCore 5.9.97 / Cores MCP 1.5.27

Cases unterstützen prepare_create/create, prepare_update/update,
prepare_archive/archive, prepare_restore/restore und redigierte audit_history.
warehouse.case_models.search liefert vorhandene Modelle. Vollständige Vorschau,
Diff, Warehouse-Admin und create/update/archive-Scope, explizite confirm_change,
Idempotenz und exakte Mikrosekunden-Version sind erforderlich; Lifecycle verlangt
zusätzlich ARCHIVE|RESTORE WAREHOUSE CASE <ID>. Keine Bestandsbewegung oder
endgültige Löschung. Nullbare Felder sind über clear_fields ausdrücklich leerbar.

Migration Warehouse 052 / Umbrella 025 versioniert Metadaten, Inhalte, Templates
und verschachtelte Cases aller Schreibpfade. Archivierte Cases behalten IDs,
Metadaten und Vorlagen; aktive Inhalte, Jobs, Aufgaben und Lagerabläufe sperren.
Scannerkennungen werden deaktiviert und bleiben reserviert. Restore prüft
Modell, Lagerhierarchie, Profil, Kapazität und aktive Template-Produkte erneut.
Mutation, Vorher/Nachher-Audit mit MCP/AI und dauerhafter Replay-Beleg sind atomar.

Schema-Discovery liefert auch eingebettete Dry-run-/Idempotenzfelder und
verschachtelte Eingaben wie Bestellpositionen. Bestehende Rental-Updates und
Warehouse-Produkt-Lifecycle haben ausdrückliche Eingabeschemas.

193 Tools: 69 Abfragen, 62 Vorschauen, 62 Ausführungen. Keine neue Konfiguration.
Vollständiger Restumfang von #4/#5: [MCP-Abschlusscheck](docs/ISSUE_COMPLETION.md).


## Lagerplätze archivieren und wiederherstellen ab 1.5.26

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

Erfordert WarehouseCore `5.9.96`.


## Produktpakete archivieren und wiederherstellen ab 1.5.25

`warehouse.packages.prepare_archive`/`archive` und `prepare_restore`/`restore`
zeigen vollständiges Paket, exakte Metadaten-/Inhaltsversion, Lebenszyklus- und
Website-Diff sowie aktive Jobs, offene Reservierungen und historische Jobnutzung.
Warehouse-Admin und `cores:warehouse:archive` (oder Legacy `cores:write`) sind
nötig. Ausführung braucht Version, Idempotenzschlüssel, `confirm_lifecycle` und
`ARCHIVE|RESTORE WAREHOUSE PACKAGE <ID>`. Dry-run bleibt verfügbar.

Aktive Jobs (einschließlich Jobs ohne Status) und offene Reservierungen sperren
beide Aktionen. Restore validiert alle Bestandteile und aktive Produktreferenzen.
Geschlossene Jobhistorie, Preise, Code und Inhaltszeilen bleiben erhalten.
Beide Aktionen deaktivieren Website-Sichtbarkeit; Restore veröffentlicht nicht.
Dafür anschließend bewusst `warehouse.packages.prepare_update`/`update` nutzen.
Keine Produkte oder Bestände werden gelöscht oder bewegt.

`warehouse.packages.audit_history` zeigt für Administratoren redigierte
Anlage-/Änderungs-/Archiv-/Restore-Ereignisse und neue Ergebnisversionen.
Beschreibungsinhalte, Roh-JSON, IPs und User-Agent bleiben ausgeschlossen.
`cores.entities.schema` liefert `lifecycle_fields` für `warehouse.packages`.
WarehouseCore `5.9.95` prüft atomar und speichert Mutation, Audit und Replay.
Vorhandene Paket-Versionstrigger decken beide Aktionen ab; keine neue Migration.
Die Geräte-Abhängigkeitsprüfung blockiert nun ebenfalls Jobs ohne Status.


## Geräte anlegen, bearbeiten und archivieren ab 1.5.24

Die Werkzeugpaare `warehouse.devices.prepare_create`/`create`,
`prepare_update`/`update`, `prepare_archive`/`archive`, `prepare_restore`/`restore`
und `prepare_revert_update`/`revert_update` decken Einzelgeräte ab. Alle verlangen
Warehouse-Administratorrechte und den passenden `cores:warehouse:create`,
`cores:warehouse:update` bzw. `cores:warehouse:archive`-Scope (oder Legacy
`cores:write`). Anlage und Bearbeitung zeigen sämtliche Metadaten inklusive
Seriennummer, Barcode/QR, Bewertung, Betriebsstunden, Datumsfeldern und Notizen.
Nullable Felder lassen sich ausdrücklich über `clear_fields` leeren. IDs,
physischer Zustand und Betriebszustand werden bei Feldänderungen bewahrt;
Bewegungen und Betriebszustand laufen weiter über ihre eigenen Werkzeuge.

`cores.entities.schema` für `warehouse.devices` liefert `fields`, `update_fields`,
`lifecycle_fields` und `revert_fields`. Die bestätigte Ausführung braucht einen
Idempotenzschlüssel und bei bestehenden Geräten die exakte Vorschau-Version.
Archivierung und Restore verlangen `confirm_lifecycle` und eine Phrase
`ARCHIVE|RESTORE WAREHOUSE DEVICE <ID>`. Aktive Jobs, Picklisten, Reservierungen,
Cases, Komponenten, Aufgaben, Defekte und Wartung sperren beide Aktionen.
Scan-Kennungen und Seriennummern bleiben bei Archivierung reserviert. Restore
prüft zusätzlich Produkt und Lagerkapazität; Historie bleibt erhalten.

`warehouse.devices.audit_history` liest redigierte Änderungen und zeigt
`can_prepare_revert` für die eigene letzte unveränderte MCP-Feldänderung.
Roh-JSON, Notizinhalte, IPs und User-Agent sind ausgeschlossen. Revert braucht
Audit-ID, Version, `confirm_revert` und
`REVERT WAREHOUSE DEVICE <ID> UPDATE <AUDIT-ID>`. Spätere Geräteänderungen oder
Audits sperren Revert. Erneute Referenz-/Identitätsprüfung, eigener Audit und
dauerhafter Replay-Beleg verhindern das Überschreiben späterer Arbeit.
WarehouseCore `5.9.94` prüft und speichert alle Aktionen atomar; Migration `051`
(Umbrella `024`) versioniert auch Geräteänderungen außerhalb des MCP.


## Produktpakete anlegen und bearbeiten ab 1.5.23

`warehouse.packages.prepare_create`/`create` und `prepare_update`/`update`
verwalten Name, Beschreibung, EUR-Preis, Kategorie, Website-Sichtbarkeit,
Aliasse und die vollständige Produktliste mit Mengen und `is_optional`.
`cores.entities.schema` liefert dafür `fields`, `update_fields` und `item_fields`.
Ein Paket enthält 1–200 verschiedene aktive Produkte; Mengen sind ganze Zahlen
von 1 bis 1000000. Preise sind nichtnegativ mit höchstens zwei Nachkommastellen.
Paket-ID und automatisch erzeugter Code bleiben erhalten. Keine Lagerbuchung
oder spiegelnde Produktanlage erfolgt. Bilder/Datei-Uploads gehören nicht dazu.

Updates erhalten ausgelassene Felder und Produktzeilen. `items` ersetzt die
vollständige Liste, `aliases: []` leert Aliasse; `clear_fields` leert nullable
`description`, `price` und `category`. Beschreibung/Kategorie können auch über
`""` geleert werden. Die Vorschau zeigt sämtliche Felder, Inhalte, Diff,
Dubletten/ähnliche Namen und Jobnutzung. Bei Verwendung in einem Job bleiben
Preis und Inhalt gesperrt, auch für abgeschlossene Jobs; ein neues Paket ist
nötig. Andere Metadaten bleiben bearbeitbar. Öffentlich sichtbare Pakete
enthalten einen entsprechenden Hinweis in der Vorschau.

Warehouse-Adminrechte und `cores:warehouse:create` bzw. `cores:warehouse:update`
(oder Legacy `cores:write`), ausdrückliche Bestätigung und Idempotenzschlüssel
sind erforderlich. Updates verlangen die exakte `expected_updated_at`.
WarehouseCore 5.9.93 speichert Paket, Inhalte, vollständigen Vorher/Nachher-Audit
und dauerhaften Wiederholungsbeleg atomar. Migration `050` (Umbrella `023`)
versioniert auch UI-/Importänderungen an Metadaten und einzelnen Produktzeilen.
Metadaten-Updates erhalten Zeilen-IDs. Dry-run verändert nichts.


## Kategorien bearbeiten und entfernen ab 1.5.22

Alle drei Warehouse-Kategorieebenen bieten `prepare_update`/`update` sowie
`prepare_delete`/`delete`: `warehouse.categories`, `warehouse.subcategories`
und `warehouse.third_categories`. `cores.entities.schema` zeigt die jeweiligen
`update_fields` und `delete_fields`. Die dritte Ebene verwendet als Eingabe
`third_category_id`; `create` gibt diese ID als `subbiercategory_id` zurück.

Updates bearbeiten Namen, Abkürzungen und auf den unteren Ebenen den vorhandenen
Elternknoten. IDs bleiben unverändert. Die Vorschau zeigt alle Felder, den Diff,
Dubletten, ähnliche Namen und verknüpfte Datensatzanzahlen. Ein Elternwechsel,
der vorhandene Produktzuordnungen widersprüchlich macht, wird gesperrt; das gilt
auch für Produkte unter dritten Kategorien eines verschobenen Unterknotens.
Auf Hauptebene ist eine Abkürzung erforderlich, auf unteren Ebenen leert `""` sie.
Ausführung: Warehouse-Adminrechte, `cores:warehouse:update` oder `cores:write`,
`expected_updated_at`, `confirm_update=true` und `idempotency_key`.

Entfernen ist dauerhaft und nur für ungenutzte Kategorien ohne Kinder erlaubt.
Die Vorschau zeigt den ganzen Datensatz, Produkt- und Kinderanzahl sowie die
exakte Bestätigungsphrase, z. B. `DELETE WAREHOUSE CATEGORY 123`. Ausführung:
Warehouse-Adminrechte, `cores:warehouse:delete` oder `cores:write`, exakte
`expected_updated_at`, `confirm_delete=true`, `confirmation_text` und
`idempotency_key`. Ein Update-Scope allein berechtigt nicht zum Löschen.
Zusätzliche Fremdschlüssel aus Erweiterungstabellen sperren das Entfernen bis
zur gesonderten Prüfung. Es gibt kein kaskadierendes Löschen, keine automatische
Umzuordnung und kein MCP-Undo für dauerhaft entfernte Kategorien.

WarehouseCore prüft Version und Abhängigkeiten erneut. Änderung bzw. Löschung,
Vorher/Nachher-Audit mit MCP/AI-Herkunft und Wiederholungsbeleg werden atomar
committet. Audit bleibt nach dem Entfernen erhalten. `dry_run=true` verändert
nichts. Die Migration `049_warehouse_category_version` versioniert auch normale
Oberflächen- und Importänderungen. Für granulare Löschrechte den Connector mit
dem neuen Scope erneut autorisieren; bestehende `cores:write`-Freigaben gelten.

## Hersteller- und Markenpflege ab 1.5.21

`warehouse.manufacturers.prepare_update`/`.update` bearbeiten Namen und Website;
`website=""` leert die Website. `warehouse.brands.prepare_update`/`.update`
bearbeiten Namen und Herstellerzuordnung; `clear_manufacturer=true` entfernt
sie ausdrücklich. Beide Vorschauen zeigen Ist/Soll-Diff, exakte Version,
Dubletten und die Anzahl verknüpfter Datensätze. Namensänderungen gelten auf
allen verknüpften Datensätzen. Ähnliche Namen brauchen eine gesonderte Prüfung.

Die Ausführung benötigt Warehouse-Adminrechte, `cores:warehouse:update` oder
`cores:write`, `expected_updated_at`, `confirm_update=true` und einen
`idempotency_key`. Ein Herstellerwechsel einer Marke ist gesperrt, solange
verknüpfte Produkte einen anderen Hersteller benötigen. WarehouseCore prüft
Version und Abhängigkeiten erneut und speichert Änderung, Vorher/Nachher-Audit
und Wiederholungsbeleg atomar. Auch Änderungen über die Oberfläche erhöhen die
Version. `cores.entities.schema` liefert die neuen Update-Felder.

Der offizielle Smoke-Test verwendet für den übergreifenden Stammdatenresolver
und die Audit-Tools jetzt die passenden Argumente; die Audit-Tools benötigen
einen angemeldeten Administrator.

## OAuth-Freigabe ab 1.5.20

Die Freigabeseite bietet bei aktivierten Schreibtools die Auswahl **Nur Lesen**
oder **Lesen und Schreiben**, auch wenn der Client zunächst nur `cores:read`
anfordert. Nur Lesen ist vorausgewählt. Die ausdrückliche Schreibfreigabe
übernimmt angeforderte granulare Scopes oder den kompatiblen `cores:write`-Scope.
Fehlende Auswahl bleibt lesend; deaktivierte Schreibtools blockieren jede
Schreibfreigabe. Refresh-Tokens behalten ihre freigegebenen Rechte.
Bestehende lesende Verbindungen müssen einmal neu autorisiert werden: im
Cores-Dialog **Lesen und Schreiben** wählen und **Ausgewählten Zugriff erlauben**
bestätigen. Die Seite unterstützt Deutsch und Englisch über die Browsersprache
oder `lang=de|en`; Core-Rollen und die Bestätigung jeder Schreibaktion gelten
weiterhin.

## Lagerplatzpflege ab 1.5.20

`warehouse.locations.prepare_update` zeigt alle bearbeitbaren Felder, den
vollständigen Ist/Soll-Diff, die genaue Version, ähnliche Lagerplätze sowie
Elternhierarchie und aktuelle Belegung. `.update` benötigt Warehouse-Adminrechte,
`cores:warehouse:update` oder `cores:write`, `expected_updated_at`,
Idempotenzschlüssel und ausdrückliche Bestätigung. WarehouseCore 5.9.89 prüft
Duplikate, Hierarchiekreise und Bestandsgrenzen erneut und speichert Änderung,
Audit und Wiederholungsbeleg atomar. Optionale Felder werden über `clear_fields`
geleert; Betriebsstatus und Archivierung werden durch dieses Werkzeug nicht
geändert. `cores.entities.schema` beschreibt die vollständigen Update-Felder.

## Geführte Lagerplatzanlage ab 1.5.19

`warehouse.locations.prepare_create` prüft Code, Scan-Code, Namen, Lagerplatztyp,
Prozessrolle, Kapazität und einen aktiven Elternknoten. Ähnliche Lagerplätze
müssen ausdrücklich geprüft werden. `warehouse.locations.create` benötigt
Warehouse-Administratorrechte, `cores:warehouse:create` oder `cores:write`,
Idempotenzschlüssel und Bestätigung. WarehouseCore speichert Lagerplatz, Audit
und Wiederholungsbeleg in einer Transaktion.

## Warehouse-Kategorieanlage ab 1.5.18

`warehouse.categories.prepare_create`/`.create`,
`warehouse.subcategories.prepare_create`/`.create` und
`warehouse.third_categories.prepare_create`/`.create` legen die drei Ebenen
auch ohne neue Produktanlage an. Die Vorschau prüft Elternknoten, identische
Namen und ähnliche Stammdaten. Eine bestätigte Anlage benötigt Warehouse-
Administratorrechte und `cores:warehouse:create` oder `cores:write`.
WarehouseCore 5.9.87 speichert die Kategorie samt Audit und dauerhaftem
Idempotenzbeleg atomar. Die zurückgegebene ID lässt sich anschließend in
`warehouse.products.prepare_update`/`.update` verwenden.

## Warehouse-Stammdaten und Produktbeziehungen ab 1.5.17

`warehouse.manufacturers.prepare_create`/`.create` legen Hersteller unabhängig
von Produkten an; `warehouse.brands.prepare_create`/`.create` legen eine Marke
für eine vorhandene Hersteller-ID an. Beide Vorschauen prüfen identische und
ähnliche Namen. Danach lassen sich die zurückgegebenen IDs mit
`warehouse.products.prepare_update`/`.update` einem bestehenden Produkt
zuordnen. Für `PRD-01000038` wurde zudem der Schreib-Body geprüft: numerische
PostgreSQL-Bestandswerte wie `item_cost_per_day` werden jetzt als JSON-Zahlen
statt Text an WarehouseCore übergeben.

`warehouse.products.prepare_link_relation` zeigt beide Produktidentitäten,
die vorhandene Beziehung, alle Feldänderungen und die exakte Quellversion.
`warehouse.products.link_relation` legt eine erforderliche, empfohlene,
kompatible, alternative, enthaltene oder verbrauchte Beziehung mit Menge,
Zuordnungsebene und Notiz an oder ändert sie. Ausführung erfordert
Warehouse-Administratorrechte, `cores:warehouse:update` oder `cores:write`,
Version, Idempotenzschlüssel und die produktgebundene Bestätigungsphrase.
WarehouseCore 5.9.86 speichert Beziehung, Audit und Wiederholungsbeleg atomar.

## Warehouse-Produktlebenszyklus ab 1.5.16

`warehouse.products.prepare_archive` und `.prepare_restore` zeigen Status,
Produktversion, betroffene Geräte und bei Archivierung offene Jobanforderungen
sowie gepackte oder ausgegebene Geräte. `.archive` und `.restore` benötigen
Warehouse-Administratorrechte, `cores:warehouse:archive` (oder `cores:write`),
die unveränderte Version, einen Idempotenzschlüssel und die produktgebundene
Bestätigungsphrase. WarehouseCore 5.9.85 prüft aktive Verwendungen erneut unter
Sperre und speichert Änderung, Audit und Wiederholungsbeleg atomar.

## Warehouse-Produktverlauf ab 1.5.15

`warehouse.audit.history` liefert Warehouse-Administratoren den redigierten
Verlauf eines Produkts: Anlage, Änderung, Archivierung und Wiederherstellung
mit Zeit, Nutzer, Herkunft und ausgewählten Vorher-/Nachher-Feldern. Private
Notizen, IP-Adressen und rohe Audit-JSON-Werte werden nicht ausgegeben.
WarehouseCore 5.9.84 schützt MCP-Produktanlagen jetzt zusätzlich mit einem
dauerhaften Idempotenzbeleg, sodass Wiederholungen keine doppelten Produkte
oder Geräte erzeugen.

## Erweiterter Procurement-Auditverlauf ab 1.5.14

`procurement.audit.history` zeigt Administratoren nun auch den Verlauf von
Produkten, Produktlinks, Lieferanten, Kategorien und Angeboten. Die Ausgabe
enthält Aktion, Nutzer, Zeit, Herkunft und ausgewählte Vorher-/Nachher-Felder
wie Name, Aktivstatus, Angebotspreis und Warehouse-Verknüpfung. Private Notizen,
IP-Adressen und rohe Audit-Daten bleiben ausgeschlossen. Nicht-Administratoren
sehen weiterhin nur den Verlauf eigener Bedarfsanforderungen.

## Bestellentwürfe ab 1.5.13

`procurement.orders.prepare_update`/`.update` bearbeiten Lieferant,
Bestellnummer, Währung, Termine, Notizen und die vollständige Positionsliste
eines Entwurfs. Die Vorschau zeigt den Ist/Soll-Diff, den neu berechneten
Gesamtwert, aktive Referenzen und die exakte Version. Die Ausführung benötigt
Procurement-Administratorrechte, `cores:procurement:update` oder `cores:write`,
einen Idempotenzschlüssel und ausdrückliche Bestätigung. ProcurementCore 1.0.54
prüft die Version unter Sperre und speichert Positionsersatz und Audit atomar.

## Produktverknüpfung ab 1.5.12

`procurement.product_links.prepare_link`/`.link` verbinden ein aktives
Beschaffungsprodukt mit genau einem aktiven Warehouse-Produkt. Die Vorschau
zeigt beide Identitäten, vorhandene Zuordnungen, Namensabweichungen,
Abhängigkeiten, den vollständigen Link-Diff und die exakte Version. Bei
verschiedenen Namen muss die Zuordnung ausdrücklich als gleicher physischer
Artikel bestätigt werden. Neuverknüpfungen mit Wareneingängen oder offenen
Bestellungen werden blockiert. Die Ausführung benötigt Procurement-Adminrechte,
`cores:procurement:update` oder `cores:write`, einen Idempotenzschlüssel sowie
die datensatzgebundene Bestätigungsphrase. ProcurementCore 1.0.53 prüft alle
Referenzen unter Sperre und speichert Link und Audit atomar.

## Procurement-Auditverlauf ab 1.5.11

`procurement.audit.history` zeigt für eine Bedarfsanforderung oder Bestellung
Aktion, Zeitpunkt, ausführende Nutzer-ID, Herkunft, Status, Betrag und
Wareneingangsmenge. Private Notizen, IP-Adressen, User-Agent und rohe
Audit-JSON-Werte werden nicht ausgegeben. Nicht-Administratoren dürfen nur den
Verlauf eigener Bedarfsanforderungen lesen. ProcurementCore 1.0.52 speichert
Entscheidungs- und Wareneingangs-Audit nun atomar mit dem jeweiligen Vorgang.

## Bestellstatus ab 1.5.10

`procurement.orders.prepare_transition`/`.transition` prüfen den erlaubten
Statuswechsel, zeigen Bestellung und Positionen, den vollständigen Status-Diff,
die exakte Version und eine datensatzgebundene Bestätigungsphrase. Versand,
Bestätigung und Storno benötigen `cores:procurement:approve` oder `cores:write`
und Procurement-Administratorrechte. „Gesendet“ dokumentiert den Status; es
verschickt keine Bestellung an den Lieferanten. Ein Storno benötigt einen Grund
und erhält bereits gebuchte Wareneingänge. `procurement.orders.create` erzeugt
über MCP/KI nur noch Entwürfe und überträgt keine reinen Vorschau-Felder an die
Core-API. ProcurementCore 1.0.51 schützt Anlage und Statuswechsel mit
transaktionalem Audit, Versionsprüfung und dauerhafter Idempotenz.

## Bedarfsanforderungen ab 1.5.9

`procurement.requisitions.prepare_create`/`.create`, `prepare_update`/`.update`
und `prepare_submit`/`.submit` führen durch Anlage, Bearbeitung und Einreichung
eines Bedarfs. Vorschauen zeigen alle Positionen, gültige Produkt- und
Lieferantenreferenzen, geschätzten Wert, bei Änderungen den vollständigen Diff
und die aktuelle Version. Nur die anfordernde Person oder Procurement-Admins
können einen Entwurf bearbeiten oder einreichen. Die Ausführung verlangt
ausdrückliche Bestätigung und einen Idempotenzschlüssel; Bearbeitung und
Einreichung benötigen die exakte `expected_updated_at`-Version. Einreichung
verlangt zusätzlich `cores:procurement:submit` oder `cores:write` und die
datensatzgebundene Phrase aus der Vorschau. ProcurementCore 1.0.50 sperrt den
Entwurf bei Änderungen und schreibt Ergebnis, Aktivität und Audit atomar.

## Beschaffungsangebote ab 1.5.8

`procurement.offers.prepare_create`/`.create` und `prepare_update`/`.update`
pflegen Lieferantenangebote für bestehende Produkte. Die Vorschau prüft
Produkt, Lieferant, Preis, Packgröße, Währung, Duplikate und bei Änderungen
den vollständigen Diff samt `expected_updated_at`. `active=false` archiviert
ein Angebot, `active=true` stellt es für aktive Stammdaten wieder her.
ProcurementCore 1.0.49 speichert Angebot, Preishistorie, Audit und
Idempotenzbeleg transaktional.

## Procurement-Produktänderung ab 1.5.7

`procurement.products.prepare_update` zeigt den vollständigen Ist/Soll-Diff,
prüft Kategorie, SKU-Duplikate und offene Referenzen und liefert die genaue
`expected_updated_at`-Version. `procurement.products.update` speichert nach
Bestätigung mit `cores:procurement:update` und Idempotenzschlüssel. `active=false`
archiviert das Produkt, sofern keine offenen Bestellungen oder Bedarfe darauf
verweisen; `active=true` stellt es wieder her. ProcurementCore 1.0.48 prüft die
Version unter Datensatzsperre und speichert Änderung, Audit und Wiederholungsbeleg
in einer Transaktion. Produktanlagen erhalten denselben dauerhaften Audit- und
Idempotenzschutz; ein optionales Erstangebot wird mit dem Produkt atomar angelegt.

## Warehouse-Produktänderung ab 1.5.6

`warehouse.products.prepare_update` lädt für Warehouse-Administratoren alle
bearbeitbaren Produktfelder, zeigt den Ist/Soll-Diff und prüft Duplikate,
Stammdatenbezüge, Trackingregeln sowie die exakte Version.
`warehouse.products.update` verlangt danach `confirm_update=true`, denselben
Versionswert und einen Idempotenzschlüssel. Nullable Felder lassen sich über
`clear_fields` leeren; das vollständige Schema steht in
`cores.entities.schema` unter `update_fields`. WarehouseCore 5.9.82 speichert
Änderung, Audit-Herkunft und Wiederholungsbeleg in einer Transaktion.

## Kategorienpflege ab 1.5.5

`procurement.categories.prepare_create` und `.create` prüfen Name,
Parameterdefinitionen sowie doppelte und ähnliche Kategorien vor einer
bestätigten Anlage. `prepare_update` und `update` zeigen den vollständigen
Ist/Soll-Diff und verlangen die exakte `expected_updated_at`-Version sowie
ausdrückliche Bestätigung. Das Parameter-Schema wird als vollständige Liste
ersetzt. Beide Ausführungen laufen über die ProcurementCore-API mit deren
Administratorprüfung, Audit und dauerhafter Idempotenz.

## OAuth-Scope für Lieferantenänderungen ab 1.5.4

`cores:procurement:update` ist jetzt im OAuth-Katalog auswählbar. Ein Token
mit diesem Scope kann das geführte Lieferanten-Update aufrufen, während
`cores:procurement:create` allein dafür nicht ausreicht. Bestehende
Verbindungen müssen den neuen Scope bei Bedarf erneut freigeben.

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

Der Server bietet 67 lesende fachliche Tools, fünf wiederverwendbare Analyse-Prompts und dokumentierbare Knowledge-Ressourcen für RentalCore, WarehouseCore, PlannerCore und ProcurementCore. Bei `MCP_ENABLE_WRITES=true` kommen 58 vorbereitende und 58 bestätigte, eng begrenzte Schreibtools für alle vier Core-Services hinzu. Beliebiges SQL, generische HTTP-Aufrufe und generische Löschwerkzeuge bleiben ausgeschlossen. Unbenutzte Warehouse-Kategorien können ausschließlich über die benannten, gesondert bestätigten Löschwerkzeuge entfernt werden.

## Geführte Schreibzugriffe mit Rückfragen

- `procurement.products.prepare_create` analysiert optional einen Produktlink, führt erkannte und explizit genannte Werte zusammen, prüft Duplikate, schlägt Kategorien/Lieferanten vor und liefert `questions_for_user` für alle fehlenden Angaben.
- `procurement.products.create` legt erst an, wenn Pflichtfelder eindeutig sind, empfohlene Lücken ausgefüllt oder ausdrücklich akzeptiert wurden und `confirm_creation=true` nach einer finalen Vorschau gesetzt ist.
- `procurement.products.prepare_update` und `procurement.products.update` zeigen Änderungen mit Diff und Version; `active=false` archiviert ohne offene Referenzen, `active=true` stellt wieder her.
- `procurement.product_links.prepare_link` und `procurement.product_links.link` verbinden Procurement- und Warehouse-Produkte nach Identitäts-, Versions- und Abhängigkeitsprüfung.
- `procurement.offers.prepare_create`/`.create` und `prepare_update`/`.update` pflegen Produkt-Lieferant-Angebote mit Live-Prüfung, Version und Archivierung.
- `procurement.suppliers.prepare_create` und `procurement.suppliers.create` prüfen Code, ähnliche Namen und alle Stammdaten vor der bestätigten Anlage.
- `procurement.suppliers.prepare_update` und `procurement.suppliers.update` zeigen jede Lieferantenänderung samt Diff und Version vor der Bestätigung; `active=false` deaktiviert den Datensatz.
- `procurement.categories.prepare_create`/`.create` und `prepare_update`/`.update` pflegen Kategorien und ihre Parameterdefinitionen nach Duplikatprüfung, Vorschau und Bestätigung.
- `rental.jobs.prepare_create` löst Kunden, Status, Jobkategorie und Location gegen die Live-Daten auf. Mehrdeutige Namen werden nie geraten, sondern als Auswahl zurückgegeben.
- `rental.jobs.create` verlangt vollständige Kerndaten und dieselbe ausdrückliche Bestätigung.
- `rental.requirements.prepare_create` und `rental.requirements.create` lösen Job und Produkt eindeutig auf, prüfen die Menge und verweigern das Überschreiben eines vorhandenen Bedarfs.
- `rental.jobs.prepare_assign_device` und `rental.jobs.assign_device` prüfen Job, Gerät und bestehende Zuordnungen vor der Zuweisung.
- `rental.jobs.prepare_update` und `rental.jobs.update` zeigen Alt-/Neuzustand und verwenden für Stornos den konfigurierten Storno-Status statt Löschung.
- `rental.requirements.prepare_update` und `rental.requirements.update` ändern nur die Menge einer einzelnen Bedarfszeile.
- `procurement.orders.prepare_create` und `procurement.orders.create` validieren Lieferant, Positionen, Währung, Termine, Duplikate und Gesamtwert; die Ausführung bleibt auf Procurement-Administratoren begrenzt.
- `procurement.orders.prepare_update` und `procurement.orders.update` ersetzen nach vollständigem Diff, Versionsprüfung und Bestätigung den Bestellentwurf einschließlich Positionen.
- `procurement.orders.prepare_transition` und `procurement.orders.transition` markieren Versand/Bestätigung oder stornieren mit Approval-Scope, Version und erhöhter Bestätigung.
- `procurement.requisitions.prepare_decide` und `procurement.requisitions.decide` prüfen Status, Version und Vier-Augen-Trennung und verlangen eine ID-gebundene Bestätigungsphrase.
- `procurement.requisitions.prepare_create`/`.create`, `prepare_update`/`.update` und `prepare_submit`/`.submit` führen durch den Bedarfsentwurf bis zur Einreichung mit vollständiger Positionsvorschau, Versionsprüfung und Audit.
- `procurement.orders.prepare_receive` und `procurement.orders.receive` prüfen offene Menge, Produktverknüpfung, Seriennummern und Zielzone; die atomare Buchung erzeugt Bestand, Geräte und Putaway-Task.
- `warehouse.movements.prepare_create` und `warehouse.movements.create` buchen bestätigte Einlagerung, Ausgabe oder Transfer über den auditierten Scannerprozess.
- `warehouse.devices.prepare_update_status` und `warehouse.devices.update_status` trennen physischen Lagerstatus und Betriebszustand; Jobbewegungen können nicht umgangen werden.
- `planner.plans.prepare_create` und `planner.plans.create` prüfen Namen und Duplikate, bevor ein Plan angelegt wird.
- `planner.tasks.prepare_create` und `planner.tasks.create` erzwingen Planmitgliedschaft, Bucket-Zugehörigkeit und eine bewusste Duplikatentscheidung.
- `warehouse.tasks.prepare_create` und `warehouse.tasks.create` prüfen Aufgabentyp, Priorität, Fälligkeit sowie jede referenzierte Zone, jedes Case, Gerät, Produkt und jeden Job live.
- `warehouse.products.prepare_create` und `warehouse.products.create` prüfen das vollständige Produkt, lösen Stammdaten fuzzy auf und legen ausdrücklich freigegebene Hersteller, Marken oder Kategorieebenen zusammen mit Produkt, Anfangsbestand und Devices atomar an.
- `warehouse.products.prepare_update` und `warehouse.products.update` zeigen den vollständigen Produkt-Diff und prüfen Version, Beziehungen sowie Trackingregeln vor einer bestätigten Änderung.
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
| `MCP_ENABLE_WRITES` | `false` | Aktiviert 116 geführte Schreibtools (58 Vorschauen plus 58 Ausführungen); nur mit `MCP_AUTH_MODE=oauth` zulässig |
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

- Die 67 Abfragetools und 58 Vorbereitungstools sind `readOnlyHint=true`; Ausführungstools erzwingen einen Idempotenzschlüssel und sind `idempotentHint=true`. Zustandsänderungen sind zusätzlich als destruktiv annotiert.
- Jede DB-Abfrage läuft in einer PostgreSQL-Transaktion mit `READ ONLY`, Timeout und Zeilenlimit.
- Produktion verwendet zusätzlich die Rolle `cores_mcp` mit ausschließlich `SELECT`-Rechten.
- Schreibtools verwenden ausschließlich validierte Endpunkte des jeweils verantwortlichen Core und ein zweiminütiges, auf den OAuth-Benutzer delegiertes Suite-Token. Rollenänderungen und Kontosperren werden dort live geprüft.
- Es existieren keine Tools für beliebiges SQL, Dateien, Shell, E-Mail, generische Änderungen oder generische Löschungen. Zulässige Änderungen, Statuswechsel, Bewegungen, Bestellungen, Procurement-Freigaben und Wareneingänge sind einzelne fachliche Capabilities mit Live-Validierung, Ziel-Core-Berechtigung, finaler Vorschau und ausdrücklicher Bestätigung.
- Lesende Tools geben keine unnötigen Kontaktinformationen, Secrets oder internen privaten Notizen aus. Geführte Anlagen zeigen die vom Benutzer eingegebenen Felder im Entwurf und im Ergebnis.
- Nutzertexte aus Beschreibungen/Notizen gelten als nicht vertrauenswürdige Daten, niemals als Agent-Anweisung.
- Toolantworten enthalten Zeitstempel, Quellen und fachliche Warnungen.

Details und Threat Model: [docs/SECURITY.md](docs/SECURITY.md).

## Knowledge-Dokumente

Markdown-, Text-, CSV- und JSON-Dateien unter `MCP_KNOWLEDGE_DIRS` werden als MCP Resources und über `knowledge.documents.search` angeboten. Damit lassen sich beispielsweise Equipment-Strategie, Freigaberegeln, Herstellerstandards oder Einkaufsgrundsätze neben den Live-Daten nutzen. Symlinks und Dateien über 2 MiB werden nicht geladen.

## Entwicklung und Release

```bash
make check
docker build -t nobentie/cores-mcp:1.5.28 -t nobentie/cores-mcp:latest .
```

Die Umbrella-Compose-Datei der Cores Suite bindet den Dienst intern ein. Der Cores-Dashboard-Reverse-Proxy veröffentlicht MCP und OAuth auf derselben Domain, damit der bestehende Suite-Login genutzt werden kann.
