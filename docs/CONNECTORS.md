# Connector-Einrichtung

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

## Voraussetzungen

- Cores MCP ist über HTTPS öffentlich erreichbar.
- `MCP_PUBLIC_URL` entspricht exakt der externen Basis-URL.
- `/mcp`, `/oauth/*` und `/.well-known/*` werden ohne vorgeschaltete Dashboard-Authentifizierung zum MCP-Dienst weitergeleitet.
- Der Benutzer ist im Cores Dashboard angemeldet und besitzt einen aktiven Account.

Beispielendpunkt:

```text
https://cores.example.com/mcp
```

## ChatGPT

In den ChatGPT-Einstellungen eine benutzerdefinierte App bzw. ein MCP-Plugin hinzufügen und die MCP-URL eintragen. Nach „Verbinden“ öffnet sich der Cores-OAuth-Dialog. Dort ist **Nur Lesen** vorausgewählt. Für Schreibtools **Lesen und Schreiben** auswählen und **Ausgewählten Zugriff erlauben** bestätigen. Diese Auswahl erscheint auch, wenn der Client zunächst nur `cores:read` anfordert. `cores:write` bleibt als kompatibler Sammel-Scope verfügbar; Clients können stattdessen gezielt `cores:<service>:create`, `cores:<service>:update`, `cores:procurement:approve` oder `cores:procurement:receive` anfordern. Ein Token nur mit `cores:read` bleibt vollständig read-only. Bereits ausgestellte lesende Tokens behalten ihre Rechte auch beim Refresh. Die Verbindung einmal trennen und neu verbinden, um im Cores-Dialog Schreibrechte ausdrücklich freizugeben.

Offizielle Referenz: <https://developers.openai.com/plugins/>

Je nach ChatGPT-Plan, Workspace-Richtlinie und Rollout kann die Bezeichnung in der Oberfläche variieren. Der Server benötigt keine OpenAI-API-Keys; ChatGPT verbindet sich direkt per MCP.

## Claude

In Claude unter den Integrationen/Connectors einen benutzerdefinierten Connector anlegen, die MCP-URL eintragen und verbinden. Claude nutzt die OAuth-Metadaten und Dynamic Client Registration automatisch. Falls noch keine Cores-Sitzung besteht, erscheint zuerst das zentrale lokale/Microsoft-Login und danach automatisch wieder der Cores-Consent. Dort den angezeigten Lese- bzw. Lese- und Anlagezugriff erlauben; die Bestätigung wird als normaler Formular-POST verarbeitet und leitet zurück zu Claude.

Offizielle Referenz: <https://support.anthropic.com/en/articles/11175166-getting-started-with-custom-connectors-using-remote-mcp>

## Codex und andere MCP-Clients

Jeder Client mit Streamable-HTTP- und OAuth-Unterstützung kann dieselbe URL verwenden. Für nicht-interaktive Agents ist ein statisches Bearer-Token möglich:

```http
Authorization: Bearer <secret>
```

Serverkonfiguration:

```dotenv
MCP_AUTH_MODE=oauth
MCP_STATIC_TOKENS=deployment-agent:a-long-random-secret
```

Der OAuth-Modus akzeptiert dann sowohl interaktive OAuth-Tokens als auch die konfigurierten Agent-Tokens. Im reinen Bearer-Modus muss `MCP_AUTH_MODE=bearer` gesetzt sein.
Statische Tokens bleiben absichtlich auf `cores:read` begrenzt. Anlage-Tools benötigen immer einen interaktiv angemeldeten Cores-Benutzer.

## Verbindung prüfen

```bash
curl -fsS https://cores.example.com/health
curl -fsS https://cores.example.com/.well-known/oauth-protected-resource/mcp
curl -fsS https://cores.example.com/.well-known/oauth-authorization-server
```

Mit einem Agent-Token:

```bash
go run ./cmd/smoke \
  -endpoint https://cores.example.com/mcp \
  -token "$MCP_TOKEN"
```

## Trennen

Den Connector beim KI-Anbieter entfernen. Statische Tokens zusätzlich serverseitig aus `MCP_STATIC_TOKENS` entfernen und den Dienst neu starten. OAuth-Zugriffstokens laufen nach einer Stunde ab; Refresh-Tokens nach 30 Tagen.

## Häufige Fehler

- `401 Unauthorized`: Cores-Login fehlt, Token ist abgelaufen oder der Scope `cores:read` fehlt.
- `mutation scope is required`: Die Verbindung besitzt nur Leserechte oder nicht den für Service und Aktion passenden Schreibscope. `MCP_ENABLE_WRITES=true` setzen und im Cores-Autorisierungsdialog **Lesen und Schreiben** auswählen und bestätigen; falls der Client ihn nicht automatisch öffnet, den Connector einmal trennen und neu verbinden.
- `only response_type=code is supported` nach „Nach der Anmeldung erneut versuchen“: Der Autorisierungslink ist veraltet oder wurde von einem Proxy doppelt URL-encodiert. Die Verbindung beim KI-Anbieter entfernen, neu anlegen und darauf achten, dass die Browser-URL echte Parameter wie `?client_id=...&response_type=code` enthält, nicht `client_id%3d...%26response_type=code`.
- OAuth-Metadaten nicht gefunden: `/.well-known/*` wird vom Reverse Proxy nicht weitergeleitet.
- Redirect-Fehler: Client-Callback wurde nicht bei Dynamic Client Registration registriert oder nutzt unsicheres HTTP außerhalb von localhost.
- Origin abgelehnt: Browser-Origin stimmt nicht mit `MCP_PUBLIC_URL`/`MCP_ALLOWED_ORIGINS` überein.
- Leere Ergebnisse: Das Tool hat keine passenden Cores-Datensätze gefunden; mit `cores.data.quality` auf Erfassungslücken prüfen.
- Ungewöhnliche Kombinationen: Zuerst `cores.query.catalog` aufrufen, dann die benötigten Entitäten in einem `cores.query.records`-Call abfragen und über eine dokumentierte Beziehung verbinden. Für Kennzahlen `cores.query.aggregate` verwenden.
