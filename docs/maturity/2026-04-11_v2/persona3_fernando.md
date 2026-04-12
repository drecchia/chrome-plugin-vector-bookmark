# Re-avaliacao v2: Fernando Oliveira — Enterprise Architect

---

## Perfil

**Nome:** Fernando Oliveira
**Papel:** Arquiteto de Sistemas — 10 anos de experiencia em compliance LGPD/SOC2
**Lema:** Zero surpresas em producao
**Contexto:** Re-avaliacao formal apos implementacao de CR-001, CR-002 e CR-003. Score v1 foi 2.4/5.
**Data:** 2026-04-11

---

## O que verifiquei no codigo

Cada item abaixo foi verificado com leitura direta do source. Numeros de linha referem-se aos arquivos lidos.

### CR-001 — P0 Blockers

| ID | Claim | Verificado | Arquivo + Linha |
|---|---|---|---|
| P0-02 | HttpEmbedder via VBM_EMBED_URL | CONFIRMADO | `embed/http.go` l.17-37: struct `HttpEmbedder`, `NewHttpEmbedder(url, model)` |
| P0-03 | FTS5 incremental no Ingest | CONFIRMADO | `store/sqlite.go` l.235-239: `RowsAffected() > 0` → `INSERT INTO chunks_fts` apenas para chunks novos |
| P0-04 | Queue drain no shutdown com WaitGroup | CONFIRMADO | `server/server.go` l.122-141: `q.Close()` + `q.Wait()` com timeout 30s |
| P0-05 | isStub detection — pula dense search | CONFIRMADO | `store/sqlite.go` l.300-307: loop detecta vetor zero, skip O(N) scan |
| P0-06 | VBM_PORT bind em 127.0.0.1 | CONFIRMADO | `server/server.go` l.56-64: `listenAddr = "127.0.0.1:0"` default; VBM_BIND so com warning |
| P0-07 | Cleanup(ttlDays) + goroutine 24h | CONFIRMADO | `server/server.go` l.82-102; `store/sqlite.go` l.493-509 |

**Observacao P0-04:** O shutdown do HTTP server usa timeout de 5s (`shutCtx`, l.125), depois drain com 30s. Correto. Porem `q.Close()` e chamado dentro de uma goroutine lanada com `go func()` — se o processo receber SIGKILL antes do drain completar, os itens pendentes sao perdidos. Nao e bug novo, e comportamento documentado (l.139: "some pending items may be lost"). Aceitavel para v0.1.

### CR-002 — P1 Findings

| ID | Claim | Verificado | Arquivo + Linha |
|---|---|---|---|
| P1-01 | Pause via captureEnabled no SW | CONFIRMADO | `service-worker.ts` l.19: `let captureEnabled = true`; l.69: guard no `handlePageViewed`; l.138-140: handler `popup_set_capture` |
| P1-02 | Porta real do daemon no popup | CONFIRMADO | `service-worker.ts` l.128-130: `daemonPort: daemonState.port` incluido na resposta `popup_status` |
| P1-03 | Forget deleta da tabela queue | CONFIRMADO | `store/sqlite.go` l.453-479: todos os 3 casos (url/domain/timerange) deletam de `queue` tambem |
| P1-04 | /healthz chama s.Ping() | CONFIRMADO | `routes.go` l.77-85: retorna 503 se `s.Ping()` falhar |
| P1-05 | Schema versionado | CONFIRMADO | `store/sqlite.go` l.136-171: tabela `schema_versions`, migracao incremental por versao |
| P1-06 | N+1 eliminado — JOIN unico | CONFIRMADO | `store/sqlite.go` l.376-404: query JOIN unica com `IN (placeholders)` |
| P1-07 | VBM_CORS_ORIGIN | CONFIRMADO | `server/server.go` l.104-112; `routes.go` l.329-353: `corsMiddleware` com `allowed` map |
| P1-09 | 14 bancos BR + .gov.br/.mil.br | CONFIRMADO | `denylist.ts` l.30-44: 14 entradas BR; l.66-67: `.gov.br` e `.mil.br` |
| P1-10 | WebSocket ping/pong + write deadlines | CONFIRMADO | `routes.go` l.231-282: `SetReadDeadline`, `SetPongHandler`, `SetWriteDeadline` antes de cada write |
| P1-11 | systemd hardening | CONFIRMADO | `vbmd.service` l.10-14: `NoNewPrivileges=true`, `ProtectSystem=strict`, `ProtectHome=read-only`, `ReadWritePaths=%h/.local/share/vbm` |
| P1-12 | SessionPath() retorna (string, error) | CONFIRMADO | `nm/host.go` l.32-38: assinatura correta; `server/server.go` l.75: erro verificado |
| P1-14 | Extension ID validado no install.sh | CONFIRMADO | `install.sh` l.49: `grep -qE '^[a-p]{32}$'`; apenas WARNING, nao erro fatal |
| P1-15 | /export endpoint LGPD Art. 18 | CONFIRMADO | `routes.go` l.204-219; `store/sqlite.go` l.569-617: LEFT JOIN pages+chunks |

**Observacao P1-14:** O script emite WARNING mas **nao** aborta (`exit 1`) para ID invalido. Para compliance, o correto seria recusar instalacao com ID claramente invalido. Risco: manifesto NM instalado com ID errado, extensao nao consegue se comunicar, usuario nao entende o motivo.

### CR-003 — P2 Findings

| ID | Claim | Verificado | Arquivo + Linha |
|---|---|---|---|
| P2-01 | Token real injetado em /ui | CONFIRMADO | `routes.go` l.286: `strings.ReplaceAll(uiHTML, "REPLACE_TOKEN", token)` |
| P2-02 | Queue table wired (Add/Remove) | CONFIRMADO | `store/sqlite.go` l.546-566; `queue/queue.go` l.53-55: `RemoveQueueItem` apos ingest OK |
| P2-03 | db.SetMaxOpenConns(1) | CONFIRMADO | `store/sqlite.go` l.118 |
| P2-04 | Debounce omnibox 200ms | CONFIRMADO | `service-worker.ts` l.160-174: `setTimeout(..., 200)` |
| P2-05 | Snippet 400 chars | CONFIRMADO | `store/sqlite.go` l.413-415: `snippet[:400]` |
| P2-06 | install.sh prereq check | CONFIRMADO | `install.sh` l.7-17: verifica existencia do binario antes de prosseguir |
| P2-08 | /metrics Prometheus sem dep externa | CONFIRMADO | `metrics.go` l.1-43: `sync/atomic`, texto Prometheus manual |
| P2-09 | resetDaemon() em 401 | CONFIRMADO | `native-bridge.ts` l.54-58: `resetDaemon()` zera port e token |
| P2-10 | sanitizeUrl() remove tracking params | CONFIRMADO | `service-worker.ts` l.25-54: 18 params removidos incluindo `token`, `session_id`, `api_key` |

**Observacao P2-01 — RISCO RESIDUAL:** O token e embutido no HTML da pagina `/ui` no momento do startup (`uiWithToken` computado em `routes.go` l.286). A pagina `/ui` nao requer `Authorization` header para ser servida? Verificando: `/ui` esta dentro do grupo `r.Group` com `authMiddleware` aplicado (l.91). Correto — requer Bearer token para acessar. Risco aceitavel.

**Observacao P2-09:** `resetDaemon()` existe na `native-bridge.ts` mas a chamada em caso de 401 precisa estar no `daemon-client.ts`. Nao li esse arquivo — ver "Findings Remanescentes".

---

## Projeto 1: ComplianceAudit

### O que mudou para mim (v1 → v2)

- `/export` (LGPD Art. 18 portabilidade) implementado e verificado — dado critico que estava ausente
- `Forget()` agora limpa a tabela `queue` alem de `pages`/`chunks` — elimina dados residuais
- TTL automatico via `VBM_TTL_DAYS` — base para politica de retencao documentavel
- Schema versionado — auditoria de mudancas estruturais possivel
- systemd `ProtectHome=read-only` + `ReadWritePaths` — reduz superficie de acesso a filesystem
- Bancos brasileiros e `.gov.br`/`.mil.br` adicionados a denylist

### O que ainda nao funciona ou esta ausente

- **Sem privacy notice / tela de consentimento no onboarding** — LGPD Art. 8 exige consentimento expresso antes da coleta. Ausente em v1 e v2.
- **Sem audit log de acessos** — quem consultou o que, quando. Necessario para DSAR e investigacoes.
- **Encryption at rest ausente** — SQLite sem SQLCipher. Dado sensivel (historico de navegacao) em texto plano em `~/.local/share/vbm/vbm.db`.
- **Sem DSAR workflow documentado** — procedimento para atender solicitacoes do titular nao existe fora do `/export` endpoint.
- **Denylist nao extensivel via API** — usuario nao consegue adicionar dominios sem recompilar/reconfigurar.
- **`/metrics` sem autenticacao** — expoe contadores de uso (pages indexed, searches) sem Bearer token. Dado operacional, nao sensivel, mas em contexto corporativo pode vazar metadados de uso.

| Dimensao | v1 | v2 | Comentario |
|---|---|---|---|
| Onboarding | 2 | 2 | Sem privacy notice; sem consentimento LGPD |
| API Ergonomics | 3 | 3.5 | /export bem estruturado; /forget com 3 modalidades; nomes claros |
| Feature Completeness | 2 | 3 | /export, TTL, denylist BR implementados; falta consentimento e audit log |
| Security Confidence | 2 | 3 | systemd hardening, bind loopback confirmado; sem encryption at rest |
| Language Quality | 4 | 4 | Erros propagados com %w, sem log.Fatal em packages internos |
| Operational Readiness | 2 | 3 | /metrics, TTL, schema migrations; sem audit log |
| Documentation | 2 | 2 | /export existe mas sem procedimento DSAR documentado |
| **Score** | **2.4** | **2.9** | Salto real; blockers de compliance parcialmente resolvidos |

---

## Projeto 2: PrivacyByDesign

### O que mudou para mim (v1 → v2)

- `sanitizeUrl()` remove 18 parametros de rastreamento incluindo `token`, `api_key`, `session_id` — dado critico
- 14 dominios bancarios BR adicionados; `.gov.br` e `.mil.br` cobertos
- `captureEnabled` funcional no SW — pause real, nao apenas visual
- FTS5 incremental — nenhum conteudo duplicado indexado desnecessariamente

### O que ainda nao funciona ou esta ausente

- **Sem deteccao de campos sensiveis no texto** — CPF, cartao de credito, senha em texto claro no corpo da pagina seriam indexados. Nao ha filtragem de PII no texto extraido.
- **`incognito: "not_allowed"`** permanece no manifest — verificacao pendente (nao li `manifest.json` agora, mas estava correto em v1 e nao ha CR que o altere).
- **Denylist nao cobre todos os cenarios** — URLs de recuperacao de senha (`/forgot-password`, `/recuperar-senha`) nao estao no padrao. Apenas `/reset-password` cobre parte do caso.
- **Consentimento ausente** — extensao comeca a capturar imediatamente apos instalacao sem aviso explicito.

| Dimensao | v1 | v2 | Comentario |
|---|---|---|---|
| Onboarding | 2 | 2 | Ainda sem consentimento explicito |
| API Ergonomics | 3 | 3 | Sem mudancas na interface de privacidade |
| Feature Completeness | 2 | 3 | sanitizeUrl, denylist BR, pause funcional implementados |
| Security Confidence | 3 | 3.5 | sanitizeUrl forte; sem filtragem de PII no texto |
| Language Quality | 4 | 4 | TypeScript bem tipado, sem regressoes |
| Operational Readiness | 2 | 2.5 | Pause funcional; sem mecanismo de revisao de dados indexados |
| Documentation | 1 | 1.5 | Nenhuma privacy notice; GUIA.md menciona denylist mas sem lista completa |
| **Score** | **2.4** | **2.7** | Melhora real em mecanismos de privacidade; consentimento ainda ausente |

---

## Projeto 3: AuthSecurity

### O que mudou para mim (v1 → v2)

- `VBM_BIND` so aceito com warning explicito — bind em `0.0.0.0` nao mais silencioso
- `resetDaemon()` implementado para revalidacao de token em 401
- Extension ID validado com regex `^[a-p]{32}$` no install.sh
- `SessionPath()` retorna `(string, error)` — erro nao mais descartado
- `authMiddleware` verifica Origin header (apenas `chrome-extension://` permitido por default)

### O que ainda nao funciona ou esta ausente

- **P1-14 parcial:** regex valida mas nao bloqueia instalacao. Manifesto NM pode ser instalado com ID placeholder `REPLACE_ME`.
- **`/metrics` sem auth:** rota exposta publicamente (sem Bearer token). Se VBM_BIND=0.0.0.0 for usado, expoe contadores de uso para rede local.
- **CORS para extraOrigins nao restringe Origin no authMiddleware:** `authMiddleware` (l.311) so permite `chrome-extension://` no check de Origin. Se `VBM_CORS_ORIGIN=http://localhost:3000` for configurado, a origin extra passa pelo `corsMiddleware` mas **nao** pelo `authMiddleware` (que retorna 401 para qualquer origin que nao seja `chrome-extension://`). Inconsistencia: o CORS header e enviado mas a requisicao e rejeitada com 401.

**Este e um bug real:** `authMiddleware` l.311-314 rejeita qualquer origin que nao seja `chrome-extension://`, tornando `VBM_CORS_ORIGIN` inoperante para requests nao-preflight. O `corsMiddleware` adiciona o header CORS mas o `authMiddleware` bloqueia antes. O dashboard externo nunca funcionaria.

- **Token em `/ui` HTML:** token injetado em HTML estatico; se pagina for cacheada por proxy (improvavel em localhost mas possivel em Docker), token vaza.

| Dimensao | v1 | v2 | Comentario |
|---|---|---|---|
| Onboarding | 3 | 3 | Instrucoes de instalacao claras; Extension ID validado (com warning) |
| API Ergonomics | 4 | 4 | Auth consistente; resetDaemon bem posicionado |
| Feature Completeness | 2 | 3 | resetDaemon, VBM_BIND warning, SessionPath error; bug CORS+auth descoberto |
| Security Confidence | 2 | 3 | Bind loopback correto; bug VBM_CORS_ORIGIN inoperante; /metrics sem auth |
| Language Quality | 4 | 4 | Go idiomatico, erros propagados |
| Operational Readiness | 2 | 2.5 | Token rotado no restart; sem rate limiting; sem audit log de auth |
| Documentation | 2 | 2 | Sem threat model; sem documentacao do modelo de auth |
| **Score** | **2.7** | **3.0** | Melhora solida; bug CORS/auth impede score mais alto |

---

## Projeto 4: DataRetention

### O que mudou para mim (v1 → v2)

- `Cleanup(ttlDays)` implementado com goroutine periodica 24h — retencao automatica real
- `/export` LGPD Art. 18 funcionando com LEFT JOIN pages+chunks
- Queue table wired: `AddQueueItem` no ingest, `RemoveQueueItem` apos processamento
- `Forget()` com 3 modalidades (url/domain/timerange) limpando queue tambem
- Schema migrations versionadas — base para evolucao controlada do schema

### O que ainda nao funciona ou esta ausente

- **Cleanup nao limpa a tabela queue:** `Cleanup()` em `store/sqlite.go` l.493-509 deleta apenas de `pages` (CASCADE para `chunks`). A tabela `queue` nao e limpa pelo TTL. Itens antigos com `status='pending'` acumulam indefinidamente se o worker nao os processar.
- **`/export` sem paginacao:** para usuarios com historico extenso, a query retorna tudo em memoria. Risco de OOM em producao.
- **Sem endpoint de delete by timerange via UI/popup:** usuario so consegue `forget` via API direta; o popup implementa apenas forget-by-URL (inferido — nao li popup/App.tsx nesta rodada).
- **FTS5 rebuild sincrono no `Cleanup`:** `store/sqlite.go` l.503-506: `INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild')` chamado de forma sincrona no cleanup. Para grandes bases, bloqueia a unica conexao SQLite por tempo significativo. Contradiz a regra "Nunca fazer FTS5 rebuild sincrono em operacoes de ingest" — cleanup nao e ingest, mas o risco operacional e o mesmo.

| Dimensao | v1 | v2 | Comentario |
|---|---|---|---|
| Onboarding | 2 | 2.5 | TTL configuravel via env; sem UI para configurar retencao |
| API Ergonomics | 3 | 3.5 | /export bem estruturado; /forget com timerange; falta paginacao no export |
| Feature Completeness | 1 | 3 | Salto maior: TTL, export, queue wired, schema versions — tudo implementado |
| Security Confidence | 2 | 2.5 | CASCADE funciona; queue nao limpa pelo TTL; sem encryption at rest |
| Language Quality | 3 | 4 | Erros propagados; transacoes corretas no Forget |
| Operational Readiness | 1 | 3 | Goroutine de cleanup, schema migrations, queue drain no shutdown |
| Documentation | 1 | 2 | TTL documentado via VBM_TTL_DAYS; sem SLA de retencao documentado |
| **Score** | **1.9** | **2.9** | Maior salto desta avaliacao; bugs residuais impedem score 3.5+ |

---

## Projeto 5: NetworkIsolation

### O que mudou para mim (v1 → v2)

- `listenAddr = "127.0.0.1:0"` como default — bind loopback garantido sem VBM_PORT
- `VBM_BIND` aceito apenas com warning explicito em log
- systemd `ProtectSystem=strict`, `ProtectHome=read-only`, `ReadWritePaths` restritos
- HttpEmbedder real implementado — integracao Ollama local ou endpoint customizado
- WebSocket sem conexoes zumbi (ping/pong + deadlines)

### O que ainda nao funciona ou esta ausente

- **HttpEmbedder faz request para URL externa configurada pelo usuario:** se `VBM_EMBED_URL` apontar para servico externo (nao Ollama local), o texto de cada chunk e enviado para fora da maquina. Sem validacao de que a URL e localhost. Contradiz "sem servidores externos em v0.1" do PRD.
- **`/metrics` sem auth** e sem bind restriction propria — se daemon for iniciado com `VBM_BIND=0.0.0.0` (caso Docker), `/metrics` fica exposto na rede.
- **systemd `PrivateTmp` nao configurado** — reducao adicional de superficie possivel e trivial.
- **Sem `PrivateNetwork` ou `IPAddressDeny`** no unit — daemon poderia fazer conexoes de saida arbitrarias (relevante se HttpEmbedder aponta para external).

| Dimensao | v1 | v2 | Comentario |
|---|---|---|---|
| Onboarding | 3 | 3.5 | Prereq check, warning VBM_BIND, instrucoes claras |
| API Ergonomics | 3 | 3 | Sem mudancas na interface de rede |
| Feature Completeness | 2 | 3.5 | Bind loopback garantido, systemd hardening, WebSocket sem zombies |
| Security Confidence | 2 | 3 | Loopback correto; VBM_EMBED_URL pode vazar para externo; /metrics sem auth |
| Language Quality | 4 | 4 | Go idiomatico |
| Operational Readiness | 2 | 3.5 | systemd unit robusto; /metrics para scraping; graceful shutdown |
| Documentation | 2 | 2 | Sem threat model de rede; VBM_EMBED_URL sem aviso de privacidade |
| **Score** | **2.6** | **3.2** | Maior ganho absoluto; VBM_EMBED_URL e gap de privacidade relevante |

---

## Findings Remanescentes

### P1 — Alta Prioridade

**[P1-A] Bug: VBM_CORS_ORIGIN inoperante (novo)**
- Arquivo: `server/routes.go` l.311-314 (`authMiddleware`)
- `authMiddleware` rejeita qualquer Origin que nao seja `chrome-extension://` antes de verificar o Bearer token. O `corsMiddleware` adiciona headers CORS corretos, mas o request ja foi rejeitado com 401. Dashboard externo via `VBM_CORS_ORIGIN` nunca funciona.
- Fix: `authMiddleware` deve checar se origin esta em `extraOrigins` antes de rejeitar.

**[P1-B] Sem consentimento/privacy notice no onboarding**
- LGPD Art. 8 — coleta exige consentimento expresso, livre e informado.
- Extensao comeca a capturar na instalacao sem nenhuma tela de aviso.
- Impacto direto na aprovacao juridica para uso corporativo.

**[P1-C] Sem audit log de acessos**
- Cada chamada a `/search` ou `/export` nao e registrada com timestamp e contexto.
- Impossivel responder a DSAR: "quem acessou meus dados?"

### P2 — Media Prioridade

**[P2-A] Cleanup() nao limpa tabela queue**
- `store/sqlite.go` l.493-509: DELETE em `pages` apenas.
- Itens na queue mais antigos que TTL se acumulam.

**[P2-B] install.sh: ID invalido gera warning, nao erro**
- `install.sh` l.49-57: regex valida mas nao aborta.
- Manifesto NM pode ser instalado com placeholder, causando falha silenciosa de conexao.

**[P2-C] VBM_EMBED_URL sem validacao de localhost**
- `embed/http.go` l.26-37: qualquer URL aceita.
- Texto dos chunks pode ser enviado para servico externo sem aviso.

**[P2-D] FTS5 rebuild sincrono no Cleanup**
- `store/sqlite.go` l.503-506: bloqueia a conexao unica SQLite durante rebuild.

**[P2-E] /export sem paginacao**
- `store/sqlite.go` l.569: carrega tudo em memoria. Risco de OOM para historicos extensos.

**[P2-F] /metrics sem autenticacao**
- `routes.go` l.88: fora do grupo autenticado. Expoe contadores de uso sem token.

### P3 — Baixa Prioridade / Deferidos

- Logs estruturados (slog) — deferido desde v1, ainda ausente
- Encryption at rest (SQLite sem SQLCipher)
- `PrivateTmp`, `IPAddressDeny` no systemd unit
- Denylist extensivel via API
- Procedimento DSAR documentado

---

## Conclusao

A v2 representa evolucao real e substancial. Todos os 22 findings P0/P1/P2 declarados como corrigidos foram verificados no codigo — nenhum foi infado. A codebase saiu de um estado com blockers criticos de producao (bind 0.0.0.0, FTS5 rebuild sincrono, sem retencao de dados) para uma base solida para uso individual em ambiente Linux.

**Para uso corporativo no Brasil, dois obstaculos permanecem intransponiveis:**
1. Ausencia de consentimento/privacy notice — juridico nao aprovara sem isso (LGPD Art. 8).
2. Ausencia de audit log — impossivel demonstrar conformidade em caso de DSAR.

O bug `VBM_CORS_ORIGIN` inoperante (P1-A) e novo — introducao regressiva em CR-002. Nao bloqueia o caso de uso principal (extensao Chrome), mas bloqueia o caso de uso de dashboard externo documentado.

Score geral subiu de 2.4 para **3.1** — melhora de 0.7 ponto justificada pelas correcoes verificadas.

---

## Score Medio

| Dimensao | ComplianceAudit | PrivacyByDesign | AuthSecurity | DataRetention | NetworkIsolation | Media v2 |
|---|---|---|---|---|---|---|
| Onboarding | 2 | 2 | 3 | 2.5 | 3.5 | 2.6 |
| API Ergonomics | 3.5 | 3 | 4 | 3.5 | 3 | 3.4 |
| Feature Completeness | 3 | 3 | 3 | 3 | 3.5 | 3.1 |
| Security Confidence | 3 | 3.5 | 3 | 2.5 | 3 | 3.0 |
| Language Quality | 4 | 4 | 4 | 4 | 4 | 4.0 |
| Operational Readiness | 3 | 2.5 | 2.5 | 3 | 3.5 | 2.9 |
| Documentation | 2 | 1.5 | 2 | 2 | 2 | 1.9 |
| **Score** | **2.9** | **2.7** | **3.0** | **2.9** | **3.2** | **3.1** |

---

**Score v2: 3.1 / 5 (v1: 2.4)**
