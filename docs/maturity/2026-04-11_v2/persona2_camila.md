# Re-avaliacao v2: Camila Santos — Startup Dev

## Perfil

Camila Santos, desenvolvedora fullstack, 2 anos de experiencia em TypeScript/React/Next.js. Time de 3 pesquisadores/devs. Avalia o Vector Bookmark sob a otica de quem precisa integrar a API do daemon em um painel Next.js (`localhost:3000`), monitorar status em tempo real via WebSocket, e garantir que a extensao Chrome funcione com tipagem correta e sem surpresas em producao.

Score v1 de referencia: **2.8 / 5**

---

## O que verifiquei no codigo

Cada claim abaixo foi verificada com leitura direta do source code.

### CR-001 — P0 Blockers

**P0-01 — search() extrai `data.results`; tipos importados de proto/types.ts**
- `extension/src/background/daemon-client.ts` linha 1–7: imports de `IngestRequest`, `SearchResult`, `SearchResponse`, `StatusResponse`, `ForgetRequest` vindos de `'../../../proto/types'`. Zero duplicatas.
- Linha 55–56: `const data = (await res.json()) as SearchResponse; return data.results;` — correto.

**P0-02 — HttpEmbedder em embed/http.go**
- `daemon/internal/embed/http.go` criado, linhas 17–77. `NewHttpEmbedder(url, model)`, timeout 10s, formato Ollama (`prompt`/`embedding`). `Version()` retorna `"http-v0"`.
- `daemon/internal/server/server.go` linhas 35–42: seleciona embedder baseado em `VBM_EMBED_URL`. Stub como fallback com warning no log.

**P0-03 — FTS5 incremental**
- `daemon/internal/store/sqlite.go` linhas 235–239: `INSERT OR IGNORE` + `RowsAffected()` — so insere na FTS quando chunk e novo (`n > 0`). Sem rebuild completo em ingest.

**P0-04 — Close()/Wait() na queue; drain no shutdown**
- `daemon/internal/queue/queue.go` linhas 36–43: `Close()` fecha o canal; `Wait()` usa `sync.WaitGroup`.
- `daemon/internal/server/server.go` linhas 122–141: goroutine de shutdown chama `q.Close()`, lanca `q.Wait()` em goroutine separada, espera com timeout de 30s via `context.WithTimeout`.

**P0-05 — Skip dense search quando vetor zero**
- `daemon/internal/store/sqlite.go` linhas 300–308: loop detecta vetor zero (`isStub`). Bloco `if !isStub { ... }` (linhas 308–343) pula completamente o full scan de embeddings.

**P0-06 — Bind em 127.0.0.1**
- `server.go` linhas 55–64: `listenAddr = "127.0.0.1:0"` por padrao. `VBM_BIND` so aceito com warning explicito. Correto.

**P0-07 — Cleanup(ttlDays)**
- `sqlite.go` linhas 493–509: `Cleanup(ttlDays int)` com cutoff em UnixMilli.
- `server.go` linhas 82–102: goroutine a cada 24h via `time.NewTicker(24 * time.Hour)`, ativada por `VBM_TTL_DAYS`.

### CR-002 — P1 findings

**P1-01 — captureEnabled modulo-level + popup_set_capture**
- `service-worker.ts` linha 19: `let captureEnabled = true;` no escopo do modulo.
- Linhas 138–141: handler `popup_set_capture` atualiza a variavel e retorna estado.
- `App.tsx` linhas 40–51: `handleToggleCapture()` envia `popup_set_capture`, atualiza estado local. Botao Pause/Resume funcional (linhas 171–173).

**P1-02 — daemonPort no popup_status**
- `service-worker.ts` linhas 124–133: resposta de `popup_status` inclui `daemonPort: daemonState.port`.
- `App.tsx` linhas 196–207: `status.daemonPort` usado para construir URL do `/ui` sem hardcode.

**P1-03 — Forget() deleta da tabela queue tambem**
- `sqlite.go` linhas 442–488: `Forget()` usa transacao e deleta de `pages` E `queue` para os tres tipos (url, domain, timerange).

**P1-04 — /healthz chama s.Ping()**
- `routes.go` linhas 77–85: `/healthz` chama `s.Ping()`, retorna 503 + JSON de erro se falhar.
- `sqlite.go` linhas 523–526: `Ping()` faz `SELECT 1`.

**P1-05 — schema_versions**
- `sqlite.go` linhas 136–172: `migrate()` cria `schema_versions`, consulta `MAX(version)`, aplica migrations em ordem. Estrutura pronta para versoes futuras.

**P1-06 — N+1 eliminado com JOIN**
- `sqlite.go` linhas 376–405: unico JOIN `chunks c JOIN pages p ON c.page_id = p.id WHERE c.id IN (?,...)`. Sem loop de queries individuais.

**P1-07 — VBM_CORS_ORIGIN**
- `server.go` linhas 104–112: parseia `VBM_CORS_ORIGIN` (separado por virgula) em `extraOrigins`.
- `routes.go` linhas 330–353: `corsMiddleware(extraOrigins)` permite origens adicionais alem de `chrome-extension://`.

**P1-09 — Denylist BR** — nao verificada diretamente nesta leitura (arquivo `denylist.ts` nao lido), mas consta como implementada no CR-002. Score mantido sem alteracao por cautela.

**P1-10 — WebSocket ping/pong + write deadlines**
- `routes.go` linhas 231–282: `readDeadline=60s`, `writeDeadline=10s`, `pingInterval=30s`. Goroutine de leitura dedicada (linhas 243–250). `SetPongHandler` renova deadline (linhas 237–239).

**P1-11 — systemd hardening**
- `vbmd.service` linhas 11–14: `NoNewPrivileges=true`, `ProtectSystem=strict`, `ProtectHome=read-only`, `ReadWritePaths=%h/.local/share/vbm`.

**P1-12 — SessionPath() retorna (string, error)**
- Nao lido diretamente, mas `server.go` linha 75 ja trata o erro: `if sessionPath, err := nm.SessionPath(); err == nil { defer os.Remove(sessionPath) }`.

**P1-14 — Regex de Extension ID**
- `install.sh` linha 49: `grep -qE '^[a-p]{32}$'`. Warning sem abortar se invalido (comportamento correto para IDe nao preenchido).

**P1-15 — /export endpoint**
- `routes.go` linhas 204–219: endpoint `/export` protegido por auth.
- `sqlite.go` linhas 569–617: `Export()` com LEFT JOIN, preserva ordem, retorna `[]ExportPage` com chunks.

### CR-003 — P2 findings

**P2-01 — Token real injetado em /ui**
- `routes.go` linha 286: `uiWithToken := strings.ReplaceAll(uiHTML, "REPLACE_TOKEN", token)`. Token real injetado no build do handler, nao em runtime por request.
- **OBSERVACAO**: o `uiHTML` ainda contem `REPLACE_TOKEN` como literal na string da linha 55 (`routes.go`). A substituicao ocorre uma vez na inicializacao do router (linha 286), o que e correto — mas o HTML hardcoded no source e feio e qualquer log do template exporia o placeholder. Nao e um bug de seguranca, mas e tecnicamente fragil.

**P2-02 — AddQueueItem/RemoveQueueItem wired**
- `routes.go` linhas 111–113: `/ingest` chama `s.AddQueueItem(ireq)` apos enfileirar.
- `queue.go` linhas 52–55: worker chama `s.RemoveQueueItem(req.URL)` apos ingest bem-sucedido.

**P2-03 — db.SetMaxOpenConns(1)**
- `sqlite.go` linha 118: `db.SetMaxOpenConns(1)` explicito.

**P2-04 — Debounce 200ms no omnibox**
- `service-worker.ts` linhas 160–174: `omniboxTimer` com `setTimeout(..., 200)`. Correto.

**P2-05 — Snippet 400 chars**
- `sqlite.go` linhas 413–415: `if len(snippet) > 400 { snippet = snippet[:400] }`.

**P2-06 — install.sh verifica binario**
- `install.sh` linhas 7–17: checa existencia do binario antes de continuar. Mensagem de erro clara com instrucoes.

**P2-08 — /metrics Prometheus sem lib externa**
- `metrics.go` completo: contadores `ingestTotal`, `searchTotal`, `forgetTotal`, `wsActive` (atomic.Int64); gauges `pages_indexed` e `queue_pending` via `GetStatus()`. Content-Type correto (`text/plain; version=0.0.4`).

**P2-09 — resetDaemon() em native-bridge.ts**
- `native-bridge.ts` linhas 55–58: `resetDaemon()` zera port e token.
- `daemon-client.ts` linhas 23–27: `checkResponse()` chama `resetDaemon()` em 401.

**P2-10 — sanitizeUrl() com 18 parametros**
- `service-worker.ts` linhas 25–54: lista com `utm_*`, `fbclid`, `gclid`, `msclkid`, `ref`, `source`, `token`, `access_token`, `api_key`, `key`, `secret`, `session`, `sid`, `sessionid`, `session_id` — 15 parametros visiveis (nao 18 conforme descricao do CR, mas cobre os casos criticos).

### Ainda nao implementado

- **P1-13**: Logs estruturados (slog) — ainda usa `log.Printf` em todo o daemon. Deferido intencionalmente.
- **App.tsx duplica tipos**: `StatusResponse` (linha 3–9) e `ForgetRequest` (linha 11–14) em `App.tsx` nao importam de `proto/types.ts`. Violacao da regra "nunca duplicar tipos do proto/types.ts na extensao". `proto/types.ts` exporta `StatusResponse` (linha 43) e `ForgetRequest` (linha 36) — App.tsx poderia importar mas nao o faz. Finding remanescente.
- **authMiddleware bloqueia Next.js dashboard**: `routes.go` linha 311–313 verifica `Origin` header — se nao comecar com `chrome-extension://`, retorna 401 antes mesmo de checar o token. Isso significa que `VBM_CORS_ORIGIN=http://localhost:3000` nao e suficiente para acessar a API de um painel Next.js via browser, pois o `Origin: http://localhost:3000` sera rejeitado no `authMiddleware` antes de chegar ao `corsMiddleware`. Bug critico para o caso de uso APIIntegration.
- **`/metrics` sem autenticacao**: exposto sem auth (linha 88 de `routes.go`). Em ambiente de equipe pode vazar metadados de uso.

---

## Projetos

### 1. ResearchSync

**O que mudou para mim:**
- `VBM_CORS_ORIGIN` permite que meu painel Next.js receba os headers CORS corretos.
- `/export` endpoint viabiliza portabilidade de dados entre membros da equipe (LGPD Art. 18 como bonus).
- `/metrics` em formato Prometheus permite integrar Grafana sem agente extra.
- Schema migrations versionadas dao mais confianca para atualizar o daemon sem perder dados.

**O que ainda nao funciona:**
- O `authMiddleware` rejeita requests com `Origin: http://localhost:3000` antes de checar token (bug identificado acima). Na pratica, meu painel Next.js no browser nao consegue chamar a API mesmo com `VBM_CORS_ORIGIN` configurado.
- Sem multi-usuario: cada dev roda daemon local separado, sem sincronizacao. Esperado para v0.1 mas limita o valor de "memoria compartilhada".
- Logs ainda sao `log.Printf` sem estrutura — dificulta correlacionar eventos entre instancias.

| Dimensao | v1 | v2 | Comentario |
|---|---|---|---|
| Onboarding | 3 | 3 | install.sh melhorou (verifica binario, valida ID), mas bug de CORS/auth bloqueia onboarding do painel |
| API Ergonomics | 3 | 3.5 | /export e /metrics sao addicoes solidas; CORS bug penaliza |
| Feature Completeness | 1 | 2.5 | export, metrics, TTL, pause — avanco real; multi-user e CORS/auth ainda ausentes |
| Security Confidence | 4 | 4 | systemd hardening, bind 127.0.0.1 confirmados; /metrics sem auth e leve regressao |
| Language Quality | 4 | 4 | Go bem idiomatico; sem melhora de log estruturado |
| Operational Readiness | 3 | 4 | graceful shutdown com drain 30s, metrics Prometheus, TTL cleanup, schema versioning |
| Documentation | 3 | 3 | sem mudancas observadas nos docs |
| **Score** | **3.0** | **3.4** | |

---

### 2. APIIntegration

**O que mudou para mim:**
- `VBM_CORS_ORIGIN` foi implementado — intencionalmente pensado para `localhost:3000`.
- Token injetado em `/ui` (nao mais placeholder literal).
- `/metrics` e `/export` sao novos endpoints prontos para consumir.
- `resetDaemon()` + `checkResponse()` tratam rotacao de token automaticamente.

**O que ainda nao funciona:**
- **Bug critico**: `authMiddleware` (routes.go linha 311–313) rejeita qualquer `Origin` que nao comece com `chrome-extension://` com 401, antes de checar o token. O `corsMiddleware` esta aplicado DEPOIS do `authMiddleware` no grupo (linhas 91–295 de routes.go), entao requests de `http://localhost:3000` sao bloqueados independente de `VBM_CORS_ORIGIN`. Para funcionar seria necessario ou inverter a ordem, ou mover a validacao de Origin para o corsMiddleware.
- Sem SDK cliente ou OpenAPI spec — ainda preciso escrever o cliente manualmente.
- `proto/types.ts` existe mas nao ha pacote npm publicado — preciso copiar o arquivo manualmente ou usar path alias.

| Dimensao | v1 | v2 | Comentario |
|---|---|---|---|
| Onboarding | 2 | 2.5 | VBM_CORS_ORIGIN documentado, mas bug de auth bloqueia uso real |
| API Ergonomics | 3 | 3.5 | tipos em proto/types.ts importaveis, search() corrigido, /export novo |
| Feature Completeness | 2 | 3 | export, metrics, WS ping/pong — mas CORS/auth bug e bloqueante |
| Security Confidence | 3 | 3 | bug de CORS/auth e double-edged: bloqueia legitimos tambem |
| Language Quality | 3 | 3.5 | daemon-client.ts sem duplicatas, checkResponse() com reset |
| Operational Readiness | 2 | 3.5 | /metrics Prometheus, graceful shutdown, resetDaemon on 401 |
| Documentation | 2 | 2 | sem mudanca; falta exemplo de integracao Next.js |
| **Score** | **2.4** | **3.0** | |

---

### 3. RealTimeMonitor

**O que mudou para mim:**
- WebSocket agora tem ping/pong, write deadlines e goroutine de leitura dedicada — conexao e estavel.
- `/metrics` em Prometheus text format: `vbm_pages_indexed`, `vbm_queue_pending`, `vbm_ws_connections_active` — posso usar Prometheus + Grafana sem codigo extra.
- `GetStatus()` agora conta `queue WHERE status = 'pending'` com dados reais (P2-02 wired).
- `wsActive` gauge reflete conexoes ativas em tempo real.

**O que ainda nao funciona:**
- WS so emite `status` (indexed/pending) a cada 5s. Sem eventos de ingest individual, erro de queue, ou alerta de "queue cheia" — o monitor e basico.
- Sem mecanismo de alerta push (ex: threshold de fila cheia notifica via WS).
- `/metrics` sem auth — em contexto de equipe, qualquer processo local pode scrape.

| Dimensao | v1 | v2 | Comentario |
|---|---|---|---|
| Onboarding | 3 | 3.5 | /metrics e /ws agora documentaveis com comportamento previsivel |
| API Ergonomics | 3 | 3.5 | WS estavel, metrics Prometheus, GetStatus() preciso |
| Feature Completeness | 2 | 3 | metrics reais, pending count preciso; alertas ausentes |
| Security Confidence | 4 | 3.5 | /metrics sem auth e regressao leve para cenario de equipe |
| Language Quality | 3 | 3.5 | atomic counters idiomaticos em Go, sem lib externa |
| Operational Readiness | 2 | 4 | Prometheus scraping, WS resiliente, graceful drain |
| Documentation | 2 | 2 | sem exemplos de dashboard/alerting |
| **Score** | **2.7** | **3.3** | |

---

### 4. SmartSearch

**O que mudou para mim:**
- `HttpEmbedder` com Ollama — posso agora ter busca semantica real configurando `VBM_EMBED_URL`.
- Deteccao de vetor zero (isStub) evita full scan inutil — performance correta em stub mode.
- Snippet aumentado para 400 chars — contexto real para docs tecnicos.
- FTS5 incremental — ingest mais rapido, sem rebuild bloqueante.
- `sanitizeUrl()` remove parametros de rastreamento — URLs mais limpas no indice.

**O que ainda nao funciona:**
- `HttpEmbedder.Version()` retorna `"http-v0"` fixo — se trocar de modelo, chunks antigos nao sao reindexados automaticamente (sem invalidacao por model_ver).
- Cosine search ainda e brute-force O(N) sobre todos os chunks — sem ANN index (esperado para v0.1).
- Nao ha endpoint de sugestao de configuracao do Ollama nos docs.

| Dimensao | v1 | v2 | Comentario |
|---|---|---|---|
| Onboarding | 3 | 3.5 | VBM_EMBED_URL documentado, stub com warning claro |
| API Ergonomics | 4 | 4 | RRF + hibrido e solido; snippet 400 chars melhora UX |
| Feature Completeness | 2 | 3.5 | embedder real configuravel e o maior avanco desta versao |
| Security Confidence | 4 | 4 | sem mudanca relevante |
| Language Quality | 3 | 3.5 | isStub detection elegante, incremental FTS correto |
| Operational Readiness | 2 | 3 | TTL cleanup, ingest performatico; sem reindex por model_ver |
| Documentation | 2 | 2.5 | falta guia de configuracao Ollama |
| **Score** | **2.9** | **3.4** | |

---

### 5. ExtensionDX

**O que mudou para mim:**
- `daemon-client.ts` 100% tipado com imports de `proto/types.ts` — zero duplicatas no arquivo.
- `resetDaemon()` + `checkResponse()` tratam 401 automaticamente — DX de recuperacao de erro.
- `sanitizeUrl()` e `captureEnabled` bem encapsulados no SW.
- Debounce no omnibox (200ms) — sem requests a cada keystroke.

**O que ainda nao funciona:**
- `App.tsx` duplica `StatusResponse` e `ForgetRequest` localmente (linhas 3–14) sem importar de `proto/types.ts`. Violacao direta da regra do CLAUDE.md ("nunca duplicar tipos do proto/types.ts na extensao"). Se o tipo mudar no proto, o popup quebra silenciosamente.
- Sem testes unitarios em nenhum dos dois lados.
- `proto/types.ts` nao tem `daemonPort` nem `captureEnabled` em `StatusResponse` — o tipo em `App.tsx` e mais completo que o canonico, o que e outro sinal de desalinhamento.

| Dimensao | v1 | v2 | Comentario |
|---|---|---|---|
| Onboarding | 4 | 4 | sem mudanca estrutural no DX de setup |
| API Ergonomics | 3 | 4 | daemon-client.ts limpo, tipos corretos, resetDaemon elegante |
| Feature Completeness | 2 | 3 | Pause funcional, forget wired, debounce — avanco real |
| Security Confidence | 4 | 4 | sem regressao; resetDaemon on 401 e bom |
| Language Quality | 2 | 3 | daemon-client.ts excelente; App.tsx ainda duplica tipos |
| Operational Readiness | 2 | 2.5 | sem testes; debounce melhora estabilidade do omnibox |
| Documentation | 3 | 3 | sem mudanca nos docs de extensao |
| **Score** | **2.9** | **3.4** | |

---

## Findings Remanescentes

### P0 — Bloqueante

Nenhum P0 remanescente da lista original. Porem identificado novo issue:

**P0-NEW-01 — authMiddleware bloqueia VBM_CORS_ORIGIN**
- Arquivo: `daemon/internal/server/routes.go` linhas 310–314
- `authMiddleware` verifica `Origin != chrome-extension://` e retorna 401. O `corsMiddleware` e aplicado DEPOIS (linha 93) no mesmo grupo. Qualquer cliente browser em `http://localhost:3000` e rejeitado antes de ser autenticado, tornando `VBM_CORS_ORIGIN` inutilizavel via browser.
- Fix: mover verificacao de origin para `corsMiddleware` ou adicionar os `extraOrigins` como origens validas no `authMiddleware`.

### P1 — Importante

**P1-NEW-01 — App.tsx duplica StatusResponse e ForgetRequest**
- Arquivo: `extension/src/popup/App.tsx` linhas 3–14
- `StatusResponse` local nao tem `version` (tem em proto/types.ts linha 46) mas tem `daemonPort` e `captureEnabled` (ausentes no proto). Desalinhamento silencioso.
- Fix: adicionar `daemonPort` e `captureEnabled` em `proto/types.ts > StatusResponse` e importar de la.

**P1-13 (original, deferido) — Logs nao estruturados**
- Todo o daemon usa `log.Printf`. Dificulta correlacao e filtragem em producao.

### P2 — Menor

**P2-NEW-01 — /metrics sem autenticacao**
- `routes.go` linha 88: `/metrics` esta fora do grupo autenticado. Em ambiente multi-usuario local, qualquer processo pode scrape contadores e gauges. Considerar auth opcional ou bind exclusivo para Prometheus.

**P2-NEW-02 — sanitizeUrl() lista 15 params, CR diz 18**
- `service-worker.ts` linhas 28–49: contei 15 parametros distintos. Sem impacto funcional critico mas discrepancia com documentacao.

**P2-NEW-03 — HttpEmbedder.Version() fixo como "http-v0"**
- `embed/http.go` linha 41: sem incluir o model name na versao. Troca de modelo Ollama nao invalida chunks antigos.

---

## Conclusao

A v2 representa um salto real em relacao a v1. Os P0s foram todos corrigidos corretamente e verificados no codigo. O graceful shutdown (P0-04), FTS5 incremental (P0-03), deteccao de vetor zero (P0-05) e bind em loopback (P0-06) sao solidos. O `HttpEmbedder` finalmente torna a busca semantica real utilizavel. O `/metrics` Prometheus e o `/export` LGPD sao adicoes que mostram maturidade operacional.

O maior problema remanescente — e que nao estava na lista original dos CRs — e o bug de CORS/auth que torna `VBM_CORS_ORIGIN` inutilizavel para clientes browser. Para o meu caso de uso principal (painel Next.js em localhost:3000), isso ainda e bloqueante. A duplicacao de tipos em `App.tsx` e secundaria mas e tecnicamente uma violacao das proprias regras do projeto.

Para a v0.1, o produto esta funcional para o caso de uso principal (extensao Chrome + daemon local). Para o caso de uso de equipe com painel externo, precisa do fix de CORS/auth.

---

## Score Medio

| Projeto | v1 | v2 | Delta |
|---|---|---|---|
| ResearchSync | 3.0 | 3.4 | +0.4 |
| APIIntegration | 2.4 | 3.0 | +0.6 |
| RealTimeMonitor | 2.7 | 3.3 | +0.6 |
| SmartSearch | 2.9 | 3.4 | +0.5 |
| ExtensionDX | 2.9 | 3.4 | +0.5 |
| **Media** | **2.8** | **3.3** | **+0.5** |

**Score v2: 3.3 / 5 (v1: 2.8)**
