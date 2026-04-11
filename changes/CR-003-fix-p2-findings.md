# CR-003 — Correcao dos P2 Findings

**Data:** 2026-04-11
**Base:** CR-002 (commit b6268f9)
**Motivacao:** 10 findings P2 identificados na avaliacao de maturidade v1. P2-07 ja resolvido em CR-002. Este CR resolve os 9 restantes.

---

## P2-01 — REPLACE_TOKEN literal na UI embutida

**Arquivo:** `daemon/internal/server/routes.go`

**Problema:** O HTML da `/ui` continha `REPLACE_TOKEN` literal no JavaScript — o formulario de busca nunca funcionava.

**Fix:** `uiWithToken := strings.ReplaceAll(uiHTML, "REPLACE_TOKEN", token)` calculado uma vez em `newRouter()`; handlers `/ui` e `/ui/*` servem `uiWithToken`.

---

## P2-02 — Queue table nunca limpa apos processamento

**Arquivos:** `daemon/internal/store/sqlite.go`, `daemon/internal/queue/queue.go`, `daemon/internal/server/routes.go`

**Problema:** A tabela `queue` existia no schema mas nunca era escrita nem limpa — `GetStatus().pending` sempre retornava 0; tabela acumularia entradas indefinidamente se fosse usada.

**Fix:**
- `sqlite.go`: `AddQueueItem(req IngestRequest) error` — insere com `status='pending'`; `RemoveQueueItem(url string) error` — deleta apos processamento
- `queue.go`: worker chama `store.RemoveQueueItem(req.URL)` apos `store.Ingest()` bem-sucedido
- `routes.go`: handler `/ingest` chama `s.AddQueueItem(ireq)` apos `q.Enqueue()` (erro logado, nao fatal)

---

## P2-03 — db.SetMaxOpenConns(1) ausente

**Arquivo:** `daemon/internal/store/sqlite.go`

**Problema:** SQLite e single-writer mas o pool Go nao tinha restricao explicita — potencial para erros de lock em cenarios de alta concorrencia.

**Fix:** `db.SetMaxOpenConns(1)` apos `sql.Open()`.

---

## P2-04 — Sem debounce no omnibox

**Arquivo:** `extension/src/background/service-worker.ts`

**Problema:** Cada keystroke no omnibox disparava um fetch imediato ao daemon.

**Fix:** `omniboxTimer` module-level; `onInputChanged` usa `setTimeout(200ms)` com `clearTimeout` no keystroke anterior.

---

## P2-05 — Snippet de 200 chars insuficiente

**Arquivo:** `daemon/internal/store/sqlite.go`

**Problema:** 200 chars truncava demais para documentacao tecnica e artigos longos.

**Fix:** Limite aumentado para 400 chars.

---

## P2-06 — install.sh sem verificacao de pre-requisitos

**Arquivo:** `daemon/install/install.sh`

**Problema:** Script falhava silenciosamente se o binario nao existia — erros confusos para o usuario.

**Fix:** Checks no inicio do script: se `$1` fornecido, valida que o arquivo existe; se nao fornecido, valida que `$HOME/.local/bin/vbmd` existe. Mensagem de erro clara com instrucoes de build.

---

## P2-07 — (Resolvido em CR-002)

`After=network.target` removido do unit file em CR-002 (P1-11).

---

## P2-08 — Sem endpoint /metrics Prometheus

**Arquivos:** `daemon/internal/server/metrics.go` (novo), `daemon/internal/server/routes.go`

**Problema:** Sem metricas expostas — impossivel monitorar o daemon com Prometheus/Grafana.

**Fix:**
- `metrics.go`: struct `serverMetrics` com `atomic.Int64` para `ingestTotal`, `searchTotal`, `forgetTotal`, `wsActive`; handler `serveMetrics()` em formato Prometheus text (sem dependencia externa)
- `routes.go`: `GET /metrics` (sem auth) expoe contadores + gauges `vbm_pages_indexed` e `vbm_queue_pending` via `s.GetStatus()`
- Contadores incrementados nos handlers `/ingest`, `/search`, `/forget`; `wsActive` decrementado no `defer` do handler `/ws`

---

## P2-09 — Token do SW cacheado sem revalidacao apos restart do daemon

**Arquivos:** `extension/src/background/native-bridge.ts`, `extension/src/background/daemon-client.ts`

**Problema:** Apos restart do daemon (token rotacionado), o SW continuava enviando o token antigo — todas as requests retornavam 401 indefinidamente ate o SW ser reiniciado.

**Fix:**
- `native-bridge.ts`: `resetDaemon()` exportado — zera `daemonState.port` e `daemonState.token`
- `daemon-client.ts`: `checkResponse()` detecta HTTP 401 e chama `resetDaemon()` antes de lancar o erro; proxima chamada a `connectDaemon()` refaz o handshake automaticamente

---

## P2-10 — URL com query string indexada sem sanitizacao

**Arquivo:** `extension/src/background/service-worker.ts`

**Problema:** URLs como `https://site.com/page?token=abc&session=xyz` eram indexadas com tokens e session IDs expostos no banco local.

**Fix:** `sanitizeUrl(url)` remove 18 parametros de rastreamento/sessao conhecidos (`utm_*`, `fbclid`, `gclid`, `token`, `access_token`, `session`, `sid`, etc.) antes do ingest. URL sanitizada usada tambem para extrair o `domain`.

---

## Arquivos modificados

| Arquivo | P2s |
|---|---|
| `daemon/internal/server/metrics.go` (novo) | P2-08 |
| `daemon/internal/server/routes.go` | P2-01, P2-02, P2-08 |
| `daemon/internal/store/sqlite.go` | P2-02, P2-03, P2-05 |
| `daemon/internal/queue/queue.go` | P2-02 |
| `daemon/install/install.sh` | P2-06 |
| `extension/src/background/service-worker.ts` | P2-04, P2-10 |
| `extension/src/background/native-bridge.ts` | P2-09 |
| `extension/src/background/daemon-client.ts` | P2-09 |

---

## Verificacao

```bash
# Build
cd daemon && go build ./...
cd ../extension && npm run typecheck

# /metrics
curl http://127.0.0.1:$PORT/metrics
# deve retornar texto Prometheus com vbm_ingest_total, vbm_search_total, etc.

# /ui com token real
curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:$PORT/ui
# o JS nao deve conter REPLACE_TOKEN

# Queue cleanup
# apos ingest: SELECT count(*) FROM queue WHERE status='pending' deve ser 0 apos processamento

# Debounce omnibox
# digitar rapido no omnibox: apenas uma requisicao enviada ao daemon

# URL sanitizacao
# https://site.com?utm_source=google&token=abc deve ser indexada como https://site.com/

# Token revalidacao
# reiniciar daemon -> usar omnibox -> deve reconectar sem necessidade de reiniciar extensao
```
