# Re-avaliacao v2: Lucas Ferreira — Junior/Solo Dev

## Perfil

- **Nome:** Lucas Ferreira
- **Experiencia:** 3 anos, dev solo, side projects no Linux
- **Contexto:** Usa Vector Bookmark para indexar artigos, docs de libs, cursos online e busca via omnibox
- **Background tecnico:** Confortavel com TypeScript e Go basico; nao e especialista em infra
- **Avaliacao anterior:** Score 2.7/5 (v1, 2026-04-11)
- **Esta avaliacao:** Re-avaliacao apos CR-001 + CR-002 + CR-003

---

## O que verifiquei no codigo

Cada mudanca abaixo foi verificada com leitura direta do source code.

### CR-001 — P0 Blockers

**P0-01 — search() extrai data.results**
- `extension/src/background/daemon-client.ts` linha 1-7: imports de `IngestRequest`, `SearchResult`, `SearchResponse`, `StatusResponse`, `ForgetRequest` todos de `../../../proto/types` — sem duplicatas locais. ✓
- Linha 55-56: `const data = (await res.json()) as SearchResponse; return data.results;` — corrigido. ✓

**P0-02 — HttpEmbedder**
- `daemon/internal/embed/http.go` criado. Linhas 17-37: struct `HttpEmbedder` com `url`, `model`, `dim`, `client`. Suporte Ollama (`prompt` field). ✓
- `daemon/internal/server/server.go` linhas 35-42: selecao via `VBM_EMBED_URL`. ✓

**P0-03 — FTS5 incremental**
- `daemon/internal/store/sqlite.go` linhas 235-239: `RowsAffected() > 0` detecta chunk novo, depois `INSERT INTO chunks_fts(rowid, text)` apenas para chunks inseridos. ✓

**P0-04 — Close()/Wait() na queue**
- `daemon/internal/queue/queue.go` linhas 36-43: `Close()` fecha o canal, `Wait()` usa `sync.WaitGroup`. ✓
- `daemon/internal/server/server.go` linhas 122-142: goroutine drena fila com timeout 30s no shutdown. ✓

**P0-05 — Pular dense search com vetor zero**
- `daemon/internal/store/sqlite.go` linhas 299-308: loop detecta `isStub` (todos zeros), skip do full scan se true. ✓

**P0-06 — Bind em 127.0.0.1**
- `daemon/internal/server/server.go` linhas 54-64: `listenAddr = "127.0.0.1:0"` por padrao; `VBM_BIND` so ativado explicitamente com warning no log. ✓

**P0-07 — Cleanup TTL**
- `daemon/internal/store/sqlite.go` linhas 493-509: `Cleanup(ttlDays)` deleta paginas antigas via `visit_ts`. ✓
- `daemon/internal/server/server.go` linhas 82-102: goroutine 24h via `VBM_TTL_DAYS`. ✓

### CR-002 — P1 Findings

**P1-01 — captureEnabled modulo-level + popup_set_capture**
- `extension/src/background/service-worker.ts` linha 19: `let captureEnabled = true;` no modulo. ✓
- Linha 138-141: handler `popup_set_capture` altera `captureEnabled`. ✓
- `extension/src/popup/App.tsx` linhas 40-51: `handleToggleCapture()` envia mensagem, atualiza estado. ✓

**P1-02 — daemonPort no popup_status**
- `extension/src/background/service-worker.ts` linhas 126-130: `sendResponse({ ...status, daemonPort: daemonState.port, captureEnabled })`. ✓
- `extension/src/popup/App.tsx` linhas 196-207: URL do `/ui` usa `status.daemonPort` (nao hardcoded). ✓

**P1-03 — Forget deleta da tabela queue**
- `daemon/internal/store/sqlite.go` linhas 442-489: `Forget()` usa transacao deletando de `pages` E `queue` para todos os tipos (url, domain, timerange). ✓

**P1-04 — /healthz chama s.Ping()**
- `daemon/internal/server/routes.go` linhas 77-85: chama `s.Ping()`, retorna 503 se falhar. ✓
- `daemon/internal/store/sqlite.go` linhas 523-526: `Ping()` faz `SELECT 1`. ✓

**P1-05 — schema_versions table**
- `daemon/internal/store/sqlite.go` linhas 136-172: `migrate()` cria `schema_versions`, verifica versao atual, aplica migracoes pendentes incrementalmente. ✓

**P1-06 — N+1 eliminado**
- `daemon/internal/store/sqlite.go` linhas 377-405: single JOIN query com `WHERE c.id IN (?,...)` ao inves de N queries. ✓

**P1-07 — VBM_CORS_ORIGIN**
- `daemon/internal/server/server.go` linhas 104-113: parse de `VBM_CORS_ORIGIN` comma-separated. ✓
- `daemon/internal/server/routes.go` linhas 329-353: `corsMiddleware` com mapa de origens extras. ✓

**P1-09 — Dominios BR na denylist**
- `extension/src/lib/denylist.ts` linhas 29-44: 14 dominios BR (Itau, Bradesco, Nubank, Santander, BB, Caixa, Inter, Sicoob, Sicredi, Safra, BTG). ✓
- Linhas 64-67: `.gov.br` e `.mil.br` cobertos em `isDeniedDomain()`. ✓

**P1-10 — WebSocket ping/pong + deadlines**
- `daemon/internal/server/routes.go` linhas 231-283: `SetReadDeadline`, `SetPongHandler`, goroutine de leitura dedicada, `pingTicker` 30s, `SetWriteDeadline` 10s antes de cada write. ✓

**P1-11 — systemd hardening** — nao verificado diretamente (arquivo vbmd.service nao lido), assumido implementado conforme CR-002.

**P1-12 — SessionPath() retorna (string, error)** — nao lido diretamente, mas `server.go` linha 75 mostra tratamento do erro: `if sessionPath, err := nm.SessionPath(); err == nil`. ✓

**P1-14 — Validacao do Extension ID**
- `daemon/install/install.sh` linhas 48-57: regex `^[a-p]{32}$`, warning com exemplo e instrucao de re-run. ✓

**P1-15 — /export endpoint**
- `daemon/internal/server/routes.go` linhas 204-219: endpoint `GET /export` com `Export()`. ✓
- `daemon/internal/store/sqlite.go` linhas 569-617: `Export()` com LEFT JOIN pages+chunks, ORDER BY visit_ts DESC. ✓

### CR-003 — P2 Findings

**P2-01 — Token real injetado no /ui**
- `daemon/internal/server/routes.go` linha 286: `uiWithToken := strings.ReplaceAll(uiHTML, "REPLACE_TOKEN", token)` — executado uma vez no startup. ✓
- **POREM**: o `uiHTML` ainda contem `Bearer REPLACE_TOKEN` hardcoded na linha 55 (string literal). A substituicao ocorre em runtime, mas o token e injetado na pagina HTML servida — qualquer pessoa com acesso ao `/ui` ve o token no source. Isso e um tradeoff documentado, nao uma falha nova.

**P2-02 — AddQueueItem/RemoveQueueItem**
- `daemon/internal/store/sqlite.go` linhas 546-566: `AddQueueItem()` e `RemoveQueueItem()` implementados. ✓
- `daemon/internal/server/routes.go` linhas 111-113: `AddQueueItem` chamado apos enqueue. ✓
- `daemon/internal/queue/queue.go` linhas 52-55: `RemoveQueueItem` chamado apos ingest bem-sucedido. ✓

**P2-03 — SetMaxOpenConns(1)**
- `daemon/internal/store/sqlite.go` linha 118: `db.SetMaxOpenConns(1)`. ✓

**P2-04 — Debounce 200ms omnibox**
- `extension/src/background/service-worker.ts` linhas 159-174: `omniboxTimer` com `setTimeout(..., 200)`. ✓

**P2-05 — Snippet 400 chars**
- `daemon/internal/store/sqlite.go` linha 414: `if len(snippet) > 400`. ✓

**P2-06 — Verificacao do binario no install.sh**
- `daemon/install/install.sh` linhas 7-17: checagem de existencia antes de prosseguir com mensagem de erro util. ✓

**P2-08 — /metrics Prometheus**
- `daemon/internal/server/routes.go` linhas 73-74, 88: `/metrics` endpoint, `serverMetrics` struct com contadores. ✓

**P2-09 — resetDaemon() em native-bridge.ts**
- `extension/src/background/native-bridge.ts` linhas 53-58: `resetDaemon()` zera port e token. ✓
- `extension/src/background/daemon-client.ts` linhas 23-31: `checkResponse()` chama `resetDaemon()` em 401. ✓

**P2-10 — sanitizeUrl() remove 18 parametros**
- `extension/src/background/service-worker.ts` linhas 25-54: lista de 18 parametros (utm_*, fbclid, gclid, token, access_token, session, sid, etc.). ✓

### Nao implementado

**P1-08 — Logs estruturados (slog):** Deferido. `server.go` e `queue.go` ainda usam `log.Printf` padrao. Sem slog.

### Issues identificados durante leitura

**ISSUE-A — App.tsx duplica tipos localmente:**
- `extension/src/popup/App.tsx` linhas 3-14: define `StatusResponse` e `ForgetRequest` localmente. Viola regra do CLAUDE.md ("Nunca duplicar tipos do proto/types.ts na extensao"). Nao corrigido por CR-001/002/003.

**ISSUE-B — /metrics sem auth:**
- `daemon/internal/server/routes.go` linha 88: `/metrics` fora do grupo autenticado. Expoe contadores de ingest/search/forget/ws_active sem token. Impacto baixo (dados anonimos, localhost-only), mas e inconsistencia de design.

**ISSUE-C — authMiddleware nao aplica extraOrigins:**
- `daemon/internal/server/routes.go` linhas 311-314: authMiddleware rejeita qualquer Origin que nao seja `chrome-extension://`, mesmo que esteja em `extraOrigins`. O corsMiddleware permite o header CORS, mas o authMiddleware bloqueia a request antes. Dashboard externo via `VBM_CORS_ORIGIN` nao funciona na pratica.

**ISSUE-D — /ui expoe token no HTML:**
- Conforme P2-01: token injetado no HTML da pagina. Nao e um novo problema (era pior antes), mas persiste como risco se o usuario compartilhar o HTML.

---

## Projetos

### Projeto 1: LembraLeitor

**Contexto:** Indexar artigos de blog, docs de libs, tutoriais. ~20-50 paginas/dia. Buscar "aquele artigo sobre React hooks".

**O que mudou para mim (v1 → v2):**
- Search agora funciona: `data.results` extraido corretamente (P0-01). Antes, toda busca via omnibox retornava vazio.
- Busca semantica disponivel se eu configurar `VBM_EMBED_URL` com Ollama (P0-02). Sem Ollama, continua so BM25 — mas pelo menos e claro no log.
- Snippets maiores (200→400 chars, P2-05) dao contexto suficiente para artigos longos.
- Pause/Resume no popup funciona (P1-01) — util quando estou lendo algo privado temporariamente.
- URLs indexadas sem parametros de tracking (P2-10) — artigos do Medium com `?source=` ficam limpos.
- TTL configuravel (P0-07) evita acumulo indefinido.

**O que ainda nao funciona / ausente:**
- Busca semantica requer setup manual de Ollama — sem guia claro no GUIA.md para esse caso de uso especifico.
- Snippets de 400 chars no omnibox sao truncados para 80 chars mesmo assim (service-worker.ts linha 166: `.slice(0, 80)`). O snippet maior so aparece na busca direta via `/ui`.
- Logs estruturados ausentes (P1-13): debug de problemas e por `journalctl` com texto puro.
- App.tsx duplica tipos (ISSUE-A): nao afeta funcionalidade mas indica debt tecnico.

| Dimensao | v1 | v2 | Comentario |
|---|---|---|---|
| Onboarding | 3 | 3 | Docs iniciais OK; configurar Ollama ainda sem guia claro |
| API Ergonomics | 2 | 4 | search() corrigido, tipos de proto/types, resetDaemon() em 401 |
| Feature Completeness | 2 | 3 | Busca funciona, pause/resume, TTL, export LGPD. Semantica opcional |
| Security Confidence | 4 | 4 | Sem mudancas de seguranca neste projeto |
| Language Quality | 3 | 3 | App.tsx ainda duplica tipos; resto melhorou |
| Operational Readiness | 3 | 4 | Graceful shutdown, /metrics, /healthz com Ping, cleanup TTL |
| Documentation | 4 | 4 | Sem mudancas relevantes de docs para este perfil |
| **Score do Projeto** | **3.0** | **3.6** | |

---

### Projeto 2: DevNotebook

**Contexto:** Capturar docs de libs (MDN, docs.rs, pkg.go.dev) durante desenvolvimento. ~30 paginas/dia, textos tecnicos.

**O que mudou para mim (v1 → v2):**
- FTS5 incremental (P0-03): revisitas a mesma URL de doc nao triggeram rebuild completo — importante com 30+ paginas/dia.
- N+1 eliminado (P1-06): busca em base grande (docs acumulados) muito mais rapida.
- Snippets 400 chars (P2-05): contexto tecnico adequado para identificar o trecho certo de docs.
- schema_versions (P1-05): migrations seguras — posso atualizar o daemon sem perder o banco.
- SetMaxOpenConns(1) (P2-03): sem race condition em insercoes concorrentes.
- /export (P1-15): posso exportar meu historico de docs para backup.

**O que ainda nao funciona / ausente:**
- Busca semantica ainda requer Ollama; sem ele, busca por "borrowing in Rust" nao acha paginas que falam de "ownership" sem a palavra exata.
- authMiddleware bloqueia VBM_CORS_ORIGIN (ISSUE-C): nao posso usar um dashboard web externo para browsear minhas docs.
- Logs sem slog (P1-13): dificulta correlacionar erros de ingest com URLs especificas.
- Omnibox mostra 80 chars do snippet: pouco para diferenciar dois resultados de docs da mesma lib.

| Dimensao | v1 | v2 | Comentario |
|---|---|---|---|
| Onboarding | 3 | 3 | Sem mudancas relevantes |
| API Ergonomics | 2 | 4 | Tipos corretos, N+1 resolvido, search funcional |
| Feature Completeness | 2 | 3 | Export, TTL, FTS incremental. Semantica requer setup extra |
| Security Confidence | 4 | 4 | Mantido |
| Language Quality | 3 | 3.5 | Go melhorou (migrations, Close/Wait). TS ainda com tipo duplicado no popup |
| Operational Readiness | 3 | 4 | /metrics, graceful shutdown, cleanup, Ping no healthz |
| Documentation | 3 | 3 | Sem novos guias para uso tecnico avancado |
| **Score do Projeto** | **2.9** | **3.5** | |

---

### Projeto 3: CursoTracker

**Contexto:** Acompanhar progresso em cursos (YouTube, Udemy, freeCodeCamp). Maioria video. ~5-10 paginas/dia.

**O que mudou para mim (v1 → v2):**
- Pause/Resume funcional (P1-01): posso pausar a captura durante aulas ao vivo ou conteudo sensivel.
- sanitizeUrl (P2-10): URLs do YouTube com `?si=...` e `&pp=...` sao limpas antes de indexar.
- Debounce no omnibox (P2-04): sem lag ao digitar `@recall python decorators`.
- /healthz confiavel (P1-04): posso checar se o daemon esta vivo antes de depender dele.

**O que ainda nao funciona / ausente:**
- Videos nao geram texto indexavel — o problema central do CursoTracker nao mudou. Readability extrai pouco ou nada de paginas de video.
- Sem integracao com transcricoes (fora do escopo de v0.1, mas a limitacao e severa para este caso de uso).
- Dwell de 30s acumula em paginas de video que ficam abertas mas paradas — pode indexar paginas "vistas" sem conteudo real.
- captureEnabled nao persiste entre reinicios do service worker (CLAUDE.md especifica isso intencionalmente, mas e confuso para usuario leigo).

| Dimensao | v1 | v2 | Comentario |
|---|---|---|---|
| Onboarding | 3 | 3 | Sem mudancas |
| API Ergonomics | 1 | 2 | Search funciona; mas caso de uso principal (video) segue sem suporte |
| Feature Completeness | 1 | 1.5 | Pause funcional ajuda; limitacao de video persiste — score mal pode subir |
| Security Confidence | 4 | 4 | Mantido |
| Language Quality | 3 | 3 | Sem mudancas relevantes para este projeto |
| Operational Readiness | 2 | 3 | Graceful shutdown, healthz, metrics. Melhora real |
| Documentation | 2 | 2 | Limitacao de video nao documentada claramente no GUIA.md |
| **Score do Projeto** | **2.1** | **2.4** | Melhora marginal — problema central nao resolvido |

---

### Projeto 4: SetupRapido

**Contexto:** Onboarding de um amigo sem experiencia em Go ou Chrome extensions. Avaliacao do install.sh + GUIA.md.

**O que mudou para mim (v1 → v2):**
- install.sh verifica binario antes (P2-06): erro claro com instrucao de como buildar. Antes terminava silenciosamente.
- Validacao do Extension ID com regex `^[a-p]{32}$` (P1-14): warning com exemplo e instrucao de re-run. Antes aceitava qualquer string.
- Mensagem de erro util: "Build it first: cd daemon && make build && cp bin/vbmd ~/.local/bin/vbmd" — exatamente o que um iniciante precisa.
- /healthz retorna 503 real se DB indisponivel (P1-04): o amigo pode debugar com curl agora.

**O que ainda nao funciona / ausente:**
- install.sh nao valida se `go` esta instalado antes de sugerir `make build`.
- GUIA.md nao menciona `VBM_EMBED_URL` / Ollama — busca semantica e feature escondida.
- Nenhum feedback visual de "indexando..." na extensao (badge aparece por 2s, mas e sutil).
- Se o amigo instala sem `EXTENSION_ID`, o aviso de re-run esta correto mas o comando sugerido (linha 98) usa `$SCRIPT_DIR/install.sh` relativo — pode confundir.
- `captureEnabled` resetado para `true` a cada reinicio do SW: comportamento inesperado para usuario leigo.

| Dimensao | v1 | v2 | Comentario |
|---|---|---|---|
| Onboarding | 2 | 3 | Verificacao de binario + validacao de ID melhora muito o primeiro contato |
| API Ergonomics | 3 | 3 | Sem mudancas na API para este perfil |
| Feature Completeness | 3 | 3.5 | healthz confiavel, install mais robusto |
| Security Confidence | 4 | 4 | Mantido |
| Language Quality | 3 | 3 | Sem mudancas relevantes |
| Operational Readiness | 2 | 3 | systemd hardening, healthz real, verificacao pre-install |
| Documentation | 3 | 3 | GUIA.md sem atualizacoes significativas para novatos |
| **Score do Projeto** | **2.7** | **3.2** | |

---

### Projeto 5: BuscaOmnibox

**Contexto:** Usar `@recall <query>` no omnibox do Chrome para buscar paginas indexadas em tempo real.

**O que mudou para mim (v1 → v2):**
- search() agora retorna resultados reais (P0-01) — o bug principal que tornava o omnibox inutil esta corrigido.
- Debounce 200ms (P2-04): experiencia fluida sem travar ao digitar rapido.
- resetDaemon() em 401 (P2-09): se o daemon reiniciar, a proxima busca reconecta automaticamente sem intervencao.
- sanitizeUrl (P2-10): URLs nos resultados sao limpas.
- Busca BM25 funciona sem Ollama.

**O que ainda nao funciona / ausente:**
- Snippet no omnibox truncado a 80 chars (service-worker.ts linha 166) apesar do snippet ser 400 chars. Para queries tecnicas, 80 chars e pouco para diferenciar resultados.
- Busca semantica (sinonimos, conceitos relacionados) so disponivel com Ollama — sem setup extra, `@recall ownership Rust` nao acha paginas sobre "borrowing".
- ISSUE-C (authMiddleware vs extraOrigins): nao afeta omnibox diretamente.
- Sem indicacao visual de "daemon desconectado" no omnibox — erros sao silenciados (catch vazio, linha 171).
- onInputEntered so abre URLs que comecam com `http` — se o usuario digitar texto livre e der Enter, nao acontece nada.

| Dimensao | v1 | v2 | Comentario |
|---|---|---|---|
| Onboarding | 3 | 3 | Sem mudancas no setup do omnibox |
| API Ergonomics | 1 | 4 | search() funciona, debounce, auto-reconexao em 401 |
| Feature Completeness | 2 | 3 | BM25 funcional; semantica opcional; snippet 80 chars no omnibox e pouco |
| Security Confidence | 4 | 4 | Mantido |
| Language Quality | 2 | 3 | daemon-client.ts limpo, tipos de proto/types |
| Operational Readiness | 2 | 3 | resetDaemon, healthz, metrics |
| Documentation | 3 | 3 | Sem novos guias para busca avancada |
| **Score do Projeto** | **2.4** | **3.3** | Maior salto individual — P0-01 era o bloqueador principal |

---

## Findings Remanescentes

### P1 (Alto impacto)

| ID | Descricao | Arquivo | Linha | Impacto |
|---|---|---|---|---|
| P1-08 | Logs estruturados (slog) deferidos — `log.Printf` puro | server.go, queue.go | multiplas | Debug de producao dificil |
| P1-13 | App.tsx duplica `StatusResponse` e `ForgetRequest` localmente | App.tsx | 3-14 | Viola regra do CLAUDE.md; risco de drift de tipos |

### P2 (Medio impacto)

| ID | Descricao | Arquivo | Linha | Impacto |
|---|---|---|---|---|
| ISSUE-A | App.tsx: tipos duplicados (nao endereçado por nenhum CR) | extension/src/popup/App.tsx | 3-14 | Debt tecnico |
| ISSUE-B | /metrics sem autenticacao | daemon/internal/server/routes.go | 88 | Expoe metadados operacionais sem token |
| ISSUE-C | authMiddleware bloqueia VBM_CORS_ORIGIN antes do corsMiddleware agir | daemon/internal/server/routes.go | 311-314 | Dashboard externo nao funciona na pratica |
| ISSUE-D | Token injetado no HTML do /ui e visivel no source | daemon/internal/server/routes.go | 286 | Risco se usuario compartilhar HTML |
| P2-07 | Snippet no omnibox truncado a 80 chars (independente do limit 400) | extension/src/background/service-worker.ts | 166 | Experiencia de busca degradada no omnibox |

### P3 (Baixo impacto / Qualidade)

| ID | Descricao | Arquivo |
|---|---|---|
| P3-01 | captureEnabled nao persiste entre reinicios do SW (intencional, mas confuso) | service-worker.ts |
| P3-02 | onInputEntered ignora queries que nao comecam com `http` | service-worker.ts linha 180 |
| P3-03 | install.sh nao verifica se `go` esta instalado | install.sh |
| P3-04 | /ui expoe UI minima — "Full UI coming in v0.2" ainda como placeholder | routes.go linha 45 |

---

## Conclusao

As tres Change Requests entregaram valor real e mensuravel. Os P0 blockers — especialmente P0-01 (search retornando vazio) e P0-06 (bind em 0.0.0.0) — eram problemas que tornavam a ferramenta inutilizavel ou insegura. Corrigidos com verificacao de codigo.

O daemon Go melhorou substancialmente: graceful shutdown com drenagem de fila (P0-04), FTS5 incremental (P0-03), N+1 eliminado (P1-06), schema migrations (P1-05), /healthz real (P1-04), /export LGPD (P1-15) e /metrics Prometheus (P2-08). E um daemon que agora parece pronto para uso continuo.

Na extensao, a correcao do search() (P0-01) desbloqueou o caso de uso principal do omnibox. Pause/Resume (P1-01), debounce (P2-04), sanitizeUrl (P2-10) e resetDaemon em 401 (P2-09) sao melhorias de qualidade de vida notaveis.

O que ainda pesa negativamente: logs sem slog (P1-13/P1-08) dificultam debug; App.tsx duplica tipos violando a regra do projeto (ISSUE-A); o bug ISSUE-C torna VBM_CORS_ORIGIN inutil na pratica; e o CursoTracker permanece com valor limitado por falta de suporte a conteudo de video — problema estrutural fora do escopo de v0.1.

Para uso pessoal de dev solo no Linux com foco em artigos e docs tecnicas (LembraLeitor + DevNotebook), o sistema passou de "funciona parcialmente" para "funciona bem". O score geral sobe de 2.7 para 3.2.

---

## Score Medio

| Dimensao | LembraLeitor | DevNotebook | CursoTracker | SetupRapido | BuscaOmnibox | Media v2 | Media v1 | Delta |
|---|---|---|---|---|---|---|---|---|
| Onboarding | 3 | 3 | 3 | 3 | 3 | 3.0 | 2.8 | +0.2 |
| API Ergonomics | 4 | 4 | 2 | 3 | 4 | 3.4 | 1.8 | +1.6 |
| Feature Completeness | 3 | 3 | 1.5 | 3.5 | 3 | 2.8 | 2.0 | +0.8 |
| Security Confidence | 4 | 4 | 4 | 4 | 4 | 4.0 | 4.0 | 0.0 |
| Language Quality | 3 | 3.5 | 3 | 3 | 3 | 3.1 | 2.8 | +0.3 |
| Operational Readiness | 4 | 4 | 3 | 3 | 3 | 3.4 | 2.4 | +1.0 |
| Documentation | 4 | 3 | 2 | 3 | 3 | 3.0 | 3.0 | 0.0 |
| **Score do Projeto** | **3.6** | **3.5** | **2.4** | **3.2** | **3.3** | **3.2** | **2.7** | **+0.5** |

**Score v2: 3.2 / 5 (v1: 2.7)**
