# Cores MCP

## Mietprodukt-Positionen — MCP 1.5.62

Mietprodukt-Korrektur: echte Rental-Auftragspositionen zu `customer_price`,
separate Lieferantenkosten zu `rental_price` und bestätigter Altfall-Repair.
[Modell, Migration und Ablauf](docs/JOB_EXTERNAL_EQUIPMENT_MCP.md).

## Mietprodukte am Job zuweisen — MCP 1.5.61

Neu: `rental.job_external_equipment.prepare_create` und `.create` für die
bestätigte Zuweisung vorhandener Fremdmietprodukte unter „Mietprodukte“.
Der Katalog enthält jetzt **444 Werkzeuge**. [Ablauf und Grenzen](docs/JOB_EXTERNAL_EQUIPMENT_MCP.md).

## OAuth-Nachfreigabe für Auftragspositionen — MCP 1.5.60

Die neun `rental.job_positions`-Werkzeuge veröffentlichen ihre benötigten
OAuth-Scopes direkt im Tool-Descriptor (`securitySchemes` und `_meta`-Spiegel).
Vorschau/Anlage/Update/Archivierung benötigen den passenden Rental-Aktionsscope
und `cores:rental:financial`; `get/search` benötigen den Finanzscope, die
redigierte Historie nur den Basis-Lesescope plus unveränderte Adminprüfung.
Legacy `cores:write` erfüllt weiterhin die Aktionsprüfung, niemals den Finanzscope.

Fehlt eine Freigabe, antwortet der MCP vor jedem Geschäftsaufruf mit einem
strukturierten `_meta["mcp/www_authenticate"]`-Challenge: Metadaten-URL,
`insufficient_scope`, verständliche Beschreibung und genau die fehlenden Rechte.
Vorhandene gültige Grants werden in der erneuten Anfrage erhalten. ChatGPT kann
so die OAuth-Nachfreigabe starten und den Finanzschalter im bestehenden
Cores-Dialog anzeigen; Nutzer müssen die gewünschte Freigabe ausdrücklich
bestätigen. Token-Refresh erweitert keine Rechte, und abgelehnter Finanzzugriff
bleibt abgelehnt. Geschäftliche Bestätigungen werden dabei nicht ausgeführt
oder automatisch wiederholt; danach eine frische Positionsvorschau erstellen.

Tool-Namen, Eingabe-/Geschäftsschemas und Kataloggröße (442) bleiben stabil.
Keine neue Konfiguration oder Migration; nur MCP ausrollen und im Client die
Tool-Definitionen aktualisieren. Statische Maschinentokens sind weiterhin reine
Lesezugänge und kein PAT-Ersatz für diese persönlichen Schreibabläufe.

Der lokale HTTP-Regressionslauf prüft beide Descriptor-Felder, alle relevanten
Positionstools, alte/lesende/falsche Finanzgrants, gültige granulare und
Legacy-Schreibrechte, unveränderte Adminprüfung und signierte Owner-Vorschauen.
OAuth-Code-/Refresh-Tests prüfen ausdrückliche Finanzfreigabe und deren Ablehnung;
zusätzlich laufen vollständige Tests, Vet und PostgreSQL-Rechteprüfungen mit Race.

## Auftragspositionen — Rental 5.3.121 / Warehouse 5.9.113 / MCP 1.5.59

`rental.job_positions.prepare_create/create`, `prepare_update/update` und
`prepare_archive/archive` pflegen echte Produkt-Auftragspositionen wie **+ Produkt**.
`get`, `search` und `audit_history` ergänzen gezielte Abfragen; insgesamt **442
Werkzeuge** (112 Abfragen / 165 Vorschauen / 165 Ausführungen).

Anlage verlangt Produkt/Job sowie ausdrücklich Menge und Einzelpreis (auch 0).
Produktmengen sind positive ganze Zahlen; Preise, Faktoren, Steuer und Rabatte
haben höchstens zwei Nachkommastellen. Standardwerte: Einheit Stück,
Folgetag-Faktor 0,50, Steuer 19 %, Rabatte 0; Beschreibung aus dem Produktnamen.
Teilupdates erhalten ausgelassene Felder; Job, Produkt und Herkunft bleiben
unveränderlich. Dienstleistungen, Fremdmiet- und Paketpositionen behalten ihre
nativen Abläufe; diese Tools verwalten Produktpositionen.

Die finale Vorschau zeigt Position, Preisberechnung nach den bestehenden
Job-/Steuer-/Tages-/Rabattregeln, Auftragswert und alle Materialquellen. Bereits
vorhandene manuelle Zusatzmengen benötigen ausdrücklich `manual_quantity`
(Ersatzmenge) oder `preserve_manual_quantity=true` (bewusst zusätzlicher Bedarf).
Beispiel: 9 manuelle Bedarfe mit `quantity=9, manual_quantity=0` übernehmen ergibt
9 aus Positionen + 0 manuell, nicht 18. Die Bedarfs-ID bleibt erhalten. Bereits
vorhandene Produktpositionen verlangen bei Anlage zusätzlich `allow_duplicate=true`.
Archivierung erhält Originalfelder/Historie, entfernt nur den Positionsanteil aus
aktivem Bedarf und Umsatz und lässt unabhängige Zusatzmengen bestehen.

Alle Vorschauen/Ausführungen benötigen aktuelle Rental-Adminrechte, passenden
`cores:rental:create|update|archive`-Scope (oder Legacy-write) **und separat**
`cores:rental:financial`. `get/search` benötigen den Finanzscope; die redigierte
Historie verlangt Adminrechte und enthält keine Preise. Bestätigte Ausführungen
benötigen `expected_updated_at` bei bestehenden Positionen,
`expected_job_updated_at`, `expected_context`, die genaue
`required_confirmation_text` als `confirmation_text`, `confirm_change=true`
und `idempotency_key`. Dry-run/Vorschau schreiben nichts. Veraltete Vorschauen,
fremde aktive Bearbeiter oder widersprüchliche Gerätezuordnungen sperren.
Position, genau zugehöriger Materialbedarf, Auftragswert, Job-Historie, Audit und
dauerhafte Wiederholungsantwort sind eine Transaktion. Wiederholungen prüfen
aktuelle Zielrechte erneut. Beschaffung und physischer Bestand werden nicht geändert.

Rental-Startup `050` / Suite-Migration `047` schützt Originalidentität und Archive,
versioniert alle Positionsschreiber und verhindert Gerätezuordnung zu Archiven.
Native Entfernung archiviert; native Listen und Analytics schließen Archive aus.
Warehouse-Upgrade `063` / Suite `048` aktualisiert den Produktbeziehungskontext.
Warehouse-Picklisten, Scan-Auswahl und Packlisten berücksichtigen ebenfalls nur
aktive Positionen. Rental zuerst ausrollen, danach Warehouse und MCP; keine neuen
Umgebungsvariablen. MCP-Tool-Liste neu laden, zusätzliche Finanz-/Schreibrechte bei
Bedarf über erneute OAuth-Verbindung freigeben. Bestehende Jobs/Bedarfe werden
beim Release nicht automatisch übernommen.

Validierung: vollständige Go-Tests aller drei Dienste, PostgreSQL-Tests für
atomare Übernahme, Preis-/Rabattberechnung, Zusatzmengen, Archive, veraltete
Kontexte, Rechteentzug, Rollback und dauerhafte Wiederholungen sowie Packlisten.

## Bedarfsmeldungen nach Nutzerrechten — MCP 1.5.58

Bedarfsmeldungen und ihre Positionen sind nur für den ursprünglichen Anforderer
oder aktuell aktive Administratoren sichtbar. Die Datenbank prüft diese Rechte
bei jeder Abfrage, bevor Suche, Filter, Sortierung, Seitennavigation, Verknüpfung
oder Aggregation ausgeführt werden. Veraltete Rollen im Token erweitern keine
Rechte; Diensttokens ohne echte Nutzeridentität erhalten keine Bedarfsmeldungen.

Dies gilt für `procurement.requisitions.list`, Produktkontexte, `cores.search`,
`cores.query.records/aggregate`, operative Übersichten, Aktivitäten und
Nachbeschaffungsempfehlungen. Bedarfszahlen dieser Werkzeuge beziehen sich daher
auf die für den aktuellen Nutzer sichtbaren Meldungen. Die bestehende
Produktänderungsvorschau prüft aktive Administratorrechte ebenfalls erneut.
Der Katalog bleibt bei 433 Werkzeugen; Finanz- und Dokumentrechte sowie die
weiteren offenen Abnahmepunkte werden separat fertiggestellt.

## Vollständige Case-Abläufe — Warehouse 5.9.112 / MCP 1.5.57

Der Katalog enthält 433 Werkzeuge: 109 Abfragen, 162 Vorschauen und
162 Ausführungen. `warehouse.case_workflows` ergänzt jeweils `prepare_*`
und Ausführung für `seal`, `unseal`, `move`, `dispatch`, `return` und
`inspect_return`. `events` liest die unveränderliche physische Historie mit
`case_id`, optionalem `after_event_id` und `limit` (Standard 100, maximal 200),
auch für archivierte Cases; private Notizen und rohe Metadaten bleiben verborgen.

Jede Aktion prüft den gesamten verschachtelten Baum und benötigt aktuelle
Warehouse-Administrator-/Update-Rechte, die genaue äußere Case-Version,
`expected_context`, ausdrückliches `confirm_change`, den endgültigen
Bestätigungstext und einen `idempotency_key`. Vorschau und `dry_run` schreiben
nichts. Änderungen an Inhalt, Geräten, Produkten, Sollvorlagen, Reservierungen,
Jobs, Bearbeitungssitzungen, Aufgaben, Lagerhierarchie oder Kapazität erfordern
eine neue Vorschau. Fremde laufende Aufgaben und unvollständige physische
Komponentenverbünde sperren widersprüchliche Aktionen.

`seal` prüft verfügbare Geräte sowie Gewichtsgrenzen aller Teilbäume; bei
unvollständigen festen/hybriden Sollvorlagen ist zusätzlich ausdrücklich
`accept_incomplete_template=true` erforderlich. `unseal` öffnet einen verfügbaren
versiegelten äußeren Case. `move` benötigt einen eindeutigen
`destination_zone_id` und erhält Gerätezustände und innere Versiegelungen.
`dispatch` erfordert einen versiegelten einsatzbereiten Baum sowie einen
bestätigten, lebenden, zeitlich geplanten `job_id`; überlappende Reservierungen,
fremde ausgegebene Geräte und andere aktive Job-Bearbeiter blockieren.

`return` übernimmt den gespeicherten Job und verlangt Zielzone und ausdrücklich
`return_mode=inspect|sealed`. `inspect` führt Geräte in `return_pending` und
Cases in `return_check`; die Zone muss return/inspection/quarantine sein.
`sealed` benötigt ein erhaltenes äußeres Siegel und einsatzbereite Geräte.
`inspect_return` bestätigt mit `inspection_passed=true` eine abgeschlossene
physische Prüfung des gesamten Baums; offene Wartung/Defekte sperren weiterhin.
Diese Bestätigung verändert keine Zustandsbewertung und schließt keine Aufgaben.

Case-/Gerätestatus, Job-Gerätezuordnung, Bewegungen, Ereignisse für alle
betroffenen Cases, Job-Historie, Audit und dauerhafte Antwort sind atomar.
Physische Mitgliedschaften, Mengenbestand, Sollvorlagen, Reservierungszuordnungen
und Aufgaben bleiben erhalten. Warehouse-Migration 062 / Suite-Migration 046
verhindert Case-Harddelete und Änderungen/Löschen physischer Ereignisse.
Native Case-Entfernung archiviert mit atomarer Historie/Audit, reserviert IDs
und deaktiviert Scan-Aliase; Standardlisten blenden Archive aus.
Auch bestehende MCP-Metadatenaktionen prüfen aktuelle Eigentümerrechte vor Replay.
Warehouse samt Migration zuerst ausrollen, danach MCP. Die Gesamtissues bleiben
für die übrigen [Abnahmepunkte](docs/ISSUE_COMPLETION.md) offen.

## Case packen und entpacken — Warehouse 5.9.109 / MCP 1.5.54

Der aktuelle Katalog umfasst 420 Werkzeuge: 108 Abfragen, 156 Vorschauen und
156 Ausführungen. `warehouse.case_contents.get` zeigt den vollständigen,
begrenzten physischen Case-Baum mit Geräte- und Mengenbestand und Versionen.
Für `pack_device`, `pack_product`, `pack_case`, `unpack_device`,
`unpack_product`, `unpack_case` und `unpack_all` stehen jeweils separate
`prepare_*`- und Ausführungswerkzeuge bereit.

Jede Aktion benötigt `case_id` und genau die passende Geräte-, Artikel- oder
Child-Case-ID. Mengenaktionen benötigen eine positive Menge mit höchstens drei
Dezimalstellen; `pack_product` zusätzlich den eindeutigen `source_zone_id`.
Entpacken erfordert einen ausdrücklichen `destination_zone_id`; `unpack_all`
lagert auch den leeren äußeren Case dort ein und erhält versiegelte Child-Cases
mit ihrem vollständigen Inhalt. Der äußere Case muss aktiv, geöffnet,
verfügbar und nicht selbst verschachtelt sein. Reservierungen, Aufgaben und
Sollvorlagen bleiben erhalten. Ausgegebene Geräte, Defekte, Wartung, fremde
Case-Zuordnungen, widersprüchliche Aufgaben und verschlossene Vorfahren blockieren
unzulässige Änderungen.

Die Vorschau prüft den gesamten Inhalt, Artikel-/Geräte-/Job-/Aufgabenversionen,
Lagerhierarchie, Profil, Kapazität, Volumen und Gesamtgewicht. Der finale
Kontext, die genaue Case-Version und der zurückgegebene Bestätigungstext müssen
zur ausdrücklichen Bestätigung passen. Aktuelle Administrator-/Update-Rechte
werden auch bei gespeicherten Wiederholungen geprüft. Inhalt, Bestand,
Gerätebewegungen, Case-Ereignis, Audit und dauerhafte Antwort werden gemeinsam
gespeichert oder vollständig zurückgerollt. Vorschau und Dry-Run schreiben nichts.

Warehouse-Migration 061 / Suite-Migration 045 schützt physische Inhalte vor
Änderungen in gesperrten Case-Bäumen und synchronisiert Gerätezuordnungen.
Mengenartikel in Cases zählen zum Gesamtbestand; Packen und Entpacken erhalten
diesen Bestand auch beim Neustart. Nach der Legacy-Übernahme werden
Case-Modelle beim Start nicht mehr aus Case-Namen zugewiesen. Signierte MCP-Delegationen können die
ungeprüften Scanner-/Entpack-Altwege auch ohne Origin-Header nicht benutzen.
Zuerst Warehouse samt Migration bereitstellen, danach MCP. Vollständige
Case-Abläufe ergänzen den oben dokumentierten Folgerelease.

## Case-Sollvorlagen — Warehouse 5.9.108 / MCP 1.5.53

`warehouse.case_templates.list` liefert aktive Sollzeilen, Artikel, tatsächliche
Packmengen, Vollständigkeit und genaue Versionen; `include_archived` zeigt
auf Wunsch erhaltene Archive. Separate `prepare_create/create`,
`prepare_update/update`, `prepare_archive/archive`, `prepare_restore/restore`
und `audit_history` ergänzen den vollständigen Vorlagen-Lebenszyklus.

Anlegen erfordert `case_id`, einen aktiven physischen `product_id` und eine
positive Sollmenge mit höchstens drei Dezimalstellen; Einzelgeräte benötigen
Ganzzahlen. Änderungen und Lebenszyklus verwenden die unveränderliche
`template_line_id`. Archivierung erhält ID, Menge und Historie. Zum Editieren
muss der Case aktiv, geöffnet, nicht verschachtelt und außerhalb eines Jobs sein.
Die final geprüfte Case-Version und der vollständige Vorlagen-/Artikel-/Inhalt-
Kontext binden die ausdrückliche Bestätigung. Aktuelle Administrator- und
Create/Update/Archive-Rechte werden auch bei gespeicherten Wiederholungen geprüft.
Vorlage, Audit und dauerhafte Antwort werden gemeinsam gespeichert oder komplett
zurückgerollt. Sollmengenänderungen bewegen keinen physischen Bestand.

Warehouse-Migration 060 / Suite-Migration 044 schützt auch native Schreiber,
verhindert dauerhaftes Löschen und beendet die wiederholte Legacy-Übernahme beim
Neustart. Unveränderte Lager- und Kabelreferenzen behalten beim Start
ihre genauen Versionen, auch für archivierte Artikel. Die bestehende UI
archiviert entfernte Sollzeilen; aktive Case-Ansichten
und Vollständigkeitsprüfungen berücksichtigen nur aktive Vorlagen. Zuerst
Warehouse bereitstellen und Migration prüfen, danach MCP aktualisieren.
Weitere ausstehende Case-Workflows bleiben im Issue-Prüfplan offen.

Dieser Vorlagen-Release umfasste 405 Werkzeuge: 107 Abfragen, 149 Vorschauen und
149 Ausführungen.

## Produkt-Batch und Hersteller-URL — Warehouse 5.9.107 / MCP 1.5.52

Der Katalog umfasst 395 Werkzeuge: 105 Abfragen, 145 Vorschauen und 145
Ausführungen. `warehouse.products.prepare_bulk_create/bulk_create` verarbeitet
1–100 vollständige Produktentwürfe in einer Transaktion. `cores.entities.schema`
für `warehouse.product_batches` beschreibt alle erlaubten Anlagefelder.

Zuerst je Produkt `warehouse.products.prepare_create` für Fuzzy-Auflösung und
Rückfragen nutzen; anschließend dessen native `draft`-Felder in `products`
übernehmen. Vorhandene Stammdaten mit ID referenzieren. `*_name_input` bestätigt
die Anlage eines fehlenden exakten Stammdatensatzes ausdrücklich; neue Kategorien
benötigen `category_abbreviation_input`. Gemeinsame Stammdaten werden einmal
angelegt, widersprüchliche Definitionen blockiert. Unvollständige empfohlene
Angaben erfordern pro Produkt `accept_incomplete`, ähnliche Artikel eine explizite
Prüfung mit `allow_similar_product`; bestehende eindeutige Kennungen einschließlich
archivierter Artikel werden niemals dupliziert.

Die reine Batch-Vorschau zeigt alle Produkte, Referenzen, Stammdatenpläne,
Datenlücken und gemeinsame Lagerkapazität. Aktuelle aktive Administratorrechte,
Create-Scope, unveränderter `expected_context`, `confirm_creation`, exakte
`CREATE WAREHOUSE PRODUCT BATCH ...`-Phrase und Idempotenzschlüssel sind Pflicht.
Preise benötigen zusätzlich ausdrücklich `cores:warehouse:financial`. Produkte,
Hersteller, Marke, Kategoriehierarchie, optionale Procurement-Zuordnung,
Anfangsbestand/Geräte (zusammen höchstens 1000), Audit und dauerhafter Beleg
werden zusammen gespeichert. Ein Fehler rollt alle Datensätze zurück. Dieselbe
Anfrage mit demselben Schlüssel liefert auch nach Neustart den ursprünglichen
Beleg; aktuelle Rechte werden vor jeder Wiederholung erneut im Eigentümer geprüft.
`dry_run` und Vorbereitung erzeugen weder Datensätze noch IDs/Audits/Belege.

`warehouse.products.prepare_create` akzeptiert nun `product_url` einer öffentlichen
HTTP(S)-Produktseite. Warehouse liest begrenzte HTML-/Schema.org-Produktdaten ohne
Shop-Login, Bilddownload oder Warenkorbzugriff. Interne Ziele, Zugangsdaten,
abweichende Ports und zu viele Weiterleitungen sind gesperrt. Erkannte Maße werden
nur mit bekannter Einheit in kg/cm umgerechnet. Mehrdeutige Produktseiten werden
abgewiesen. Ausdrücklich eingegebene Werte haben Vorrang; Text bleibt ungeprüfte
Geschäftsdaten, keine Anweisung. Quelle und ein `creation_input` ohne URL werden
zurückgegeben. Dieser eingefrorene Entwurf wird erneut geprüft und bestätigt;
`create` mit noch gesetzter URL wird abgewiesen, damit die Anlage keine veränderte
Produktseite neu einliest. Für gemeinsame Anlage die geprüften nativen Entwürfe
an die Batch-Vorschau übergeben. Es gibt keine generischen Datei-/HTTP-/SQL-Tools.

Keine neue Schema-Migration. Warehouse zuerst ausrollen, dann MCP. Die übrigen
Anforderungen der Eltern-Issues #4 und #5 bleiben offen.

## Adam-Hall-Warenkorb und Bestellung getrennt bestätigen — Procurement 1.0.76 / MCP 1.5.51

Vier benannte Werkzeuge ergänzen den Lieferantenablauf:
`procurement.orders.prepare_build_adam_hall_cart/build_adam_hall_cart` und
`procurement.orders.prepare_send_adam_hall/send_adam_hall`.
Der Katalog umfasst 393 Werkzeuge: 105 Abfragen, 144 Vorschauen, 144 Ausführungen.
Nur aktuelle aktive Administratoren mit explizitem `cores:procurement:send`
dürfen Vorschau, Ausführung und Wiederholung nutzen; allgemeines Schreiben,
Create, Update und Approve ersetzen dieses Recht nicht.

Die erste reine Vorschau bindet vollständigen lokalen Entwurf, aktive Referenzen,
Lieferanten-Artikelnummern und ganze Mengen an exakte Version, Kontext und
`BUILD ADAM HALL CART ORDER ...`-Phrase. Erst `confirm_cart` plus unverändertem
Idempotenzschlüssel bereitet den externen Warenkorb vor. Fremde vorhandene
Positionen werden niemals gelöscht; ein abweichender Warenkorb blockiert den
Ablauf. Diese Aktion bestellt nichts und verändert keinen Lagerbestand.

Die zweite reine Vorschau liest ausschließlich den gespeicherten Lieferanten-
Warenkorb. Vollständige Positionen, EUR-Preise/Gesamtbetrag, Geschäfts-Liefer-
und Rechnungsadresse, Zahlungs-/Versandart und vorgeschlagene lokale Preise
müssen sichtbar geprüft werden. Der gespeicherte Checkout gilt höchstens
15 Minuten und verliert bei lokalen Artikel-/Entwurfs-/Kontoänderungen seine
Gültigkeit. Exakte Checkout-ID, Version, Kontext, `SEND ADAM HALL ORDER ... EUR
... CHECKOUT ...`-Phrase, `confirm_send` und eine eigene Idempotenzkennung
bestätigen die kostenpflichtige Bestellung. Vor dem einzigen Bestellaufruf wird
derselbe private Lieferanten-Warenkorb erneut vollständig geprüft; geänderte
Preise, Adressen oder Methoden blockieren den Versand. Er wird nicht neu gebaut.

Native `014` / Root `043` bewahren Warenkorbauftrag und geprüfte Antwort mit
unveränderlichen Identitäten und Audit. Der private Lieferanten-Kontext wird
AES-GCM-verschlüsselt gespeichert, an Checkout/Auftrag/Nutzer/Kontext gebunden
und niemals ausgegeben oder auditiert. Schlüsselgrundlage ist das gemeinsame
JWT-Secret; Schlüsselwechsel macht bestehende private Checkouts unlesbar und
erfordert eine neue ausdrücklich bestätigte Warenkorbvorbereitung.

Der dauerhafte bezahlte Übermittlungsauftrag, geprüfte lokale Preise mit
unveränderten Positions-IDs, Audit und Wiederholungsbeleg werden vor dem externen
Aufruf atomar gespeichert. Lieferantenantwort und Abschluss werden in eigenen
vollständig auditierten Phasen gesichert. Wiederholung derselben ursprünglichen
Anfrage liest/finalisiert ausschließlich gespeicherte Ergebnisse. Ungewisse
Antworten oder verlorene Ergebnisspeicherung blockieren jede erneute Übermittlung;
die bestehende menschliche Klärung entscheidet den weiteren Geschäftsstatus.
Es gibt keine automatische Ersatzbestellung oder Rücksetzung.

Auch der native Bestelldialog liest beim Öffnen nur eine lokale Vorschau und
verlangt beide Bestätigungen getrennt. Er zeigt Preise und Geschäftsadressen
vor dem bezahlten Schritt, übernimmt die geprüften Preise und behält nach
Verbindungsfehlern exakt dieselbe Anfragekennung. Bestehende unbestätigte native
POSTs erzeugen nur eine Vorschau; signierte MCP-Delegationen können native UI-
Endpunkte auch durch Weglassen des Origin-Headers nicht umgehen.
Die übrigen Abnahmepunkte von #4/#5 bleiben offen.

## Ungewisse Übermittlungen menschlich klären — Procurement 1.0.75 / MCP 1.5.50

`procurement.orders.prepare_reconcile_submission/reconcile_submission` dokumentiert
eine abgeschlossene menschliche Prüfung im korrekten Lieferantenkonto. `found_order`
verlangt die gefundene Lieferanten-Bestellnummer; `confirmed_not_sent` verlangt
nachvollziehbare Geschäftsevidenz und storniert den ursprünglichen Auftrag.
Ein Timeout, eine leere Suche oder eine KI-Vermutung beweist keine Nichtübermittlung.
Die Werkzeuge kontaktieren den Lieferanten nicht, erzeugen keine Bestellung und
buchen keinen Bestand. Neue Nachfrage benötigt einen separat freigegebenen Warenkorb.

Aktueller aktiver Admin und expliziter `cores:procurement:send`-Scope gelten vor jeder
Vorschau, Ausführung und Wiederholung. Frühestens 15 Minuten nach dem ursprünglichen
Übermittlungsauftrag darf geklärt werden; bekannte positive Bestätigungen, vorhandene
Lieferantenbestätigungen/Wareneingänge und bereits geklärte Aufträge blockieren den
Ablauf. Vollständiger Auftrag, ursprünglicher Übermittlungsauftrag/geprüfter Payload,
Referenzen, Ergebnis, Evidenz und erwartete Wirkung binden Version/Kontext und die
exakte `RECONCILE ... HUMAN VERIFIED ...`-Phrase. Zusätzlich sind `human_verified`,
`confirm_reconcile` und ein unveränderter Idempotenzschlüssel erforderlich.

Originalanspruch, Lieferantenantwort und Positionen bleiben erhalten. Klärungsbeleg,
Bestellstatus, vollständiger MCP/AI-Audit, native Aktivität und Wiederholungsantwort
werden atomar gespeichert. Native `013` / Root `042` schützen Klärungsbelege gegen
Änderung/Löschung; der ursprüngliche Auftrag bleibt gegen erneuten Versand gesperrt.
Historische Übermittlungsbelege behalten ihr damaliges Ergebnis; der Klärungsbeleg
und aktuelle Auftrag dokumentieren die spätere menschliche Entscheidung. Zeitwerte
der Übermittlungs-/Klärungsbelege werden unabhängig von der Host-Zeitzone in UTC
angelegt. Katalog: 389 Werkzeuge (105 Abfragen / 142 Vorschauen / 142 Ausführungen).
Adam-Hall-Übermittlung und die übrigen Abnahmepunkte von #4/#5 bleiben offen.


## Amazon-Bestellungen sicher absenden — Procurement 1.0.74 / MCP 1.5.49

`procurement.orders.prepare_send_amazon/send_amazon` übermittelt einen vollständig
geprüften Amazon-Business-Bestellentwurf. Nur aktuelle Administratoren mit dem
expliziten Scope `cores:procurement:send` dürfen diese Werkzeuge verwenden;
`cores:write`, Create, Update, Approve und Bedarf-Submit reichen nicht aus.
Die OAuth-Freigabe erklärt dieses gesondert angefragte Senderecht in Deutsch und
Englisch. Lesender Zugriff bleibt voreingestellt. Für Ausführung und Replay gelten
dieselben aktuellen Rechte; die konkrete bezahlte Bestellung wird erneut bestätigt.

Die reine Vorschau bindet vollständigen Auftrag/Positionen, den ursprünglichen
freigegebenen Bedarf, aktive Referenzen, den tatsächlichen Lieferanten-Payload,
EUR-Gesamtbetrag, Liefer- und Rechnungsadresse, Konto-Fingerprint und Test-/Live-Modus
an exakte Version, vollständigen Kontext und eine Auftrag/Betrag/Kontext-Phrase.
Ein ursprünglicher Bedarf mit Entscheidung eines anderen Nutzers ist zwingend;
Artikelreferenzen, Mengen und Preise müssen seinem freigegebenen Warenkorb entsprechen.
Gemeinsame Secrets und authentifiziertes cXML werden weder ausgegeben noch gespeichert.
Vorschau und Dry-run kontaktieren den Lieferanten nicht und verändern keine Daten.

Vor dem einmaligen externen Aufruf werden ein eindeutiger dauerhafter Übermittlungs-
auftrag, der vollständige geprüfte Geschäftskontext, der native Status, Audit und
Idempotenz-Beleg atomar gespeichert. Eine Lieferantenbestätigung wird separat
mit Audit/Aktivität gesichert; danach werden lokaler Bestellstatus, Audit und
Erfolgsbeleg atomar abgeschlossen. Ein später lokaler Fehler lässt sich mit dem
ursprünglichen unveränderten Schlüssel aus der gespeicherten Bestätigung abschließen,
ohne erneut zu senden. Die Datenbank und Amazon können keine gemeinsame Transaktion
bilden. Bei Timeout, verlorener Antwort oder Prozessabbruch bleibt `pending` bzw.
`submission_unknown` erhalten: im Amazon-Business-Konto prüfen und niemals automatisch
erneut bestellen. Fehlerantworten weisen ausdrücklich auf den möglichen externen
Auftrag hin. Neuer Schlüssel, Neustart oder Zurücksetzen des alten API-Status umgehen
die dauerhafte Sperre nicht.

Native `012` / Root `041` bewahren Übermittlungsidentität, geprüften Kontext und
Originalpositionen; sie blockieren Löschung, kommerzielle Positionsänderung und
Zurücksetzen zur erneuten Übermittlung für alle Schreiber. Wareneingang und
Bestandsbuchung bleiben eigene Vorgänge. Der bestehende UI-Amazon-Aufruf verwendet
jetzt dieselben dauerhaften Phasen und Audits; der alte ungeführte MCP-Aufruf verlangt
neue Vorbereitung (428). Der Katalog enthält 387 Werkzeuge (105 Abfragen / 141
Vorschauen / 141 Ausführungen). Adam-Hall-Übermittlung und weitere Abnahmepunkte von
#4/#5 bleiben offen. Produktion wird ausschließlich lesend geprüft; Übermittlungstests
verwenden einen lokalen TLS-Lieferanten-Mock mit isolierten Testzugängen.

## Bedarf in Bestellung umwandeln — Procurement 1.0.73 / MCP 1.5.48

`procurement.requisitions.prepare_order/order` wandelt einen bereits freigegebenen,
aktiven Bedarf genau einmal in einen Lieferanten-Bestellentwurf um. Erforderlich
sind aktuelle Administratorrechte, die signierte Aktion `cores:procurement:create`,
die exakte Bedarfs-Version, der vollständige Vorschau-Kontext und die an Bedarf,
Lieferant und Kontext gebundene Bestätigungsphrase. Auch gespeicherte und im MCP
zwischengespeicherte Ergebnisse prüfen die aktuellen Rechte erneut.

Die Vorschau enthält sämtliche ursprünglichen Felder und Positionen, den gewählten
Lieferanten, Produkt-/Lieferanten-Versionen, alle Angebotskandidaten, die ausgewählten
Angebote und den vollständigen neuen Bestellentwurf. Die native Preisregel bleibt
bestehen: Das günstigste aktive Lieferantenangebot liefert den Preis, sofern der
Bedarf keinen Schätzpreis für diesen bevorzugten Lieferanten vorgibt. Bei gleichen
Preisen entscheidet die Angebots-ID. Eine fehlende Einkaufs-URL kommt aus dem Angebot.
Abgelaufene Angebote, abweichende Preiswährung, Mindestmengen und Verpackungseinheiten
müssen vorab korrigiert werden; Bedarfsmengen werden niemals stillschweigend gerundet.
Amazon-Bedarfe behalten Sitzung und Lieferanten-Positionsreferenzen und müssen den
ursprünglichen Amazon-Business-Lieferanten verwenden. Die separate Lieferantenübermittlung
wird durch diese Umwandlung nicht ausgelöst.

Bestellentwurf, Bedarfsstatus `ordered`, beide vollständigen Audits, beide nativen
Aktivitäten und dauerhafte Wiederholungsantwort sind eine Transaktion. Ursprüngliche
Bedarfsfelder, Freigabe und Positions-IDs bleiben erhalten. Gleichzeitige Aufrufe
erzeugen nur eine Bestellung. Der vorhandene UI/API-Ablauf prüft den Bedarf ebenfalls
erst unter derselben Transaktionssperre und schreibt beide Audits atomar; der alte
ungeführte MCP-Zugriff erhält 428 und muss neu vorbereitet werden. Vorschau und Dry-run
ändern keine Daten. Es werden weder Lieferanten-Nachrichten versendet noch Bestände
gebucht. Der Katalog umfasst 385 Werkzeuge (105 Abfragen / 140 Vorschauen / 140
Ausführungen). Keine neue Migration. Die übrigen Abnahmepunkte von #4/#5 bleiben offen.

## Vollständige Bestellentwürfe — Procurement 1.0.72 / MCP 1.5.47

Die vier bestehenden Create-/Update-Werkzeuge für Bestellungen delegieren an
`/api/v1/mcp/order-drafts/{create|update}`. Aktuelle aktive Administratorrechte
und die signierte passende Aktion gelten vor jedem neuen, gecachten, alten oder
nach Neustart gespeicherten Ergebnis. Vollständige Kopfdaten, Positionswerte,
kanonische Gesamtwerte, aktive Lieferanten/Produkte, ursprünglicher Bedarf,
Wareneingänge und doppelte Lieferanten-Bestellnummern gehören zur exakten Vorschau.
Ausführung verlangt `expected_context`, bei Änderung `expected_updated_at`, die
vollständige gebundene Phrase, `confirm_creation` bzw. `confirm_update` und einen
Idempotenzschlüssel. Vorschau und Dry-Run verändern keine Daten.

Ausgelassene Positionen behalten IDs und native Zusatzfelder; eine vollständige
Positionsliste kann vorhandene `line_id` erhalten und neue Zeilen ergänzen.
Vorschau und Speicherung verwenden dieselbe native ID-Reihenfolge. Teilmengen
und Preise berechnen denselben Gesamtwert wie der native Bestellprozess.
Create verwendet Datumswerte in YYYY-MM-DD, Update RFC3339; leere Update-Daten
löschen nullable Datumsfelder. Bestellentwürfe versenden keine externen Bestellungen.
Empfangene, bestätigte, archivierte und Amazon-PunchOut-Datensätze bleiben für
Entwurfsänderungen gesperrt. Änderung, Audit, Aktivität und dauerhaftes Ergebnis
werden atomar gespeichert. Alte erfolgreiche Geschäftsbelege bleiben mit dem
ursprünglichen nativen Payload abrufbar; neue öffentliche Legacy-MCP-Entwürfe
verlangen eine neue Vorbereitung. Falls eine alte Lieferantensuche nach Umbenennung
nicht mehr auflösbar ist, die originale Lieferanten-ID aus dem Beleg verwenden.

Keine neue Migration; Native 011 / Suite 040 und sämtliche vorhandenen Guards
bleiben erforderlich. Der Katalog enthält weiterhin 383 Werkzeuge. Weitere
Akzeptanzpunkte der Issues bleiben offen.

## Vollständige Bedarfsentwürfe — Procurement 1.0.71 / MCP 1.5.46

Die sechs bestehenden Bedarfswerkzeuge für Create, Update und Submit delegieren
an `/api/v1/mcp/requisitions/{create|update|submit}`. Die geschlossene API prüft
aktuelle aktive Nutzerrechte vor neuen und gespeicherten Ergebnissen. Änderungen
und Einreichung verlangen den ursprünglichen Anforderer oder einen aktuellen
Administrator sowie die signierte passende Aktion. Eine vollständige Vorschau
bindet alle Felder, Positionswerte, aktuelle Produkt-/Lieferantenreferenzen,
Duplikate, exakte Version und `expected_context` an die Bestätigungsphrase.
`confirm_creation`, `confirm_update` bzw. `confirm_submit` und ein gültiger
Idempotenzschlüssel geben die geprüfte Ausführung frei. Dry-Run bleibt rein lesend.

Ausgelassene Positionen behalten IDs und native Zusatzfelder. Eine vollständige
explizite Positionsliste kann vorhandene `line_id` erhalten und neue Positionen
ergänzen. Rückgabe → Überarbeitung → Einreichung erhält die Entscheidungshistorie
und löscht bei Einreichung die aktive vorige Entscheidung. Gleichnamige eigene
Bedarfe werden als Duplikate gezeigt; `allow_duplicate` gilt nur für bewusst
getrennte Neuanlagen. Änderung, Audit, Aktivität und dauerhafter Erfolgsbeleg
werden atomar gespeichert. Erfolgreiche alte Geschäftsbelege bleiben mit ihrem
ursprünglichen Payload abrufbar; neue Aufrufe alter öffentlicher MCP-Pfade müssen
neu vorbereitet werden. Wiederholungen prüfen erneut die aktuellen Rechte.

Native Migration 011 / Suite 040 sperrt aktive Referenzen für sämtliche
Schreibwege und schließt das Rennen zwischen Validierung und Katalogarchiv.
Historische abgeschlossene Datensätze behalten ihre Referenzen. Der Katalog
enthält weiterhin 383 Tools. Andere Akzeptanzpunkte der Issues bleiben offen.

## Kontextgebundene Freigaben — Procurement 1.0.70 / MCP 1.5.45

`procurement.requisitions.prepare_decide/decide` und
`procurement.orders.prepare_transition/transition` delegieren direkt an die
abgeschlossene Owner-API `/api/v1/mcp/approvals/{requisitions|orders}`.
Sie prüfen den aktuellen aktiven Administrator; Bedarfsentscheidungen verlangen
auch beim Replay einen anderen Nutzer als den ursprünglichen Anforderer.
`cores:procurement:approve` bleibt ausdrücklich erforderlich. Vollständige
Originalfelder, Positionen, Katalog-/Elternzustand und Wareneingänge sowie die
konkrete Entscheidung und Begründung gehören zur exakten Vorschau. Ausführung
verlangt `expected_updated_at`, `expected_context`, die vollständige kontextgebundene
Bestätigungsphrase, die passende `confirm_*`-Freigabe und Idempotenzschlüssel.
Vorschau/Dry-Run verändern keine Daten; Änderung, Audit, Aktivität und dauerhaftes
Ergebnis werden zusammen verbucht. Wiederholungen prüfen erneut aktuelle Rechte.

Genehmigen, Ablehnen und Zurückgeben sowie Senden, Bestätigen und Stornieren sind
getrennte fachliche Aktionen. `sent` dokumentiert den Status und versendet keine
Lieferantennachricht. Storno erhält bestehende Wareneingänge, Bestand und separate
Einlagerungspflichten. Amazon-PunchOut muss den nativen Bestellprozess verwenden.
Bereits erfolgreiche alte Aufrufe wiederholen ihren ursprünglichen Geschäftsbeleg;
ungebuchte alte Vorschauen müssen neu vorbereitet werden. Die alten öffentlichen
MCP-Entscheidungs-/Statuspfade erlauben ausschließlich solche Erfolgs-Replays.

Restore erhält auch bei empfangenen und stornierten Bestellungen den ursprünglichen
Status; die Validierung prüft Geschäftsfelder unabhängig vom initialen Neuanlage-
Status. Vollständige Workflow-Vorschauen sind auf 512 KiB pro Originaldatensatz
begrenzt. `workflow_fields` enthält separate Entscheidungs-/Einreichungs-/Status-
Schemas. Es gibt keine neue Migration und weiterhin 383 Werkzeuge.
Andere Punkte der beiden Issues bleiben offen.

## Bedarfs- und Bestellarchive — Procurement 1.0.69 / MCP 1.5.44

Für `procurement.requisitions` und `procurement.orders` gibt es jeweils
`prepare_archive/archive`, `prepare_restore/restore` und `audit_history`.
Die geschlossene API `/api/v1/mcp/workflows/{requisitions|orders}/{archive|restore}`
erhält ursprünglichen Status, sämtliche Geschäftsfelder, Positions-IDs und
Wareneingangshistorie. Die Vorschau zeigt das vollständige Original und aktuelle
Abhängigkeiten. Ausführung verlangt Archiv-Scope, exakte Mikrosekunden-Version,
`expected_context`, datensatz-/kontextgebundene Bestätigungsphrase,
`confirm_change` und Idempotenzschlüssel. Aktuelle Rechte werden auch bei
Wiederholung geprüft: Bestellungen verlangen Administratorrechte, Bedarfe den
ursprünglichen Anforderer oder Administrator. Audit, Aktivität und Erfolgsbeleg
werden zusammen mit der Änderung verbucht.

Offene Bestellungen blockieren Bedarfsarchive. Bestellungen können nur als
Entwurf, vollständig empfangen oder storniert archiviert werden; offene zugehörige
Einlagerungsaufgaben blockieren ebenfalls. Restore prüft ursprüngliche aktive
Produkte, Lieferanten und gegebenenfalls den ursprünglichen Bedarf. Operative
Listen schließen Archive aus. Migration `010` / Root `039` schützt alle Schreiber,
erhält Identitäten und lässt auch Positionsänderungen die Elternversion erhöhen.
Bereits bestehende Einlagerungszuordnungen werden aus Wareneingangs-Audits übernommen.

Zurückgegebene Bedarfe können ausdrücklich überarbeitet werden und werden dabei
wieder zum Entwurf. Erneutes Einreichen entfernt die vorherige Entscheidungsfreigabe;
deren Historie bleibt im Audit erhalten. Der Katalog umfasst 383 Werkzeuge
(105 Abfragen / 139 Vorschauen / 139 Ausführungen). Die übrigen Abnahmepunkte
der Issues #4/#5 bleiben offen.

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


## Beschaffungskategorien — MCP 1.5.42

`procurement.categories.prepare_archive/archive`, `prepare_restore/restore` und
`audit_history` ergänzen fünf Werkzeuge: 373 insgesamt (103 Abfragen / 135
Vorschauen / 135 Ausführungen). Die identische Eigentümer-API erhält das vollständige
Parameterschema und alle ursprünglichen Geschäftsfelder. Aktive Produkte blockieren
Archivierung. Produktanlage/Restore benötigen aktive Kategorien; normale Pflege
archivierter Kategorien muss zuerst eine separate Wiederherstellung durchführen.
`cores.master_data.resolve` zeigt archivierte Kategorien und Lieferanten als
`restoration_required` ohne automatische Auswahl. Exakte Version/Kontext/Bestätigung,
aktuelle Admin-/Archiv-Rechte und atomare Historie/Audit/Replay bleiben Pflicht.

Einheit und Auswahloptionen einer Parameterdefinition sind im MCP-Eingabeschema
tatsächlich optional; Auswahlparameter benötigen weiterhin gültige Optionen.

Die OAuth-Freigaben und Browseroberfläche bleiben wie in 1.5.41; vorhandene
Erfolgsbelege sind auch nach dem Upgrade wiederholbar und prüfen aktuelle Rechte.

## Procurement-Katalogarchive und OAuth — MCP 1.5.41

Der Katalog umfasst 368 Werkzeuge (102 Abfragen / 133 Vorschauen /
133 Ausführungen). Für `procurement.suppliers`, `procurement.products` und
`procurement.offers` ergänzen `prepare_archive/archive`, `prepare_restore/restore`
und `audit_history` die vorhandene Anlage und Pflege. Die Eigentümer-API bewahrt
alle ursprünglichen Felder und prüft exakte Version, vollständigen Kontext,
datensatzgebundene Bestätigung und aktuellen aktiven Administrator mit
`cores:procurement:archive`. Offene Bestellungen/Bedarfe blockieren betroffene
Archive; Wiederherstellung eines Angebots benötigt aktive Eltern. Änderung,
Audit, native Aktivität und dauerhafte Idempotenzantwort sind atomar. Auch
wiederholte erfolgreiche Aufrufe prüfen die aktuellen Rechte im Eigentümer-Core.
Audit-Aliasse liefern redigierte Ereignisse samt Ergebnisversion und bleiben
bei deaktivierten Schreibwerkzeugen verfügbar; aktuelle Rollen werden aus der
Datenbank gelesen.

OAuth veröffentlicht und akzeptiert nun auch `cores:rental:archive`,
`cores:procurement:archive` und `cores:rental:financial`. Finanzfreigabe bleibt
separat und standardmäßig deaktiviert. Die Einwilligungsseite nennt ausschließlich
die angefragten Finanzbereiche; Schreibzugriff und Rollen sind zusätzlich nötig.
Bereits ausgestellte Zugriffstoken bekommen keine neuen Scopes: Die Verbindung
muss die gewünschten Scopes erneut mit Nutzereinwilligung autorisieren.
Werkzeug-Metadaten werden nach dem dokumentierten
[Refresh-/Rescan-Verfahren](#tool-katalog-in-ki-apps-aktualisieren) aktualisiert.

Browsernachweise: [Deutsch / Light / Mobil](docs/screenshots/catalog-consent-de-light-mobile.png)
und [Englisch / Dark / Desktop](docs/screenshots/catalog-consent-en-dark-desktop.png).
Alle vier Breakpoints, beide Sprachen und Themes sowie Tastaturfokus,
keine horizontale Überbreite und sichere Vorgaben sind im Browser geprüft.

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

Existing atomic job receipts remain replayable after the requirement schema upgrade. Previously prepared unversioned requirement writes must be prepared again with the new record, parent-job and context versions before confirmation.

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

## Tool-Katalog in KI-Apps aktualisieren

Cores stellt den aktuellen Katalog nach dem MCP-Rollout über `tools/list` bereit.
Der stateless Streamable-HTTP-Transport hält keine dauerhafte Sitzung für
`notifications/tools/list_changed`; laufende Chats übernehmen Schemaänderungen
abhängig vom jeweiligen Client.

Bei **veröffentlichten ChatGPT-Plugins** prüft OpenAI den gehosteten MCP täglich.
Neue oder geänderte Tools werden nach den automatischen Prüfungen freigeschaltet.
Eine sofortige Prüfung erfolgt im Plugin-Portal: Plugin → MCPs → Server →
Issues → Rescan. Zurückgehaltene Tools bleiben bis zur Freigabe unzugänglich.
[Offizielle Beschreibung](https://developers.openai.com/plugins/deploy/submission#update-to-your-mcp-server).

Bei einer **Developer-Mode-Verbindung** nach dem Rollout die Verbindung öffnen,
**Refresh** ausführen, die aktualisierten Metadaten prüfen und einen neuen Chat
starten. Der Chat-Befehl „Cores Tools neu laden“ stellt allein keine
Metadatenaktualisierung sicher.
[Offizielle Anleitung](https://developers.openai.com/plugins/deploy/connect-chatgpt#refresh-metadata).

Bei **Responses-API-Clients** bleibt `mcp_list_tools` im Kontext zwischengespeichert;
der Client muss eine neue Discovery ohne die alte Liste auslösen.
[Offizielle API-Dokumentation](https://developers.openai.com/api/docs/guides/tools-connectors-mcp).
Am 02.10.2026 zeigte die Prüfung vor diesem Job-Release 341 Server-Tools und
158 in der laufenden KI-App bereitgestellte Tools. Aktuelle Entitätsschemas
kamen bereits aus dem neuen Backend; die clientseitige Liste enthielt die neuen
Revert-Werkzeuge noch nicht. Backend-Rollout und Client-Katalog separat prüfen.

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
Browsernachweise: [Dark/Mobil](docs/screenshots/maintenance-financial-consent-dark-mobile.png)
und [Light/Desktop](docs/screenshots/maintenance-financial-consent-light-desktop.png).

## Atomare MCP-Wartungspläne — WarehouseCore 5.9.100 / Cores MCP 1.5.30

`warehouse.maintenance_plans` bietet `search`, `prepare_create`/`create`,
`prepare_update`/`update`, `prepare_archive`/`archive`, `prepare_restore`/`restore`
und redigierte `audit_history`. Alle fachlichen Planfelder sind im Schema
`warehouse.maintenance_plans` beschrieben. Updates ergänzen nur angegebene
Felder; `clear_fields=["instructions"]` leert Arbeitsanweisungen ausdrücklich.
Gerätezuordnung und historische Abschlüsse bleiben erhalten.

Die Vorschau zeigt Plan und Gerät, Diff, Duplikate, offene/historische Aufträge,
den neuen Gerätetermin und den Entwurf eines gegebenenfalls fälligen Auftrags.
Admin und create/update/archive-Scope, `confirm_change`, `idempotency_key`,
exakte `expected_device_updated_at` und bei bestehenden Plänen zusätzlich
`expected_updated_at` sind erforderlich. Lifecycle verlangt die Phrase
`ARCHIVE|RESTORE WAREHOUSE MAINTENANCE PLAN <ID>` und keine offenen Aufträge.
Restore benötigt ein aktives, nicht ausgemustertes Gerät und aktives Produkt.
Vorschau und `dry_run` schreiben nichts.

Plan, synchronisierter nächster Gerätetermin, automatisch fälliger geplanter
Auftrag samt Ereignis, Vorher/Nachher-Audits und dauerhafter Replay werden gemeinsam
gespeichert oder zurückgerollt. Zustand und Lagerort des Geräts werden erhalten.
Migration Warehouse `054` / Umbrella `027` versioniert alle Plan-/Auftragsschreiber;
Auftragsänderungen machen auch die Planvorschau ungültig. Neue Umbrella-Datenbanken
erhalten das kanonische Wartungsschema. Neustarts erzeugen bei vorhandenen
benutzerdefinierten Plänen keine zusätzlichen Standardpläne.

215 Tools: 73 Abfragen, 71 Vorschauen, 71 Ausführungen. Keine neue Konfiguration.
Manuelle Auftrags-/Defektprozesse und übrige Anforderungen von #4/#5 sind weiter
im [Abschlusscheck](https://github.com/nbt4/cores-mcp/blob/main/docs/ISSUE_COMPLETION.md) offen.

## MCP-Hersteller und Marken — WarehouseCore 5.9.99 / Cores MCP 1.5.29

Hersteller und Marken unterstützen `prepare_archive`/`archive`,
`prepare_restore`/`restore` und redigierte `audit_history`. Warehouse-Admin,
`cores:warehouse:archive` (oder Legacy `cores:write`), vollständige Vorschau,
exakte Version, `confirm_lifecycle`, Idempotenz und
`ARCHIVE|RESTORE WAREHOUSE MANUFACTURER|BRAND <ID>` sind erforderlich.

Aktive Produkte sperren beide Stammdatenarchive; aktive Marken sperren das
Herstellerarchiv zusätzlich. Historische Produktbeziehungen, Felder und IDs
bleiben erhalten. Restore prüft Identität und aktiven Hersteller der Marke.
Migration Warehouse `053` / Umbrella `026` schützt diese Regeln auch bei
bestehenden Schreibpfaden. Archivierte Datensätze sind vor Bearbeitung zu
restaurieren. Aktive Produkt-/Markenzuordnungen dürfen nicht auf archivierte
Stammdaten zeigen. Normale Auswahllisten zeigen aktive Hersteller und Marken;
MCP-Auflösung zeigt archivierte Identitäten mit Status für Duplikatprüfung.
Mutation, vollständiger Vorher/Nachher-Audit und dauerhafter Replay sind atomar.
Die Historientools liefern keine Roh-JSON, Website, IP oder User-Agent.

205 Tools: 71 Abfragen, 67 Vorschauen, 67 Ausführungen. Keine neue Konfiguration.
Der übrige Umfang von #4/#5 bleibt im [Abschlusscheck](https://github.com/nbt4/cores-mcp/blob/main/docs/ISSUE_COMPLETION.md) dokumentiert.

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
Bestätigung und Storno benötigen ausdrücklich `cores:procurement:approve`
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
docker build -t nobentie/cores-mcp:1.5.30 -t nobentie/cores-mcp:latest .
```

Die Umbrella-Compose-Datei der Cores Suite bindet den Dienst intern ein. Der Cores-Dashboard-Reverse-Proxy veröffentlicht MCP und OAuth auf derselben Domain, damit der bestehende Suite-Login genutzt werden kann.
