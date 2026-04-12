# CR-004 — Findings Remanescentes (v1 + Regressoes v2)

**Data:** 2026-04-11
**Base:** CR-003
**Motivacao:** Apos CR-001/002/003, a avaliacao v2 identificou 4 issues abertos: 1 regressao P0 de CORS introduzida em CR-002, 1 P1 de logging deferred desde v1, 1 P1 de validacao de install.sh semi-resolvida, e 1 P1 de inconsistencia de tipos TypeScript. Este CR fecha o gap para nivel RC.

---

## P0-NEW — authMiddleware rejeita extraOrigins antes de corsMiddleware

**Arquivo:** `daemon/internal/server/routes.go`

**Problema:** O router registrava `authMiddleware` antes de `corsMiddleware` (linhas 92-93). O `authMiddleware` rejeitava com 401 qualquer origin que nao fosse `chrome-extension://` — incluindo origens listadas em `VBM_CORS_ORIGIN`. A env var era completamente inoperante para requests autenticadas. Regressao introduzida em CR-002.

**Fix:**
- `authMiddleware` agora recebe `extraOrigins []string` como parametro e constroi seu proprio `map[string]bool`
- Condicao de rejeicao: `origin != "" && !strings.HasPrefix(origin, "chrome-extension://") && !allowed[origin]`
- Chamada em `newRouter()` atualizada: `r.Use(authMiddleware(token, extraOrigins))`

---

## P1-NEW-A — App.tsx define StatusResponse localmente em vez de importar de proto/types.ts

**Arquivos:** `proto/types.ts`, `extension/src/popup/App.tsx`

**Problema:** `App.tsx` linhas 3-14 definia `StatusResponse` com campos `daemonPort: number | null` e `captureEnabled: boolean` e `ForgetRequest` — ambos ausentes do tipo canonico em `proto/types.ts`. Divergencia entre o contrato da API e o usado pelo popup.

**Fix:**
- `proto/types.ts`: adicionados `daemonPort: number | null` e `captureEnabled: boolean` ao `StatusResponse`
- `App.tsx`: removidas as definicoes locais; importa `StatusResponse` e `ForgetRequest` de `../../../proto/types`

---

## P1-13 — Logs nao estruturados (slog)

**Arquivos:** `daemon/cmd/vbmd/main.go`, `daemon/internal/server/server.go`, `daemon/internal/server/routes.go`, `daemon/internal/queue/queue.go`

**Problema:** Todos os logs usavam `log.Printf` / `log.Println` — sem nivel, sem campos estruturados, impossivel filtrar em producao.

**Fix:**
- `main.go`: configura `slog.SetDefault` com `slog.NewJSONHandler(os.Stderr, ...)` antes de `server.Run()`. Nivel controlado por `VBM_LOG_LEVEL=debug` (default: `info`). `nm-host` mode nao e afetado.
- `server.go`, `routes.go`, `queue.go`: todos os `log.Printf`/`log.Println` substituidos por `slog.Info`, `slog.Warn` ou `slog.Error` com campos estruturados (`"url"`, `"err"`, `"addr"`, `"ttl_days"`, etc.)
- Imports `"log"` removidos; `"log/slog"` adicionado

**Exemplo de saida:**
```json
{"time":"2026-04-11T10:00:00Z","level":"INFO","msg":"server listening","addr":"127.0.0.1:7700"}
{"time":"2026-04-11T10:00:01Z","level":"WARN","msg":"dropped ingest, buffer full","url":"https://example.com/page"}
```

---

## P1-14 — install.sh ainda escrevia REPLACE_ME se EXTENSION_ID nao fornecido

**Arquivo:** `daemon/install/install.sh`

**Problema:** Linha 64 usava `${EXTENSION_ID:-REPLACE_ME}` — se o usuario pressionasse Enter sem digitar o ID (ou rodasse sem TTY), o manifest era gravado com `REPLACE_ME` e o Native Messaging nao funcionava. CR-002 adicionara apenas um warning, nao um abort.

**Fix:**
- Se `EXTENSION_ID` env var nao estiver definida e houver TTY: loop interativo que repete o prompt ate receber um ID valido (exatamente 32 chars `a-p`)
- Se `EXTENSION_ID` env var nao estiver definida e nao houver TTY: aborta com mensagem `EXTENSION_ID env var not set`
- Se `EXTENSION_ID` env var estiver definida mas invalida: aborta com mensagem de erro
- Linha de sed usa `$EXTENSION_ID` sem fallback (garantidamente valido ao chegar la)
- Footer da instalacao simplificado (removida a branch REPLACE_ME)

---

## Arquivos modificados

| Arquivo | Issues |
|---|---|
| `daemon/internal/server/routes.go` | P0-NEW, P1-13 |
| `daemon/internal/server/server.go` | P1-13 |
| `daemon/internal/queue/queue.go` | P1-13 |
| `daemon/cmd/vbmd/main.go` | P1-13 |
| `proto/types.ts` | P1-NEW-A |
| `extension/src/popup/App.tsx` | P1-NEW-A |
| `daemon/install/install.sh` | P1-14 |
| `changes/CR-004-remaining-findings.md` | (este arquivo) |

---

## Verificacao

```bash
# Build
cd daemon && go build ./...
cd ../extension && npm run typecheck

# P0-NEW: CORS fix — VBM_CORS_ORIGIN agora funciona
# VBM_CORS_ORIGIN=http://localhost:3000 ./bin/vbmd server &
# curl -H "Origin: http://localhost:3000" -H "Authorization: Bearer $TOKEN" \
#      http://127.0.0.1:$PORT/search?q=test
# deve retornar 200, nao 401

# P1-13: slog JSON
VBM_LOG_LEVEL=debug ./bin/vbmd server 2>&1 | head -3
# deve retornar JSON: {"time":"...","level":"DEBUG","msg":"..."}

# P1-14: REPLACE_ME bloqueado
echo "" | bash daemon/install/install.sh 2>&1 | grep ERROR
# deve mostrar: ERROR: EXTENSION_ID env var not set

# P1-NEW-A: typecheck limpo
cd extension && npm run typecheck
# deve retornar sem erros
```
