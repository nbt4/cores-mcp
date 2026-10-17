# Rental-Finanzrechte für Schreibabläufe

## Rental-Finanzrechte · RentalCore 5.3.130 / MCP 1.5.71

Preisführende Auftragspositionen, Fremdmietkatalog-/Zuweisungsaktionen und die
Gerätezuweisung verlangen ausdrücklich `cores:rental:financial` zusätzlich zum
bestehenden Aktionsrecht. Das gilt vor Vorbereitung, Dry-run, Ausführung und
Wiederholung. Die Gerätezuweisung behält `cores:rental:update` als Aktionsscope;
ihr optionaler Mietpreis einschließlich des Nullpreis-Defaults gehört zum
kommerziellen Vertrag.

Kommerzielle Jobfelder (`revenue`, `discount`, `discount_type`,
`multiply_by_days`, `prices_include_tax`) verlangen auch bei `0` oder `false`
eine getrennte Finanzfreigabe. OAuth fordert diese bei entsprechender Eingabe
zusätzlich an. Organisatorische Jobänderungen, Archivieren und Restore bleiben
mit ihren Aktionsrechten möglich; ohne Finanzscope enthalten ihre Vorschauen,
Diffs und Erfolgsantworten keine Umsatz-/Rabatt-/Steuerfelder. Änderungen, die
positionsbasierte Summen verändern, bleiben bis zur Finanzfreigabe gesperrt.
Versionen, Kontext und Bestätigungsphrasen bleiben unverändert. Gespeicherte
vollständige Owner-Belege werden nur für die Antwort projiziert; erneut erteilte
Finanzrechte liefern den ursprünglichen Beleg. Materialanforderungen und
Personalplanung enthalten bereits ausschließlich operative Kontextfelder.

Delegierte MCP-Tokens dürfen ausschließlich die benannten Rental-POST-Abläufe
und die bestehende Gerätezuweisung verwenden. Native Job-/History-GETs und
sonstige Browseraktionen sind damit gesperrt, auch bei zusätzlichem Session-
Cookie. Die Gerätezuweisung prüft aktuelle aktive Adminidentität, Nutzerbindung,
Update- und Finanzrecht. Normale UI-Sitzungen bleiben nutzbar.

Keine neuen Tools, Eingabeschemas, Migrationen oder Umgebungsvariablen.
Die Parent-Issues #4/#5 bleiben für die übrigen Abnahmepunkte `in_progress`.
