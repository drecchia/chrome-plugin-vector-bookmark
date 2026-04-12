# Re-avaliacao v2: Renata Costa — Platform/DevOps Engineer

## Perfil

**Nome:** Renata Costa
**Especialidade:** Engenheira de Plataforma, 7 anos de experiencia
**Foco:** Operabilidade, graceful shutdown, observabilidade, comportamento sob carga
**Projetos avaliados:** SystemdReliability, PerformanceUnderLoad, Observability, NativeMessagingReliability, DatabaseIntegrity

---

## O que verifiquei no codigo

Leitura completa dos seguintes arquivos com verificacao de linha:

| Arquivo | Linhas | Mudancas verificadas |
|---|---|---|
| `daemon/internal/queue/queue.go` | 57 | Close()/Wait() com WaitGroup (L36-43), RemoveQueueItem pos-ingest (L53-55) |
| `daemon/internal/server/server.go` | 144 | HttpEmbedder selection (L35-42), VBM_PORT loopback bind (L56-64), queue drain 30s timeout (L130-140), VBM_TTL_DAYS cleanup goroutine (L82-101) |
| `daemon/internal/server/routes.go` | 354 | /healthz com s.Ping() (L77-85), /metrics sem auth (L88), /ws ping/pong + deadlines (L231-282), /export (L204-219), AddQueueItem em /ingest (L111-113), token injetado em /ui (L286-294) |
| `daemon/internal/server/metrics.go` | 43 | serverMetrics com atomic counters (L13-17), Prometheus text format (L20-43) |
| `daemon/internal/store/sqlite.go` | 617 | SetMaxOpenConns(1) (L118), migrate() incremental (L136-172), Forget() deleta queue (L451-488), Cleanup() (L493-509), Ping() (L523-526), Export() (L569-617), AddQueueItem/RemoveQueueItem (L546-566) |
| `daemon/internal/nm/host.go` | 123 | SessionPath() retorna (string, error) (L32-38), erro propagado em readSession() (L40-51) |
| `daemon/install/vbmd.service` | 21 | NoNewPrivileges=true, ProtectSystem=strict, ProtectHome=read-only, ReadWritePaths (L10-14) — After=network.target removido |
| `daemon/install/install.sh` | 106 | prereq check binario (L7-17), Extension ID validado `^[a-p]{32}$` (L49-56) |
| `extension/src/background/native-bridge.ts` | 67 | resetDaemon() (L55-58), getDaemonBase/getAuthHeader helpers |
| `extension/src/background/daemon-client.ts` | 89 | search() retorna data.results (L54-56), checkResponse() chama resetDaemon() em 401 (L24-27) |
| `extension/src/background/service-worker.ts` | 190 | captureEnabled modulo-level (L19), popup_set_capture handler (L138-141), sanitizeUrl() 18 params (L25-53), debounce 200ms (L160-174), daemonPort no popup_status (L128-130) |
| `daemon/internal/embed/http.go` | 78 | HttpEmbedder com Ollama-compat (L17-37), Timeout 10s (L33-36) |

---

## Projeto 1: SystemdReliability

### O que mudou para mim

**P0-04 (queue drain):** Confirmado em `server.go` L122-141. Shutdown HTTP primeiro (5s), depois `q.Close()` + `q.Wait()` com timeout de 30s. Goroutine separada para nao bloquear `srv.Serve`. Correto.

**P1-11 (systemd hardening):** Confirmado em `vbmd.service` L10-14. `NoNewPrivileges=true`, `ProtectSystem=strict`, `ProtectHome=read-only`, `ReadWritePaths=%h/.local/share/vbm`. `After=network.target` removido (correto — daemon nao precisa de rede). `StandardOutput=journal` presente (L16-17).

**P0-06 (bind loopback):** Confirmado em `server.go` L56-64. Default `127.0.0.1:0`. `VBM_BIND` so ativa com warning explicito. Correto.

**P2-06 (prereq check):** Confirmado em `install.sh` L7-17. Verifica existencia do binario antes de qualquer operacao.

### O que ainda nao funciona ou esta ausente

- **`Restart=on-failure` sem `RestartPreventExitStatus`**: se o daemon falhar por erro de configuracao (ex: porta ja em uso), vai reiniciar em loop infinito sem backoff exponencial. `RestartSec=3s` fixo e muito simples para producao.
- **Sem `TimeoutStopSec`**: se o drain de 30s nao concluir (timeout interno), o systemd pode matar o processo antes. O timeout interno do drain e de 30s mas o systemd nao tem instrucao de esperar — usa o default de 90s, o que e OK, mas nao e documentado.
- **Logs estruturados (P1-13):** ainda `log.Printf` puro em todo o daemon. Sem `slog`. Journald recebe texto nao estruturado.
- **Sem `WatchdogSec`/`Type=notify`**: impossivel usar `sd_notify` para informar readiness ao systemd. `systemctl --user status vbmd` mostra `active (running)` imediatamente mesmo se o bind falhou.

| Dimensao | v1 | v2 | Comentario |
|---|---|---|---|
| Onboarding | 4 | 4 | Prereq check e install melhorado, sem regressao |
| API Ergonomics | 3 | 3 | Sem mudancas na API de controle |
| Feature Completeness | 2 | 4 | Queue drain + hardening implementados e verificados |
| Security Confidence | 3 | 4 | ProtectSystem=strict + NoNewPrivileges confirmados |
| Language Quality | 3 | 3 | Goroutine de shutdown correta; logs ainda sem slog |
| Operational Readiness | 2 | 3 | Drain funciona; falta sd_notify e TimeoutStopSec explicito |
| Documentation | 2 | 2 | Comportamento do drain nao documentado no service file |
| **Score** | **2.7** | **3.3** | Salto real gracias ao P0-04 e P1-11 |

---

## Projeto 2: PerformanceUnderLoad

### O que mudou para mim

**P0-05 (isStub bypass):** Confirmado em `sqlite.go` L299-307. Loop sobre `queryVec` detecta zeros e pula full table scan. Efetivo para o caso stub (producao com Ollama nao e afetado).

**P1-06 (N+1 eliminado):** Confirmado em `sqlite.go` L365-404. JOIN unico com `WHERE c.id IN (?,...)` — sem loop de queries individuais.

**P2-05 (snippet 400 chars):** Confirmado em `sqlite.go` L413-415.

**P2-04 (debounce omnibox):** Confirmado em `service-worker.ts` L160-174. 200ms.

**P2-10 (sanitizeUrl):** Confirmado em `service-worker.ts` L25-53. 18 parametros removidos.

**Queue single-worker:** `queue.go` L45-56 confirma worker unico. Para 30-50 paginas/dia (avg 1 pagina/30min) e absolutamente adequado. Nao e issue para o caso de uso declarado.

### O que ainda nao funciona ou esta ausente

- **Full table scan ainda presente para embedder real:** quando `VBM_EMBED_URL` e configurado, `sqlite.go` L316 faz `SELECT id, text, page_id, embedding FROM chunks` — sem filtro, sem limit. Com 10k chunks isso e O(N) na memoria. Nao ha ANN index. O P0-05 foi bypass, nao solucao.
- **Sem benchmark/teste de carga:** nao ha testes que validem latencia de busca com volume real. Aceitavel para POC mas e um gap operacional.
- **`HttpEmbedder` sem retry/circuit-breaker:** `embed/http.go` L51 faz um unico `client.Post` com timeout 10s. Se Ollama estiver lento, o ingest bloqueia o worker por 10s e a fila pode acumular.
- **Cleanup FTS5 sincrono em `Cleanup()`:** `sqlite.go` L504 faz `INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild')` inline, bloqueando o banco. Isso viola a regra de negocio declarada ("nunca FTS5 rebuild sincrono em ingest") — mas e chamado em cleanup, nao em ingest, o que e aceitavel. Porem e operacao potencialmente lenta.

| Dimensao | v1 | v2 | Comentario |
|---|---|---|---|
| Onboarding | 3 | 3 | Sem mudanca relevante |
| API Ergonomics | 3 | 3 | Sem mudanca |
| Feature Completeness | 1 | 3 | N+1 resolvido, stub bypass correto, sanitize URL |
| Security Confidence | 3 | 3 | Sem mudanca |
| Language Quality | 2 | 3 | isStub idiomatico, JOIN correto |
| Operational Readiness | 1 | 2 | Metricas de throughput no /metrics, mas sem profiling real |
| Documentation | 1 | 1 | Comportamento de degradacao sob carga nao documentado |
| **Score** | **2.0** | **2.6** | Melhora moderada; full scan e HttpEmbedder sem retry sao gaps reais |

---

## Projeto 3: Observability

### O que mudou para mim

**P1-04 (/healthz com Ping):** Confirmado em `routes.go` L77-85. `s.Ping()` faz `SELECT 1`. Retorna 503 se banco inacessivel. Correto.

**P2-08 (/metrics Prometheus):** Confirmado em `metrics.go` L13-43. `atomic.Int64` para todos os contadores. Content-Type correto (`text/plain; version=0.0.4`). Sem dependencia externa. `vbm_pages_indexed` e `vbm_queue_pending` como gauges via `GetStatus()`. Muito bom.

**P1-10 (WebSocket ping/pong):** Confirmado em `routes.go` L231-282. `SetReadDeadline` (60s), `SetWriteDeadline` (10s), `SetPongHandler` que renova o deadline, goroutine de leitura dedicada para processar control frames. Implementacao correta do gorilla/websocket pattern.

**P1-02 (porta real no popup):** Confirmado em `service-worker.ts` L128-130. `daemonPort: daemonState.port` enviado na resposta `popup_status`.

### O que ainda nao funciona ou esta ausente

- **P1-13 (slog) ainda ausente:** Todo o daemon usa `log.Printf`. Sem campos estruturados, sem nivel, sem trace ID. Journald recebe linhas de texto — impossivel fazer `journalctl -o json` e extrair campos. Gap significativo para producao.
- **/metrics sem auth:** `routes.go` L88 — `/metrics` esta fora do grupo autenticado. Intencional para scraping Prometheus, mas em producao expose contadores de uso (search_total, pages_indexed) sem autenticacao. Para uso local isso e aceitavel, mas e uma decisao que deveria estar documentada.
- **Sem correlacao de request ID:** `middleware.Logger` do chi nao gera request IDs. Impossivel correlacionar logs de um request especifico.
- **`/healthz` nao informa versao nem uptime:** retorna apenas `{"ok":true}`. Para monitoring adequado, uptime e versao sao essenciais.
- **Sem alertas ou runbook:** nao ha documentacao de "o que fazer quando /healthz retorna 503".

| Dimensao | v1 | v2 | Comentario |
|---|---|---|---|
| Onboarding | 3 | 3 | Sem mudanca no setup |
| API Ergonomics | 3 | 4 | /healthz correto, /metrics bem formatado, /status funcional |
| Feature Completeness | 2 | 4 | /metrics, /healthz DB, WS ping/pong, porta real no popup |
| Security Confidence | 3 | 3 | /metrics sem auth e decisao consciente mas nao documentada |
| Language Quality | 2 | 4 | atomic.Int64 idiomatico, WS pattern correto |
| Operational Readiness | 1 | 3 | Prometheus-ready, health check real — falta slog e request IDs |
| Documentation | 1 | 1 | Nenhum runbook adicionado |
| **Score** | **2.1** | **3.1** | Salto expressivo nos contadores e health check |

---

## Projeto 4: NativeMessagingReliability

### O que mudou para mim

**P1-12 (SessionPath retorna erro):** Confirmado em `host.go` L32-38. `SessionPath() (string, error)` propaga erro do `os.UserHomeDir()`. `readSession()` L40-51 usa o erro. Em `server.go` L75-77, o defer de remocao ignora o erro do `SessionPath()` (`if err == nil`), o que e defensivamente correto.

**P2-09 (resetDaemon + revalidacao automatica):** Confirmado em `native-bridge.ts` L55-58 e `daemon-client.ts` L24-27. `checkResponse` chama `resetDaemon()` em 401, forcando re-handshake na proxima chamada. Pattern correto.

**P1-14 (Extension ID validado):** Confirmado em `install.sh` L49-56. Regex `^[a-p]{32}$` com warning se invalido (nao abort — deixa continuar com aviso, o que e razoavel para o caso "instalar antes de ter o ID").

### O que ainda nao funciona ou esta ausente

- **NM host sem timeout de leitura:** `host.go` L71-94. `readMessage(os.Stdin)` pode bloquear indefinidamente se o Chrome nao enviar dados. Para o protocolo NM isso raramente e problema (Chrome fecha o pipe), mas nao ha `os.Stdin.SetDeadline` ou contexto com timeout.
- **connectDaemon() sem retry com backoff:** `native-bridge.ts` L17-51. Se o handshake falhar, a proxima chamada a qualquer funcao tenta de novo imediatamente (`if (daemonState.port !== null)` e falso apos reset). Sem jitter nem backoff exponencial — pode gerar burst de NM calls se o daemon estiver em restart loop.
- **Token enviado pelo NM host em plaintext:** session.json (chmod 600) e lido e retornado pelo host. Correto por design, mas nao ha validacao de que o caller e a extensao correta alem do Extension ID no manifest. Aceitavel para v0.1.
- **Sem teste de reconexao automatica documentado:** o comportamento pos-401 funciona no codigo mas nao ha teste automatizado nem documentacao do fluxo.

| Dimensao | v1 | v2 | Comentario |
|---|---|---|---|
| Onboarding | 3 | 3 | Install melhorado, mas setup NM ainda manual |
| API Ergonomics | 4 | 4 | Contrato NM claro e nao mudou |
| Feature Completeness | 3 | 4 | resetDaemon() + revalidacao automatica implementados |
| Security Confidence | 3 | 3 | SessionPath com erro, chmod 600 — sem regressao |
| Language Quality | 3 | 4 | Propagacao de erro correta, pattern de reset idiomatico |
| Operational Readiness | 2 | 3 | Re-handshake automatico e real ganho operacional |
| Documentation | 2 | 2 | Fluxo de reconexao nao documentado |
| **Score** | **2.9** | **3.3** | Melhora solida no reliability de conexao |

---

## Projeto 5: DatabaseIntegrity

### O que mudou para mim

**P2-03 (SetMaxOpenConns(1)):** Confirmado em `sqlite.go` L118. Explicito, com comentario explicando o motivo.

**P1-05 (schema versionado):** Confirmado em `sqlite.go` L136-172. Tabela `schema_versions`, `COALESCE(MAX(version), 0)`, loop de migrations. Correto e extensivel.

**P0-03 (FTS5 incremental):** Confirmado em `sqlite.go` L235-240. `RowsAffected() > 0` detecta chunk novo. `INSERT INTO chunks_fts(rowid, text)` apenas para novos chunks. Sem rebuild em ingest. Correto.

**P1-03 (Forget deleta queue):** Confirmado em `sqlite.go` L451-488. Todos os tres tipos (`url`, `domain`, `timerange`) deletam de `pages` e `queue` na mesma transacao.

**P0-07 (Cleanup com TTL):** Confirmado em `sqlite.go` L493-509. Cleanup correto — nota: faz FTS5 rebuild sincrono (L504), o que e aceitavel em contexto de cleanup (nao ingest).

**Foreign keys e WAL:** Confirmados em `sqlite.go` L119-125. `PRAGMA journal_mode=WAL` e `PRAGMA foreign_keys=ON` executados na abertura.

### O que ainda nao funciona ou esta ausente

- **`migrate()` sem transacao atomica:** `sqlite.go` L157-171. Cada migration e executada sem `BEGIN/COMMIT`. Se o processo for morto durante uma migration, `schema_versions` pode nao ser atualizada mas o DDL foi executado. Nao e problema com uma unica migration (v1), mas e fragil para futuras.
- **`ON CONFLICT DO UPDATE` em pages nao retorna `LastInsertId` consistentemente:** `sqlite.go` L210-217. O codigo tem um fallback correto (`SELECT id WHERE url_hash = ?`), mas isso e uma query extra por upsert. Para o volume declarado (30-50/dia) e inofensivo.
- **Cleanup nao deleta da `queue` table:** `sqlite.go` L498. `DELETE FROM pages WHERE visit_ts < ?` aciona CASCADE em chunks, mas a tabela `queue` nao e limpa pelo Cleanup. Itens processados (status = qualquer coisa apos RemoveQueueItem) sao removidos, mas itens cujas pages foram limpas por TTL ainda teriam o URL pendente se o ingest falhou e o item ficou na queue com outro status. Gap menor.
- **Nao ha `PRAGMA integrity_check`:** nenhum endpoint ou rotina verifica integridade do banco. Para um banco local de longa vida, WAL sem checkpoint periodico pode crescer indefinidamente.

| Dimensao | v1 | v2 | Comentario |
|---|---|---|---|
| Onboarding | 4 | 4 | Sem regressao |
| API Ergonomics | 3 | 3 | Sem mudanca |
| Feature Completeness | 2 | 4 | FTS5 incremental, schema versioning, FK ON, SetMaxOpenConns(1) |
| Security Confidence | 3 | 4 | WAL + FK + SetMaxOpenConns eliminam classes de bugs de concorrencia |
| Language Quality | 3 | 4 | migrate() idiomatico, Forget() transacional correto |
| Operational Readiness | 2 | 3 | Cleanup com TTL funcional; falta integrity_check e WAL checkpoint |
| Documentation | 2 | 2 | Schema migration nao documentada externamente |
| **Score** | **2.7** | **3.4** | Maior salto da v2 — fundacao de dados solida |

---

## Findings Remanescentes

### P0 (Bloqueadores)

Nenhum P0 remanescente identificado. Todos os P0 da v1 foram corrigidos e verificados.

### P1 (Importantes)

| ID | Descricao | Arquivo | Linha | Status |
|---|---|---|---|---|
| P1-13 | Logs estruturados (slog) ausentes | todos os .go | - | Nao implementado (deferido) |
| P1-NEW-01 | Full table scan em dense search com embedder real | `sqlite.go` | L316 | Sem ANN index — risco de latencia com >5k chunks |
| P1-NEW-02 | `migrate()` sem transacao atomica | `sqlite.go` | L157-171 | Fragil para futuras migrations |
| P1-NEW-03 | Cleanup nao limpa tabela `queue` por TTL | `sqlite.go` | L493-509 | Gap de consistencia menor |

### P2 (Melhorias)

| ID | Descricao | Arquivo | Linha |
|---|---|---|---|
| P2-NEW-01 | `HttpEmbedder` sem retry/circuit-breaker | `embed/http.go` | L51 |
| P2-NEW-02 | `/metrics` sem auth — decisao nao documentada | `routes.go` | L88 |
| P2-NEW-03 | `/healthz` nao retorna versao nem uptime | `routes.go` | L77-85 |
| P2-NEW-04 | `connectDaemon()` sem backoff exponencial | `native-bridge.ts` | L17-51 |
| P2-NEW-05 | `Restart=on-failure` sem `RestartPreventExitStatus` | `vbmd.service` | L9 |
| P2-NEW-06 | Sem `sd_notify`/`Type=notify` — systemd nao sabe quando daemon esta pronto | `vbmd.service` | L4 |
| P2-NEW-07 | WAL sem checkpoint periodico — WAL file pode crescer indefinidamente | `sqlite.go` | - |

---

## Conclusao

A v2 representa uma evolucao real e verificavel. Os tres CRs cobriram os blockers com precisao cirurgica:

**Pontos mais fortes da v2:**
- Graceful shutdown com drain de fila e timeout de 30s e o padrao correto para um daemon Go.
- `/metrics` em formato Prometheus com `atomic.Int64` e zero dependencia externa e elegante e operacionalmente util.
- FTS5 incremental via `RowsAffected()` e a abordagem certa — sem rebuild, sem lock.
- Schema migrations versionadas dao ao projeto capacidade de evoluir o banco sem perda de dados.
- WebSocket com ping/pong e `SetReadDeadline` elimina conexoes zumbi.

**Gaps que limitam o score:**
- Logs estruturados ausentes sao o maior gap operacional remanescente. Em producao, `log.Printf` no journald e insuportavel para debugging.
- O full table scan em dense search com embedder real e uma bomba-relogio para bases de dados maiores.
- `migrate()` sem transacao e uma armadilha para futuras versoes do schema.

O projeto passou de "tem problemas bloqueadores" para "funciona corretamente em producao para o caso de uso declarado (30-50 paginas/dia, single user, Linux)". Para escalar alem disso, P1-NEW-01 (ANN index) e P1-13 (slog) sao os proximos investimentos obrigatorios.

---

## Score Medio

| Dimensao | SystemdReliability | PerformanceUnderLoad | Observability | NMReliability | DBIntegrity | Media v2 | Media v1 |
|---|---|---|---|---|---|---|---|
| Onboarding | 4 | 3 | 3 | 3 | 4 | 3.4 | 3.4 |
| API Ergonomics | 3 | 3 | 4 | 4 | 3 | 3.4 | 3.2 |
| Feature Completeness | 4 | 3 | 4 | 4 | 4 | 3.8 | 2.0 |
| Security Confidence | 4 | 3 | 3 | 3 | 4 | 3.4 | 3.0 |
| Language Quality | 3 | 3 | 4 | 4 | 4 | 3.6 | 2.6 |
| Operational Readiness | 3 | 2 | 3 | 3 | 3 | 2.8 | 1.6 |
| Documentation | 2 | 1 | 1 | 2 | 2 | 1.6 | 1.6 |
| **Score do Projeto** | **3.3** | **2.6** | **3.1** | **3.3** | **3.4** | **3.1** | **2.5** |

**Score v2: 3.1 / 5 (v1: 2.5)**
