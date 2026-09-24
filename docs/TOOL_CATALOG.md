# Tool-Katalog

Seit 1.1.1 sind alle Planner-Ergebnisse auf die aktuellen Planmitgliedschaften des
OAuth-Benutzers beschränkt. Das gilt auch für `cores.search`,
`cores.operations.overview`, `cores.activity.recent`, `cores.data.quality`,
`cores.query.records` und `cores.query.aggregate`. Administratorstatus allein
berechtigt nicht zum Lesen fremder Pläne. Maschinentokens liefern keine Planner-Daten.
Toolnamen und Eingabeschemas bleiben unverändert.

Die 62 Abfragetools sind read-only und idempotent. Bei `MCP_ENABLE_WRITES=true` werden zusätzlich siebzehn vorbereitende und siebzehn bestätigte Schreibtools für alle vier Core-Services registriert. Suchtools akzeptieren üblicherweise `query`, `limit` und `offset`; Zeitfenster `from`, `to` und `limit`; Detailtools `id`. Datumswerte sind `YYYY-MM-DD` oder RFC3339.

Der MCP-Endpunkt verlangt immer `cores:read`. Für ein Vorbereitung- oder
Ausführungstool ist zusätzlich entweder der kompatible Sammel-Scope
`cores:write` oder der passende granulare Scope erforderlich:

| Scope | Freigegebene Operationen |
|---|---|
| `cores:rental:create` | Jobs und Requirements anlegen |
| `cores:rental:update` | Job/Requirement ändern und Gerät zuweisen |
| `cores:warehouse:create` | Tasks und Produkte anlegen |
| `cores:warehouse:update` | Bewegungen und Gerätezustände buchen |
| `cores:planner:create` | Pläne und Tasks anlegen |
| `cores:procurement:create` | Produkte, Lieferanten und Bestellungen anlegen |
| `cores:procurement:update` | Lieferantenfelder ändern sowie Lieferanten deaktivieren oder reaktivieren |
| `cores:procurement:approve` | Eingereichte Bedarfe im Vier-Augen-Prinzip entscheiden |
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

## WarehouseCore (17 + 8 geführte Schreibtools)

| Tool | Zweck |
|---|---|
| `warehouse.products.search` | Produktstamm, Hersteller, Kategorie, Stock und Geräteanzahl suchen |
| `warehouse.products.get` | Produktdetail mit Geräten, Orten, Relationen, Bedarf und Beschaffungslink |
| `warehouse.products.relations` | Alternativen, Zubehör und Abhängigkeiten |
| `warehouse.master_data.resolve` | Hersteller, Marken, Kategorien, Einheiten und Lagerplätze fuzzy auflösen und Trefferart klassifizieren |
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

## ProcurementCore (11 + 12 geführte Schreibtools)

| Tool | Zweck |
|---|---|
| `procurement.products.search` | Beschaffungsprodukte tolerant suchen und mit fachlichem Kontext statt nur Codes erklären |
| `procurement.products.get` | Produkt mit Angeboten, Historie, Bedarf, Orders und Lagerlink |
| `procurement.offers.compare` | Angebote normalisiert nach Stückpreis, Packung, Mindestmenge und Lieferzeit |
| `procurement.suppliers.search` | Lieferantenleistung, Risiko, Angebote und Bestellvolumen |
| `procurement.requisitions.list` | Bedarfsanforderungen, Status, Begründung und Wert |
| `procurement.orders.list` | Bestellungen und Wareneingangsfortschritt |
| `procurement.deliveries.expected` | Offene Lieferpositionen im Zeitfenster |
| `procurement.prices.history` | Preisverlauf und Änderung zum vorherigen Wert |
| `procurement.reorder.candidates` | Produkte unter Meldebestand samt bestem Angebot |
| `procurement.spend.summary` | Bestellvolumen nach Monat, Lieferant, Status und Währung |
| `procurement.risks.list` | Lieferanten-, Angebots-, Preis- und Lieferrisiken |
| `procurement.products.prepare_create` | Produktlink analysieren, Daten zusammenführen, Duplikate prüfen und konkrete Rückfragen liefern |
| `procurement.products.create` | Bestätigtes Produkt und optional eine Bezugsquelle über die ProcurementCore-API anlegen |
| `procurement.suppliers.prepare_create` | Alle Lieferantenfelder, eindeutigen Code und ähnliche Bestandsnamen prüfen; vollständigen Entwurf und Rückfragen liefern |
| `procurement.suppliers.create` | Lieferanten nach finaler Bestätigung über die ProcurementCore-API atomar und auditiert anlegen |
| `procurement.suppliers.prepare_update` | Administratorrechte, alle Feldänderungen, ähnliche Namen und exakte Version prüfen; Ist/Soll-Diff zeigen |
| `procurement.suppliers.update` | Bestätigte Änderung oder Deaktivierung mit Versionsprüfung und transaktionalem Audit speichern |
| `procurement.orders.prepare_create` | Lieferant, Positionen, Termine, Währung, Duplikate und Gesamtwert prüfen |
| `procurement.orders.create` | Bestätigte Bestellung mit Procurement-Administratorrechten anlegen |
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
ihn für Lieferantenanlagen, Bedarfsentscheidungen und Wareneingänge transaktional und
speichert das Ergebnis zusammen mit der Fachmutation.

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
