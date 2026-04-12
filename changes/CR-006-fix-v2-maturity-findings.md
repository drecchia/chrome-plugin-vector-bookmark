# CR-006 — Correcao dos Findings do Relatorio de Maturidade v2

**Data:** 2026-04-11
**Base:** CR-005
**Motivacao:** Relatorio de maturidade v2 (`docs/maturity/2026-04-11_v2/MATURITY_REPORT.md`) identificou 1 P0 (regressao de CORS/auth ordering), 2 novos P1s e Documentation 2.3 como os bloqueadores principais para RC. Exploracao da codebase confirmou o P0, mas tres findings apontados como abertos (P1-NEW-A, P1-13, P2-NEW) ja estavam resolvidos em CRs anteriores — foram verificados e marcados STALE neste CR para historico.

Este CR fecha o que de fato resta aberto:
- P0-NEW (CORS/auth middleware order) + suite de testes de regressao.
- Documentation (novo OPERATIONS.md, extensao de GUIA.md com embeddings, dicas de omnibox e FAQ de privacidade).

---

## P0-NEW — authMiddleware executa antes de corsMiddleware

**Arquivo:** `daemon/internal/server/routes.go` (linhas 91-98)

**Problema:** `newRouter` registrava os middlewares na ordem `authMiddleware → corsMiddleware`. Em requisicoes nao-preflight vindas de `VBM_CORS_ORIGIN`, `authMiddleware` rejeitava com 401 antes de `corsMiddleware` setar `Access-Control-Allow-*`. O browser transformava o 401 sem headers CORS em erro de rede opaco, tornando `VBM_CORS_ORIGIN` silenciosamente inoperante para qualquer dashboard externo. Preflight OPTIONS funcionava por acaso porque `authMiddleware` ja tem um short-circuit para OPTIONS (linha 315-318), mas requisicoes reais (GET/POST/DELETE) quebravam.

**Fix:** Swap da ordem dos dois `r.Use(...)` dentro do `r.Group`:

```go
r.Group(func(r chi.Router) {
    // P0-NEW (v2): corsMiddleware MUST run before authMiddleware so that
    // Access-Control-Allow-* headers are attached to every response
    // (including 401 from auth rejection).
    r.Use(corsMiddleware(extraOrigins))
    r.Use(authMiddleware(token, extraOrigins))
    // ...rotas
})
```

**Rationale:** OPTIONS preflight continua bypassando auth via o early return em `authMiddleware` E pelo early return em `corsMiddleware` (linha 357-360 retorna 204 para OPTIONS apos setar headers). Para requests reais, headers CORS passam a ser setados antes do auth avaliar token/origin. Um dashboard externo legitimo recebe 200 + ACAO; um cliente nao-autorizado recebe 401 + ACAO (browser consegue ler o corpo e reportar erro significativo).

**Seguranca preservada:** `authMiddleware` ainda rejeita origens nao listadas em `extraOrigins` (linha 322) mesmo com CORS rodando antes — ou seja, um `evil.com` que mandar request com token valido ainda recebe 401. `corsMiddleware` so seta ACAO quando o origin bate com `chrome-extension://*` ou com `extraOrigins`, entao origens desconhecidas recebem 401 sem headers CORS, como esperado.

**Testes:** 6 casos novos em `daemon/internal/server/routes_test.go` — ver secao "Testes" abaixo.

---

## Docs — OPERATIONS.md ausente + GUIA.md incompleto

**Arquivos:**
- `docs/OPERATIONS.md` (novo)
- `docs/GUIA.md` (extensao com 3 secoes)

**Problema:** A dimensao Documentation do relatorio de maturidade v2 ficou em 2.3/5 (abaixo do limiar 3.5 exigido para RC) porque:
- Nao havia runbook operacional separado — operadores e integradores tinham que vasculhar codigo para descobrir `VBM_EMBED_URL`, `VBM_TTL_DAYS`, endpoints de metrics, etc.
- Setup de busca semantica real (Ollama) nao estava documentado em lugar nenhum — o stub embedder era a unica opcao "descoberta".
- Nao havia exemplo de Prometheus scrape config nem alerts de referencia.
- Troubleshooting de CORS para dashboards externos estava ausente.
- `GUIA.md` focava em usuario final e nao cobria o workflow de "quero que a busca realmente funcione com embeddings".

**Fix:**

### Novo `docs/OPERATIONS.md` (~500 linhas pt-BR)

10 secoes cobrindo todo o ciclo de operacao:
1. Pre-requisitos (SO, systemd, Go, Node, Chrome, Ollama opcional)
2. Instalacao passo-a-passo + checklist pos-instalacao
3. Tabela completa de variaveis de ambiente com defaults, fonte no codigo e exemplo de drop-in systemd
4. Setup de busca semantica com Ollama — instalacao, modelos, protocolo `/api/embeddings`, verificacao, migracao
5. Retencao de dados LGPD — `VBM_TTL_DAYS`, export via `/export`, esquecimento manual via `DELETE /forget`
6. Observabilidade — endpoints `/healthz`/`/metrics`/`/status`, lista completa de metricas Prometheus, scrape config estatico e `file_sd_configs` dinamico, tres alerts de exemplo (`VbmQueueBackpressure`, `VbmDaemonDown`, `VbmNoIngest`)
7. Logs estruturados com slog — formato JSON, filtros `jq`, habilitar DEBUG temporariamente
8. Troubleshooting — 7 cenarios comuns (daemon nao inicia, extensao desconectada, busca repetitiva, CORS error em dashboard externo, timeout do embedder, FTS5 inconsistente, banco corrompido)
9. Desinstalacao limpa (passos manuais + `make uninstall`)
10. Referencias cruzadas para GUIA.md, CLAUDE.md, CODEBASE_SNAPSHOT.md, DECISIONS.md

### Extensao de `docs/GUIA.md`

Tres secoes novas, sem reescrever o que ja existia:

- **Busca semantica com embeddings** (apos §Instalacao): setup rapido do Ollama em 3 comandos, drop-in systemd, explicacao de stub vs. real, link para OPERATIONS.md §4 para detalhes.
- **Dicas de query no omnibox** (dentro de §Uso diario, subsecao de busca): exemplos bons (`pagamento pix brasil`, `react hooks useeffect cleanup`) e ruins (`docker`, `configuracao`), fallbacks quando a busca nao acha, orientacao sobre comprimento ideal (2-5 palavras).
- **FAQ de Privacidade** (dentro de §Privacidade): 7 perguntas respondidas — envio externo, paginas sensiveis, esquecimento, retencao, backup/exportacao, multiusuario na maquina, comportamento em crash.

**Rationale:** `GUIA.md` segue focado em usuario final casual; `OPERATIONS.md` absorve o runbook pesado para operadores e integradores. Separacao reduz ruido para quem so quer instalar e usar, e da profundidade para quem precisa configurar Ollama, Prometheus, ou auditar LGPD. As duas secoes extras no GUIA.md (embeddings + FAQ privacidade) resolvem as queixas mais comuns do relatorio sem sobrecarregar o guia principal.

---

## Findings stale verificados e marcados resolvidos

Os tres findings abaixo estavam listados como abertos em `docs/maturity/2026-04-11_v2/MATURITY_REPORT.md §5` mas a exploracao da codebase confirmou que ja foram resolvidos (provavelmente em CRs anteriores aos que o relatorio v2 viu). Documentado aqui para que a proxima avaliacao de maturidade (v3) nao os conte como abertos:

### P1-NEW-A — App.tsx type duplication

**Status:** STALE (resolvido antes de CR-006).

**Evidencia:** `extension/src/popup/App.tsx:1-2`:

```typescript
import React, { useEffect, useState } from 'react';
import type { StatusResponse, ForgetRequest } from '../../../proto/types';
```

App.tsx ja importa ambos os tipos de `proto/types.ts`. Nao ha definicao local de `StatusResponse` ou `ForgetRequest` em arquivo algum da extensao. Os tipos locais mencionados no relatorio v2 (linhas 3-14 de App.tsx) nao existem na codebase atual.

**Acao:** Nenhuma. Remover do tracking de issues abertos.

### P1-13 — slog estruturado ausente

**Status:** STALE (ja integrado).

**Evidencia:**
- `log/slog` importado em `daemon/cmd/vbmd/main.go`, `daemon/internal/server/server.go`, `daemon/internal/queue/queue.go`, `daemon/internal/server/routes.go`.
- Handler JSON configurado em `main.go:30` com nivel controlado por `VBM_LOG_LEVEL` (main.go:27).
- `grep 'log\.Printf\|fmt\.Printf\|log\.Println' daemon/` retorna **zero** matches — nao ha mais logging via pacotes legados.
- `metrics.go` usa `fmt.Fprintf` intencionalmente (escrita de texto Prometheus, nao logging).

**Acao:** Nenhuma. Documentar filtros `jq` para slog em `OPERATIONS.md §7` (feito neste CR).

### P2-NEW — proto/types.ts faltando daemonPort/captureEnabled

**Status:** STALE (proto correto, o "bug" foi um erro de interpretacao do relatorio).

**Evidencia:** `proto/types.ts:43-49`:

```typescript
export interface StatusResponse {
    indexed: number;
    pending: number;
    version: string;
    daemonPort: number | null;
    captureEnabled: boolean;
}
```

Ambos os campos estao declarados. O relatorio v2 parece ter se confundido com o fato de que a resposta do endpoint `/status` do daemon nao inclui esses campos diretamente — mas isso eh intencional: `extension/src/background/service-worker.ts:123-131` compoe a resposta final no handler `popup_status`, fazendo spread da resposta do daemon e acrescentando `daemonPort` do `daemonState` (memoria do SW) e `captureEnabled` do estado local. O tipo `StatusResponse` em `proto/types.ts` modela corretamente o contrato **popup↔service worker**, nao o contrato **service worker↔daemon**.

**Acao:** Nenhuma. Nao adicionar os campos ao handler `/status` do daemon — isso quebraria a separacao de camadas (o daemon nao conhece `captureEnabled`, que eh estado puramente da extensao).

---

## Testes

**Novo arquivo:** `daemon/internal/server/routes_test.go` (primeira suite de testes do daemon).

Testes usam apenas stdlib (`testing`, `net/http/httptest`) e testam o middleware chain diretamente via composicao de funcoes, sem depender do chi.Router ou de `*store.Store`/`*queue.Queue` reais. Isso mantem o teste como unit test puro e evita o quirk do chi onde OPTIONS em rotas `r.Get` retorna 405 antes dos middlewares rodarem.

**Casos:**

1. **TestCORSHeadersPresentOn401** — regressao canonica. Request autenticada sem Authorization, origin em `extraOrigins`. Espera 401 + `Access-Control-Allow-Origin: http://localhost:3000` + `Access-Control-Allow-Methods` + `Access-Control-Allow-Headers: Authorization`. Reverter o swap em routes.go:97-98 faz este teste falhar (validado).

2. **TestCORSPreflightStillBypassesAuth** — OPTIONS `/search` com Origin whitelistado. Espera 204 + headers CORS completos. Garante que a mudanca nao quebrou preflights.

3. **TestAuthorizedRequestFromAllowedOrigin200** — caminho feliz. GET `/status` com Origin whitelistado + Bearer token valido. Espera 200 + ACAO + Content-Type JSON.

4. **TestDisallowedOriginNoCORSHeaders** — seguranca. GET `/status` com Origin `evil.com` + token valido. Espera 401 + ACAO **ausente**. Garante que a fix nao afrouxa a seguranca para origens desconhecidas.

5. **TestBuggyOrderProducesCORSlessOn401** — red test que prova a forma do bug. Compoe os middlewares na ordem ANTIGA (`auth(cors(...))`) e confirma que 401 vem sem headers CORS. Se este teste comecar a passar sem mudanca, o comportamento dos middlewares ou do chi mudou e os outros testes precisam ser revisados.

6. **TestChromeExtensionOriginAllowed** — sanity. GET `/status` com Origin `chrome-extension://...` e token valido, sem extraOrigins configurado. Espera 200 + ACAO ecoado. Garante que o cliente principal (a extensao) continua funcionando.

**Resultado da execucao:**

```
=== RUN   TestCORSHeadersPresentOn401             --- PASS
=== RUN   TestCORSPreflightStillBypassesAuth      --- PASS
=== RUN   TestAuthorizedRequestFromAllowedOrigin200 --- PASS
=== RUN   TestDisallowedOriginNoCORSHeaders       --- PASS
=== RUN   TestBuggyOrderProducesCORSlessOn401     --- PASS
=== RUN   TestChromeExtensionOriginAllowed        --- PASS
PASS
ok  	github.com/vbm/daemon/internal/server	0.008s
```

**Verificacao manual adicional (smoke test end-to-end):**

```bash
cd daemon && make build && VBM_CORS_ORIGIN=http://localhost:3000 ./bin/vbmd server &
PORT=$(jq -r .port ~/.local/share/vbm/session.json)
TOKEN=$(jq -r .token ~/.local/share/vbm/session.json)

curl -i "http://127.0.0.1:$PORT/status" \
  -H "Origin: http://localhost:3000" \
  -H "Authorization: Bearer $TOKEN"
# Esperado: 200 + Access-Control-Allow-Origin: http://localhost:3000

curl -i "http://127.0.0.1:$PORT/status" \
  -H "Origin: http://localhost:3000"
# Esperado: 401 + Access-Control-Allow-Origin: http://localhost:3000 (era o bug: sem ACAO)
```

---

## Nota sobre escopo

Este CR se limita aos findings do relatorio v2 que (a) eram reais e (b) podiam ser resolvidos sem mudanca arquitetural. Tres items nao foram tocados e permanecem como trabalho pos-RC:

- **P1-NEW-B (full scan O(N) com >50k chunks)** — requer integracao com `sqlite-vec` (ANN index). Planejado para v0.2 conforme `DECISIONS.md`.
- **UI de busca no /ui** — atualmente apenas um formulario basico com o token injetado (P2-01 de CR-003). Refinamento de UX eh backlog, nao bloqueia RC.
- **Suporte macOS/Windows** — v0.1 eh Linux-only por design; porting eh v0.3.

---

## Projecao de impacto no score

Baseado nas dimensoes do relatorio v2:

| Dimensao | v2 | Post-CR-006 | Delta | Justificativa |
|---|---|---|---|---|
| Onboarding | 3.1 | ~3.6 | +0.5 | Checklist pos-install + Ollama setup em 3 comandos |
| API Ergonomics | 3.5 | 3.5 | 0 | Sem mudanca de API |
| Feature Completeness | 3.2 | 3.2 | 0 | Sem features novas |
| Security Confidence | 3.5 | ~3.7 | +0.2 | CORS funcional + testes de seguranca (disallowed origin) |
| Language Quality | 3.6 | ~3.7 | +0.1 | Primeira suite de testes adicionada |
| Operational Readiness | 3.1 | ~3.8 | +0.7 | Runbook completo, alerts de Prometheus, troubleshooting |
| Documentation | 2.3 | ~3.6 | +1.3 | OPERATIONS.md + 3 secoes em GUIA.md |

**Score composto projetado: 3.2 → ~3.6** (cruza o limiar de RC 3.5).

**P0 abertos: 1 → 0.**

Proxima avaliacao de maturidade (v3) deve validar esses deltas e detectar novos findings residuais se houver.

---

## Arquivos alterados

| Arquivo | Tipo | Descricao |
|---|---|---|
| `daemon/internal/server/routes.go` | Edit | Swap linhas 92-93 (CORS antes de auth) + comentario explicativo |
| `daemon/internal/server/routes_test.go` | Novo | 6 testes cobrindo ordering, preflight, seguranca, happy path, regressao |
| `docs/OPERATIONS.md` | Novo | Runbook operacional completo em pt-BR |
| `docs/GUIA.md` | Edit | +3 secoes: Busca semantica com embeddings, dicas de omnibox, FAQ de privacidade |
| `changes/CR-006-fix-v2-maturity-findings.md` | Novo | Este documento |
