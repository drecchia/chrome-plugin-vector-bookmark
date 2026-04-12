# CR-005 — Correcao de findings remanescentes: CORS, slog, install.sh, tipos TS

**Data:** 2026-04-11
**Solicitante:** projeto
**App Version:** 0.1.0
**Status:** implemented
**Urgencia:** high
**Dominios afetados:** Daemon server, Extension popup, Install/setup, Proto types

---

## Descricao da mudanca

Quatro correcoes independentes que fecham o gap para nivel RC identificado na avaliacao de maturidade v2:

1. `authMiddleware` em `routes.go` passou a aceitar `extraOrigins` — a env var `VBM_CORS_ORIGIN` era completamente inoperante porque o middleware rejeitava qualquer origin nao-`chrome-extension://` antes de `corsMiddleware` poder agir.
2. Todos os `log.Printf`/`log.Println` no daemon foram substituidos por `log/slog` com campos estruturados e saida JSON. Nivel controlado por `VBM_LOG_LEVEL=debug`.
3. `install.sh` passou a exigir um Extension ID valido (32 chars `a-p`) antes de continuar — antes aceitava Enter em branco e gravava `REPLACE_ME` no manifest, quebrando o Native Messaging silenciosamente.
4. `proto/types.ts` recebeu os campos `daemonPort` e `captureEnabled` que so existiam como interface local em `App.tsx`; `App.tsx` agora importa do tipo canonico.

## Motivacao

Findings identificados na avaliacao de maturidade v2 (2026-04-11). P0-NEW (CORS) era uma regressao introduzida em CR-002 que tornava `VBM_CORS_ORIGIN` inutilizavel. P1-13 (slog) estava deferred desde CR-002. P1-14 (REPLACE_ME) foi semi-resolvido em CR-002 com apenas um warning — usuarios em CI/CD ou que pressionavam Enter em branco obtinham um manifest quebrado. P1-NEW-A (App.tsx tipos) era divergencia entre o contrato canonico e o usado pelo popup.

## Comportamento atual (antes desta CR)

- Requests de origens em `VBM_CORS_ORIGIN` recebiam 401 — env var inoperante
- Logs em texto livre (`[vbmd] msg...`) sem nivel ou campo estruturado
- `install.sh` gravava `REPLACE_ME` no NM manifest se o usuario nao digitasse o ID
- `StatusResponse` e `ForgetRequest` definidas localmente em `App.tsx` com campos extras (`daemonPort`, `captureEnabled`) ausentes do tipo canonico

## Comportamento desejado (apos esta CR)

- Origens em `VBM_CORS_ORIGIN` sao aceitas por `authMiddleware` e `corsMiddleware`
- Logs em JSON estruturado: `{"time":"...","level":"INFO","msg":"...","addr":"..."}`
- `install.sh` exige ID valido antes de prosseguir; aborta em modo nao-interativo se `EXTENSION_ID` nao estiver definido
- `proto/types.ts` e a unica fonte de verdade para `StatusResponse` e `ForgetRequest`

---

## Analise de impacto

### Entidades afetadas

Nenhuma entidade de dados alterada. Mudancas sao de infraestrutura/tooling/tipagem.

### Backend

- `daemon/internal/server/routes.go`: assinatura de `authMiddleware` alterada (novo parametro `extraOrigins []string`); import `"log"` → `"log/slog"`
- `daemon/internal/server/server.go`: todos os `log.Printf` → `slog.*`; import atualizado
- `daemon/internal/queue/queue.go`: todos os `log.Printf` → `slog.*`; import atualizado
- `daemon/cmd/vbmd/main.go`: setup de `slog.SetDefault` com `JSONHandler` antes de `server.Run()`

### Frontend / Mobile

- `extension/src/popup/App.tsx`: removidas interfaces locais `StatusResponse` e `ForgetRequest`; adicionado import de `../../../proto/types`
- `proto/types.ts`: `StatusResponse` recebeu `daemonPort: number | null` e `captureEnabled: boolean`

### Dados existentes

Nao requer migracao.

### Integracoes externas

Nenhuma integracao afetada. A correcao do CORS e beneficio para clientes externos (dashboard local), nao uma quebra.

### Conflitos com decisoes existentes

Nenhum conflito identificado. O CLAUDE.md ja mandava bind exclusivo em `127.0.0.1` e nunca duplicar tipos de `proto/types.ts` — esta CR reafirma essas regras.

### CRs relacionadas

- CR-002: introduziu a regressao P0-NEW (authMiddleware ordering) e adicionou `VBM_CORS_ORIGIN` que nao funcionava
- CR-004: documenta os mesmos findings em formato tecnico (rascunho pre-verificacao)

---

## Decisoes necessarias antes de implementar

Nenhuma decisao pendente — implementacao concluida.

---

## Criterio de aceite

- [x] `go build ./...` sem erros
- [x] `npm run typecheck` sem erros
- [x] `curl -H "Origin: http://localhost:3000" -H "Authorization: Bearer $TOKEN" http://127.0.0.1:$PORT/search?q=test` retorna 200 quando `VBM_CORS_ORIGIN=http://localhost:3000`
- [x] `VBM_LOG_LEVEL=debug ./bin/vbmd server 2>&1 | head -1` retorna JSON valido
- [x] `echo "" | bash daemon/install/install.sh 2>&1` contem `ERROR: EXTENSION_ID env var not set`
- [x] `EXTENSION_ID=invalido bash daemon/install/install.sh 2>&1` contem `ERROR: EXTENSION_ID=`

---

## Atualizacoes de documentacao necessarias

Apos implementacao, os seguintes documentos devem ser atualizados:
- [x] `CLAUDE.md` — secao "Stack": mencionar `log/slog` como dependencia de logging
- [x] `DECISIONS.md` — adicionar entrada sobre slog como padrao de logging e sobre a correcao do CORS ordering

---

*CR-005 — gerado em 2026-04-11 — status: implemented*
