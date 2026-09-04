# Tool-Katalog

Alle 56 Tools sind read-only und idempotent. Suchtools akzeptieren üblicherweise `query`, `limit` und `offset`; Zeitfenster `from`, `to` und `limit`; Detailtools `id`. Datumswerte sind `YYYY-MM-DD` oder RFC3339.

## Suiteweit und Wissen (8)

| Tool | Zweck |
|---|---|
| `cores.operations.overview` | Aktuelles Lagebild über Jobs, Bestand, Defekte, Wartung, Aufgaben und Beschaffung |
| `cores.search` | Gemeinsame Suche über Produkte, Geräte, Jobs, Tasks, Lieferanten, Bestellungen, Orte und Cases |
| `cores.activity.recent` | Letzte Änderungen aus allen Cores in einem Zeitfenster |
| `cores.services.health` | Live-Healthchecks aller Suite-Dienste |
| `cores.data.dictionary` | Tabellen- und Spalteninventar der freigegebenen Cores-Daten |
| `cores.data.quality` | Entscheidungsrelevante Erfassungs- und Verknüpfungslücken |
| `knowledge.documents.list` | Verfügbare Strategie- und Referenzdokumente |
| `knowledge.documents.search` | Volltextsuche in freigegebenen Knowledge-Dokumenten |

## RentalCore (9)

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

## WarehouseCore (16)

| Tool | Zweck |
|---|---|
| `warehouse.products.search` | Produktstamm, Hersteller, Kategorie, Stock und Geräteanzahl suchen |
| `warehouse.products.get` | Produktdetail mit Geräten, Orten, Relationen, Bedarf und Beschaffungslink |
| `warehouse.products.relations` | Alternativen, Zubehör und Abhängigkeiten |
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

## PlannerCore (8)

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

## ProcurementCore (11)

| Tool | Zweck |
|---|---|
| `procurement.products.search` | Beschaffungsprodukte und Attribute suchen |
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
