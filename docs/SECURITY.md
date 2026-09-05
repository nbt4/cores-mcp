# Sicherheit und Datenschutz

## Vertrauensgrenzen

Der MCP-Server liest operative Daten aus der gemeinsamen Cores-PostgreSQL-Datenbank und gibt ausgewählte Felder an den verbundenen MCP-Client zurück. Der verwendete KI-Anbieter verarbeitet diese Antworten nach seinen eigenen Workspace-, Datenschutz- und Aufbewahrungsregeln. Vor der Freigabe muss daher entschieden werden, welche Anbieter und Accounts Cores-Daten erhalten dürfen.

## Zugriffsschutz

- OAuth 2.0 Authorization Code mit PKCE S256, Dynamic Client Registration und Scope `cores:read`.
- Autorisierung nur mit gültigem Cores-Suite-Cookie und aktivem Benutzer in der Datenbank.
- Exakte Redirect-URI-Prüfung, HTTPS-Pflicht außer localhost, State/CSRF-Schutz und kurzlebige einmalige Codes.
- Signierte Access-Tokens: 1 Stunde; Refresh-Tokens: 30 Tage.
- Aktiver Kontostatus wird bei jedem OAuth-API-Zugriff sowie beim Code- und
  Refresh-Austausch erneut geprüft; Datenbankfehler verweigern Zugriff.
- Planner-Daten erfordern eine aktuelle Mitgliedschaft, auch für Administratoren.
  Der Store setzt die Nutzeridentität ausschließlich transaktionslokal; sie kann
  nicht zwischen gepoolten Verbindungen oder Nutzern weitergegeben werden.
  Statische Maschinentokens und anonyme Entwicklungszugriffe sehen keine Planner-Daten.
- Optional benannte, starke Bearer-Tokens für Maschinenzugriff.
- Origin-Prüfung, Security-Header, Request-Limits, Rate-Limit und strukturierte Audit-Logs.

## Read-only in drei Schichten

1. MCP bietet ausschließlich fest definierte fachliche Abfragen und eine deklarative Query-API über kuratierte Entitäten, Felder, Operatoren und Beziehungen an. Freies SQL ist nicht möglich; sämtliche Werte werden parametrisiert.
2. Der Store öffnet jede Transaktion mit PostgreSQL `READ ONLY` und setzt ein Statement-Timeout.
3. Der Produktionsnutzer `cores_mcp` erhält ausschließlich `SELECT` auf das öffentliche Schema und `default_transaction_read_only=on`.

Es gibt absichtlich kein universelles SQL-, HTTP-, Datei- oder Shell-Werkzeug. Schreibzugriff, Bestellungen, Freigaben, Statusänderungen und Benutzerverwaltung sind nicht Teil dieses Dienstes.

## Datenminimierung

Tools schließen private Kontaktfelder, Passwörter, Tokens und unnötige Notizen aus. Fachliche Freitexte werden nur dort geliefert, wo sie für die Frage relevant sind. Resultate sind serverseitig begrenzt. Knowledge-Dateien werden nur aus explizit konfigurierten Verzeichnissen gelesen; Symlinks und Dateien über 2 MiB werden ignoriert.

## Prompt Injection

Job-, Produkt-, Aufgaben- und Notiztexte sind nutzergenerierte, nicht vertrauenswürdige Daten. Toolbeschreibungen und Warnungen weisen Clients darauf hin, diese Inhalte nicht als Anweisungen zu befolgen. Da der Server keine schreibenden oder offenen Tools besitzt, bleibt der mögliche Schaden zusätzlich begrenzt.

## Fachliche Grenzen

Bestandsaussagen hängen von vollständig erfassten Anforderungen, Paketauflösungen, Gerätezuständen und Lagerzahlen ab. Sicherheitskritische Aussagen zu Rigging, Lasten, Elektro- oder Herstellervorgaben sind niemals eine Freigabe. Das Ergebnis muss von qualifizierten Personen und anhand der Herstellerdokumentation geprüft werden.

## Betrieb

- Dienst nur über HTTPS veröffentlichen; Port 8090 ausschließlich im internen Netz.
- Dedizierten Read-only-DB-Login verwenden und regelmäßig Berechtigungen prüfen.
- `CORES_JWT_SECRET`, DB-Passwort und statische Tokens nur über Secret Management/.env zuführen.
- OAuth-Datenvolume sichern; Logs zentral sammeln und auf ungewöhnliche Abrufmuster prüfen.
- Nach Entzug eines Maschinenzugriffs Token rotieren. Nach Entzug eines Benutzers
  `users.is_active=false` setzen; auch bereits ausgestellte OAuth-Access-Tokens
  werden bei der nächsten Anfrage verweigert.
- Knowledge-Ordner nur lesend mounten und Änderungen reviewen.
