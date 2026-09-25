# Sicherheit und Datenschutz

## Vertrauensgrenzen

Der MCP-Server liest operative Daten aus der gemeinsamen Cores-PostgreSQL-Datenbank und gibt ausgewählte Felder an den verbundenen MCP-Client zurück. Der verwendete KI-Anbieter verarbeitet diese Antworten nach seinen eigenen Workspace-, Datenschutz- und Aufbewahrungsregeln. Vor der Freigabe muss daher entschieden werden, welche Anbieter und Accounts Cores-Daten erhalten dürfen.

## Zugriffsschutz

- OAuth 2.0 Authorization Code mit PKCE S256, Dynamic Client Registration und
  Mindest-Scope `cores:read`. Schreibtools verlangen zusätzlich entweder den
  kompatiblen Sammel-Scope `cores:write` oder einen granularen Scope je Service
  und Aktionsklasse (`cores:<service>:create|update|approve|receive`).
- Ein bewusst read-only ausgestelltes Token bleibt auch bei serverseitig
  aktivierten Schreibtools nutzbar. Neu benötigte Schreibscopes erfordern einen
  sichtbaren OAuth-Consent.
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

## Lesezugriff und eng begrenzte Schreibfunktionen

1. Die Abfrageseite bietet ausschließlich fest definierte fachliche Tools und eine deklarative Query-API über kuratierte Entitäten, Felder, Operatoren und Beziehungen an. Freies SQL ist nicht möglich; sämtliche Werte werden parametrisiert.
2. Der Store öffnet jede Transaktion mit PostgreSQL `READ ONLY` und setzt ein Statement-Timeout.
3. Der Produktionsnutzer `cores_mcp` erhält ausschließlich `SELECT` auf das öffentliche Schema und `default_transaction_read_only=on`.

Optionale Schreibtools schreiben niemals über die Datenbankrolle. Sie rufen ausschließlich fest verdrahtete fachliche Endpunkte des verantwortlichen Core mit einem zweiminütigen, aus dem interaktiven OAuth-Benutzer abgeleiteten Suite-Token auf. Dazu zählen additive Anlagen sowie die einzeln definierten P0/P1-Operationen Gerätezuweisung, Job-/Requirement-Änderung, Bestellung, Lagerbewegung und Gerätezustand. Rollen- und Mitgliedschaftsregeln des Zielservices bleiben wirksam; Warehouse-Produkte samt ausdrücklich freigegebenen neuen Stammdaten benötigen Warehouse-Administratorrechte und werden in einer Zielservice-Transaktion angelegt, Bestellungen benötigen Procurement-Administratorrechte und Planner-Tasks werden bereits in der Vorschau auf die persönliche Planmitgliedschaft begrenzt. Statische Maschinentokens können keine Schreibtools nutzen.

Vor jeder Operation liefert ein eigenes Vorbereitungstool Pflichtlücken, Referenzkandidaten, aktuelle Werte, Risiken und mögliche Duplikate. Das Ausführungstool schreibt erst nach finaler Vorschau, dem operationsspezifischen `confirm_*` und einem gültigen `idempotency_key`; unvollständige empfohlene Produktdaten benötigen zusätzlich `accept_incomplete=true`. `dry_run=true` entfernt die Bestätigung serverseitig und führt nur die Validierung/Vorschau aus. Gleiche bestätigte Aufrufe werden je MCP-Prozess 24 Stunden dedupliziert, gleichzeitige Wiederholungen zusammengeführt und abweichende Payloads unter demselben Schlüssel blockiert. Der Header wird an den Ziel-Core weitergereicht; ProcurementCore speichert ihn für Produkte, Angebote, Lieferanten, Kategorien, Bedarfsentscheidungen und Wareneingänge atomar mit dem Ergebnis. Andere Workflows benötigen für Deduplizierung über MCP-Neustarts und Replikate weiterhin persistente Verarbeitung im jeweiligen Ziel-Core. Es gibt absichtlich kein universelles SQL-, HTTP-, Datei- oder Shell-Werkzeug und keine Tools für Hard-Deletes, Benutzerverwaltung oder beliebige Mutation.

Procurement-Freigaben und Wareneingänge besitzen eigene Scopes. Freigaben
erzwingen das Vier-Augen-Prinzip. Beide Aktionen prüfen die unveränderte
Datensatzversion und verlangen zusätzlich eine ID-gebundene Bestätigungsphrase.
Überlieferungen werden in der Vorschau quantifiziert und benötigen eine
gesonderte `OVERDELIVERY`-Phrase. Die Ziel-Core-Transaktion umfasst
Idempotenzdatensatz, Status-/Mengenänderung, Geräte- beziehungsweise
Bestandsanlage und Putaway-Task.

Schreibaufrufe werden im MCP-Prozess mit Herkunft `MCP/AI`, Benutzer,
Toolnamen, Dry-Run-/Bestätigungsstatus, Ergebnisstatus und einem nicht
umkehrbaren Kurz-Hash des Idempotenzschlüssels protokolliert; fachliche
Payloads oder Schlüssel gelangen nicht ins zentrale Log. Die Ziel-Cores führen
zusätzlich ihre fachlichen Audit-Trails: RentalCore Job-Historie,
WarehouseCore Gerätehistorie und Bewegungen sowie ProcurementCore Aktivitäten.
Ein universelles kaskadierendes Undo existiert bewusst nicht. Berechtigte
Menschen nehmen Änderungen über eine erneut validierte Gegenoperation zurück,
beispielsweise durch Rücksetzen von Job/Requirement/Gerätezustand oder eine
inverse Lagerbewegung.

## Datenminimierung

Tools schließen private Kontaktfelder, Passwörter, Tokens und unnötige Notizen aus. Fachliche Freitexte werden nur dort geliefert, wo sie für die Frage relevant sind. Resultate sind serverseitig begrenzt. Knowledge-Dateien werden nur aus explizit konfigurierten Verzeichnissen gelesen; Symlinks und Dateien über 2 MiB werden ignoriert.

## Prompt Injection

Job-, Produkt-, Shopseiten-, Aufgaben- und Notiztexte sind nutzergenerierte, nicht vertrauenswürdige Daten. Toolbeschreibungen und Warnungen weisen Clients darauf hin, diese Inhalte nicht als Anweisungen zu befolgen. Produktseiten werden ausschließlich über den SSRF-geschützten ProcurementCore-Importer gelesen. Extrahierte Werte werden als Entwurf behandelt; Rückfragen, Duplikatprüfung und explizite Bestätigung begrenzen das Risiko vor einer Anlage.

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
