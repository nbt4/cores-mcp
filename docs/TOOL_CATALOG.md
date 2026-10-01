# Tool-Katalog

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
| `rental.requirements.prepare_create` | Job und aktives Produkt eindeutig auflösen, positive Menge prüfen und vorhandenen Bedarf erkennen |
| `rental.requirements.create` | Einen bestätigten Produktbedarf additiv anlegen, ohne bestehende Mengen zu überschreiben |
| `rental.jobs.prepare_assign_device` | Job und aktives Gerät auflösen sowie vorhandene Zuweisung prüfen |
| `rental.jobs.assign_device` | Ein bestätigtes Gerät einem Job zuweisen |
| `rental.jobs.prepare_update` | Aktuellen Job laden, neue Referenzen/Termine prüfen und Stornofolgen anzeigen |
| `rental.jobs.update` | Jobdaten oder Status bestätigt ändern; Storno ersetzt keine Löschung |
| `rental.requirements.prepare_update` | Vorhandene Bedarfszeile und neue positive Menge prüfen |
| `rental.requirements.update` | Ausschließlich die bestätigte Menge einer Bedarfszeile ändern |

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
