# CR-002 — Correcao dos P1 Findings

**Data:** 2026-04-11
**Base:** CR-001 (commit df812e7)
**Motivacao:** 15 findings P1 identificados na avaliacao de maturidade v1. P1-08 ja resolvido em CR-001. Este CR resolve os 13 restantes.

---

## P1-01 — Pause funcional no popup

**Arquivos:** `extension/src/background/service-worker.ts`, `extension/src/popup/App.tsx`

**Problema:** Botao Pause alterava apenas estado local React (`setPaused`) — nao chegava ao service worker. Captura continuava normalmente.

**Fix:**
- `service-worker.ts`: variavel de modulo `let captureEnabled = true`; `handlePageViewed` retorna imediatamente se `!captureEnabled`; handler `popup_set_capture` alterna o estado e responde com o novo valor
- `App.tsx`: `handleToggleCapture()` envia `{ type: 'popup_set_capture', enabled: !current }` ao SW; estado visual derivado de `status.captureEnabled` (sincronizado com SW)

---

## P1-02 — Porta hardcoded 7700 no popup

**Arquivo:** `extension/src/popup/App.tsx`, `extension/src/background/service-worker.ts`

**Problema:** `App.tsx:181` usava `const port = 7700` para construir a URL da UI — quebrado em toda instalacao nativa (porta aleatoria).

**Fix:**
- `service-worker.ts`: `popup_status` response inclui `daemonPort: daemonState.port`
- `App.tsx`: botao "Open full UI" so aparece quando `status?.daemonPort` e conhecido; usa `status.daemonPort` na URL

---

## P1-03 — Forget nao limpava tabela queue

**Arquivo:** `daemon/internal/store/sqlite.go`

**Problema:** `Forget()` deletava apenas de `pages` (com CASCADE para `chunks`). A tabela `queue` acumulava URLs mesmo apos o usuario solicitar esquecimento.

**Fix:** `Forget()` agora executa dentro de uma transacao e deleta da `queue` tambem, para cada modalidade (`url`, `domain`, `timerange`).

---

## P1-04 — /healthz retornava 200 com banco inacessivel

**Arquivos:** `daemon/internal/store/sqlite.go`, `daemon/internal/server/routes.go`

**Problema:** `/healthz` retornava `{"ok":true}` sempre, mesmo com SQLite corrompido ou inacessivel — falso-positivo para health checks.

**Fix:**
- `sqlite.go`: novo metodo `Ping() error` — executa `SELECT 1` no banco
- `routes.go`: `/healthz` chama `s.Ping()`; se falhar, retorna HTTP 503 com `{"ok":false,"error":"database unavailable"}`

---

## P1-05 — Schema sem versionamento

**Arquivo:** `daemon/internal/store/sqlite.go`

**Problema:** `migrate()` executava apenas `CREATE TABLE IF NOT EXISTS` — sem rastreamento de versao. Mudancas de schema em v0.2+ requereriam intervencao manual ou wipe do banco.

**Fix:**
- Constante `schema` renomeada para `schemaV1`
- Nova tabela `schema_versions (version INTEGER PK, applied_at INTEGER)`
- `migrate()` reescrito: le versao atual, aplica migrations pendentes em ordem, registra cada versao aplicada
- Migrations futuras adicionam apenas uma entrada ao slice `migrations`

---

## P1-06 — N+1 queries no build de resultados de busca

**Arquivo:** `daemon/internal/store/sqlite.go`

**Problema:** Para cada resultado (ate 20), `Search()` executava 2 queries separadas — ate 40 queries sequenciais por busca.

**Fix:** Substituido por um unico JOIN query com `WHERE c.id IN (?, ?, ...)`. Resultados coletados em map e reordenados por score RRF para preservar a ordem de relevancia.

---

## P1-07 — CORS bloqueava consumidores externos

**Arquivos:** `daemon/internal/server/routes.go`, `daemon/internal/server/server.go`

**Problema:** `corsMiddleware` permitia apenas `chrome-extension://` — dashboards externos (Next.js, etc.) eram bloqueados pelo browser.

**Fix:**
- `VBM_CORS_ORIGIN`: env var com origens adicionais permitidas (comma-separated; ex: `http://localhost:3000,http://localhost:4000`)
- `server.go` le a env var e passa `extraOrigins []string` para `newRouter()`
- `corsMiddleware` aceita a lista; verifica `chrome-extension://` OU origin na lista

---

## P1-08 — (Resolvido em CR-001)

Tipos duplicados em `daemon-client.ts` — removidos no CR-001.

---

## P1-09 — Bancos brasileiros ausentes na denylist

**Arquivo:** `extension/src/lib/denylist.ts`

**Problema:** Denylist cobria apenas bancos americanos e dominios `.gov`/`.mil` (EUA). Bancos brasileiros e TLDs `.gov.br`/`.mil.br` ausentes.

**Fix:**
- Adicionados 14 dominios: `itau.com.br`, `bradesco.com.br`, `bradesconetempresa.b.br`, `nubank.com.br`, `santander.com.br`, `bb.com.br`, `caixa.gov.br`, `bancobrasil.com.br`, `inter.co`, `bancointer.com.br`, `sicoob.com.br`, `sicredi.com.br`, `safra.com.br`, `btgpactual.com`
- Adicionadas verificacoes de TLD: `.gov.br` e `.mil.br` em `isDeniedDomain()`

---

## P1-10 — WebSocket sem ping/pong (conexoes zumbi)

**Arquivo:** `daemon/internal/server/routes.go`

**Problema:** Handler do `/ws` nunca lia mensagens nem enviava pings — conexoes de clientes desconectados ficavam abertas indefinidamente acumulando goroutines.

**Fix:**
- `SetReadDeadline(now + 60s)` no inicio da conexao
- `SetPongHandler` reseta o read deadline a cada pong recebido
- Goroutine de leitura dedicada (obrigatoria pelo gorilla/websocket para processar control frames)
- `pingTicker` a cada 30s envia `PingMessage` com write deadline de 10s
- `SetWriteDeadline` antes de cada `WriteJSON` e `WriteMessage`
- Loop principal monitora `readDone` channel para encerrar quando cliente desconecta

---

## P1-11 — Systemd unit sem hardening

**Arquivo:** `daemon/install/vbmd.service`

**Problema:** Unit file tinha `PrivateNetwork=false` (redundante) e `After=network.target` (desnecessario — daemon nao usa rede externa no startup).

**Fix:**
- Removido `After=network.target` e `PrivateNetwork=false`
- Adicionado: `NoNewPrivileges=true`, `ProtectSystem=strict`, `ProtectHome=read-only`, `ReadWritePaths=%h/.local/share/vbm`
- Nota: `PrivateNetwork=true` NAO e usado pois criaria namespace de rede isolado, impedindo Chrome de conectar ao daemon via loopback

---

## P1-12 — `os.UserHomeDir()` com erro descartado

**Arquivo:** `daemon/internal/nm/host.go`

**Problema:** `SessionPath()` descartava o erro de `os.UserHomeDir()` com `_` — em ambientes sem `$HOME`, retornava caminho invalido silenciosamente.

**Fix:** `SessionPath()` agora retorna `(string, error)`; todos os callers tratam o erro (`readSession()`, `WriteSession()`, `server.go`).

---

## P1-13 — Logs nao estruturados

Postergado para CR-003 (refatoracao completa para `slog` com campos estruturados). Os logs mais criticos (startup, shutdown, errors) ja incluem contexto suficiente.

---

## P1-14 — install.sh aceitava Extension ID invalido

**Arquivo:** `daemon/install/install.sh`

**Problema:** `REPLACE_ME` era aceito silenciosamente se o usuario deixasse o campo em branco — o NM manifest ficava acessivel a qualquer extensao.

**Fix:** Apos a leitura, valida que o Extension ID consiste em exatamente 32 letras `a-p` (formato Chrome). Se invalido, exibe WARNING com instrucoes de correcao mas continua a instalacao (para nao bloquear usuarios com IDs temporarios).

---

## P1-15 — Sem endpoint /export (LGPD Art. 18)

**Arquivos:** `daemon/internal/store/sqlite.go`, `daemon/internal/server/routes.go`

**Problema:** Sem mecanismo para o titular obter seus dados em formato portatil.

**Fix:**
- `sqlite.go`: tipos `ExportPage` e `ExportChunk`; metodo `Export()` retorna todas as paginas com chunks via LEFT JOIN ordenado por `visit_ts DESC`
- `routes.go`: novo endpoint `GET /export` (autenticado) retorna `{pages: ExportPage[], total: int}`

---

## Arquivos modificados

| Arquivo | P1s |
|---|---|
| `extension/src/background/service-worker.ts` | P1-01, P1-02 |
| `extension/src/popup/App.tsx` | P1-01, P1-02 |
| `extension/src/lib/denylist.ts` | P1-09 |
| `daemon/internal/store/sqlite.go` | P1-03, P1-04, P1-05, P1-06, P1-15 |
| `daemon/internal/server/routes.go` | P1-04, P1-07, P1-10, P1-15 |
| `daemon/internal/server/server.go` | P1-07, P1-12 (SessionPath caller) |
| `daemon/internal/nm/host.go` | P1-12 |
| `daemon/install/vbmd.service` | P1-11 |
| `daemon/install/install.sh` | P1-14 |

---

## Verificacao

```bash
# Build
cd daemon && go build ./...
cd ../extension && npm run typecheck

# /healthz com banco ok
curl http://127.0.0.1:$PORT/healthz
# {"ok":true}

# /export
curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:$PORT/export | jq '.total'

# CORS externo
VBM_CORS_ORIGIN=http://localhost:3000 ./bin/vbmd server
# requests de localhost:3000 recebem Access-Control-Allow-Origin: http://localhost:3000

# Pause via popup: clicar Pause -> paginas nao sao mais indexadas
# Resume: captura volta ao normal

# Denylist BR
# itau.com.br deve retornar true em isDeniedDomain()

# /ws ping/pong
# wscat -c ws://127.0.0.1:$PORT/ws -H "Authorization: Bearer $TOKEN"
# deve receber status a cada 5s e ping a cada 30s
```
