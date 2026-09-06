# Connector-Einrichtung

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

In den ChatGPT-Einstellungen eine benutzerdefinierte App bzw. ein MCP-Plugin hinzufügen und die MCP-URL eintragen. Nach „Verbinden“ öffnet sich der Cores-OAuth-Dialog. Dort den Lesezugriff und – falls `MCP_ENABLE_WRITES=true` – zusätzlich das geführte Anlegen bestätigen. Eine ältere Verbindung muss getrennt und neu verbunden werden, damit sie `cores:write` erhält.

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
- `cores:write permission is required`: Schreibfunktion ist nicht aktiviert oder die Verbindung besitzt noch einen alten Lesetoken. `MCP_ENABLE_WRITES=true` setzen, Dienst neu starten und den Connector neu verbinden.
- `only response_type=code is supported` nach „Nach der Anmeldung erneut versuchen“: Der Autorisierungslink ist veraltet oder wurde von einem Proxy doppelt URL-encodiert. Die Verbindung beim KI-Anbieter entfernen, neu anlegen und darauf achten, dass die Browser-URL echte Parameter wie `?client_id=...&response_type=code` enthält, nicht `client_id%3d...%26response_type=code`.
- OAuth-Metadaten nicht gefunden: `/.well-known/*` wird vom Reverse Proxy nicht weitergeleitet.
- Redirect-Fehler: Client-Callback wurde nicht bei Dynamic Client Registration registriert oder nutzt unsicheres HTTP außerhalb von localhost.
- Origin abgelehnt: Browser-Origin stimmt nicht mit `MCP_PUBLIC_URL`/`MCP_ALLOWED_ORIGINS` überein.
- Leere Ergebnisse: Das Tool hat keine passenden Cores-Datensätze gefunden; mit `cores.data.quality` auf Erfassungslücken prüfen.
- Ungewöhnliche Kombinationen: Zuerst `cores.query.catalog` aufrufen, dann die benötigten Entitäten in einem `cores.query.records`-Call abfragen und über eine dokumentierte Beziehung verbinden. Für Kennzahlen `cores.query.aggregate` verwenden.
