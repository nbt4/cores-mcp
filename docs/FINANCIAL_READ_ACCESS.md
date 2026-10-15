# Explizite Finanzrechte für lesende MCP-Werkzeuge

Cores MCP 1.5.64 setzt den Finanz-Leseanteil aus Issues #4 und #5 um.
Der Werkzeugkatalog bleibt bei 446 Werkzeugen; Namen und Eingabeschemas
bleiben stabil. Es gibt keine neue Datenbankmigration oder Abhängigkeit.

| Finanzscope | Geschützte Geschäftsdaten |
| --- | --- |
| `cores:warehouse:financial` | Warehouse-Produkt-/Paketpreise, Geräteumsätze und Wartungskosten |
| `cores:rental:financial` | Jobpreise/-umsätze/-rabatte, Auftragspositionen und Fremdmietpreise/-kosten |
| `cores:procurement:financial` | Angebotspreise, Preisverlauf, Bedarfsschätzungen und Bestellwerte |

`cores:read`, Legacy-`cores:write` und ein Finanzrecht eines anderen Dienstes
erteilen diese Rechte nicht. OAuth zeigt nur die angefragten Finanzbereiche;
der unabhängig beschriftete Finanz-Select bleibt standardmäßig gesperrt.
Finanzfreigabe erteilt keine Schreibrechte. Read-only-Konfigurationen dürfen
explizit angefragte Finanz-Leserechte freigeben; Maschinen-Lesetokens erhalten
keine persönlichen Finanzrechte. Token-Refresh erweitert keine Freigaben.

## Operative und finanzielle Lesezugriffe

Gemischte Werkzeuge behalten Identitäten, Mengen, Termine und Statusfelder,
lassen aber nicht freigegebene Preise/Kosten/Umsätze aus. Das gilt auch für
verschachtelte Datensätze und sämtliche Procurement-Audit-Aliase. Die Antwort
erklärt ausgelassene Finanzfelder; gespeicherte Datensätze werden nicht geändert.
Fremdmiet-Katalogpreise gehören zu Rental, auch wenn Warehouse-Rechte vorliegen.

Rein finanzielle Werkzeuge (`rental.revenue.summary`, `procurement.offers.compare`,
`procurement.prices.history`, `procurement.spend.summary`) benötigen das
jeweilige Finanzrecht vor jeder Abfrage. Die bisherigen vollständigen
`rental.jobs.get`-/Positions- und Wartungskosten-Abfragen behalten ihre
bestehenden Finanzsperren. Erforderliche Scopes stehen im Tool-Descriptor und
in dessen `_meta`-Spiegel. Fehlende Freigaben liefern einen standardisierten
OAuth-Challenge für die gezielte Nachfreigabe.

Bei `cores.query.records/aggregate` schützt die Prüfung ausdrücklich angeforderte
Finanzfelder ebenso bei Projektion, Filtern, Sortierung, Join-Schlüsseln,
Gruppierung und Kennzahlen. Standardprojektionen lassen nicht freigegebene
Finanzfelder aus; `cores.query.catalog` nennt pro Feld den erforderlichen Scope.
Aliasse ändern die Prüfung der tatsächlichen Quellfelder nicht.

Auch die Auswahl eines günstigsten Lieferanten kann Preise verraten. Ohne
Procurement-Finanzrecht liefern entsprechende Empfehlungen keine preisgerankte
Lieferantenidentität; Produktangebote werden stattdessen nach Lieferzeit/ID
sortiert, und Preisverläufe werden nicht geladen. Der explizite Finanzgrant
wird pro lesender SQL-Transaktion gesetzt und kann nicht vom vorigen Nutzer
einer gepoolten Verbindung übernommen werden. Bestehende Bedarfs-/Planner-
Mitglieds- und aktuelle Benutzerrechte gelten zusätzlich weiter.

## Client-Aktualisierung und Grenzen

Nach dem MCP-Rollout Werkzeug-Metadaten neu laden. Falls Finanzwerte benötigt
werden, den jeweiligen Scope anfragen und in OAuth ausdrücklich freigeben.
Vorhandene Tokens werden nicht stillschweigend erweitert.

Dieses Release behandelt lesende Werkzeuge. Es verändert keine fachlichen
Schreib-/Vorschau-/Bestätigungsverträge und keine gespeicherten Erfolgsbelege.
Die separaten Finanzprüfungen aller übrigen Schreibworkflows, Dokumentabläufe,
Rental-/Planner-Lifecycle und verbleibende Undo-Pfade bleiben in #4/#5 offen.
Die übergreifenden Issues werden durch dieses Teilrelease nicht geschlossen.

## Verifikation

Lokale PostgreSQL-16-Prüfungen nutzen ausschließlich synthetische Daten in
einer eigenen `_test`-Datenbank. HTTP-MCP-Tests wechseln nacheinander zwischen
Leser, Legacy-Writer und den drei Finanzgrants auf derselben gepoolten Verbindung.
Sie prüfen Query-Inferenz, Aliasse, alle sechs Audit-Historien, Preisrangfolge,
Fremdmietpreise, negative Rechtekombinationen und vollständige erlaubte Werte.
OAuth-Tests prüfen ausdrückliche Zustimmung/Ablehnung, readonly und Refresh.
Browserprüfungen verwenden den tatsächlichen Go-Renderer und die tatsächliche
CSP mit kanonischem Suite-CSS; DE/EN, Light/Dark, alle Suite-Breakpoints,
Touch-Ziele, Tastaturfokus, Selectbedienung, AA-Kontrast und Formvarianten.

[OAuth-Designvertrag](OAUTH_CONSENT.md),
[Prüfprotokoll](FINANCIAL_READ_ACCESS_TESTS.md).

Die Browserprüfung deckte zusätzlich ein bestehendes unsichtbares Dark-Logo auf:
CSP blockierte die Inline-SVG-Füllstile. Dieselben unveränderten Logopfade nutzen
jetzt `fill="currentColor"` als SVG-Präsentationsattribut. Die CSP bleibt streng;
ein Regressionstest und echte Logo-Kontrastmessung sichern beide Themes ab.
