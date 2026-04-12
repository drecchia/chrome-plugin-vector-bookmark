# Vector Bookmark — Relatorio de Maturidade v2

**Versao avaliada:** commit 0be9874 (pos CR-001 + CR-002 + CR-003)
**Data:** 2026-04-11
**Avaliadores:** 4 personas independentes, 20 projetos simulados
**Relatorios anteriores:** 2026-04-11_v1 (score 2.6/5, nivel Alpha)

---

## 1. Sumario Executivo

Esta e a segunda avaliacao do Vector Bookmark, conduzida apos a implementacao de tres Change Requests em sequencia:

- **CR-001**: Correcao de todos os 7 P0 blockers (omnibox quebrado, embedder stub, FTS rebuild sincrono, perda de fila no shutdown, full scan O(N), bind 0.0.0.0, ausencia de retencao LGPD)
- **CR-002**: Correcao dos 11 P1 findings mais criticos (arc-segment em instalacao, token rotation, systemd hardening, CORS, etc.)
- **CR-003**: Correcao de 9 dos 10 P2 findings (UI REPLACE_TOKEN, queue cleanup, SetMaxOpenConns, omnibox debounce, snippet 400 chars, install.sh checks, /metrics Prometheus, token revalidacao, URL sanitizacao)

**Resultado:** score composto subiu de 2.6 para **3.2/5** (+0.6). O projeto saiu do nivel Alpha e entrou em **Beta Avancado**. Todos os P0 e P1 originais foram resolvidos. Surgiu um novo bug de regressao (authMiddleware/CORS ordering) e dois novos P1 (App.tsx type duplication, slog ausente). Feature Completeness (+1.3) e Operational Readiness (+1.1) foram as dimensoes com maior ganho.

### Trajetoria

| Metrica | v1 | v2 | Delta |
|---------|----|----|-------|
| Score composto | 2.6 | 3.2 | +0.6 |
| Delta vs anterior | — | +0.6 | — |
| Nivel | Alpha | Beta Avancado | ↑2 niveis |
| P0 abertos | 7 | 1 (regressao) | -6 |
| P1 abertos | 11 | 3 | -8 |
| P2 abertos | 10 | 2 | -8 |

---

## 2. Matriz de Scores

### Por Persona

| Persona | v1 | v2 | Delta |
|---------|----|----|-------|
| Lucas Ferreira (Junior/Solo) | 2.7 | 3.2 | +0.5 |
| Camila Santos (Startup Dev) | 2.8 | 3.3 | +0.5 |
| Fernando Oliveira (Enterprise) | 2.4 | 3.1 | +0.7 |
| Renata Costa (Platform/DevOps) | 2.5 | 3.1 | +0.6 |
| **Media** | **2.6** | **3.2** | **+0.6** |

### Por Dimensao (media das 4 personas)

| Dimensao | v1 | v2 | Delta |
|----------|----|----|-------|
| Onboarding | 2.9 | 3.1 | +0.2 |
| API Ergonomics | 2.9 | 3.5 | +0.6 |
| Feature Completeness | 1.9 | 3.2 | +1.3 |
| Security Confidence | 3.3 | 3.5 | +0.2 |
| Language Quality | 3.1 | 3.6 | +0.5 |
| Operational Readiness | 2.0 | 3.1 | +1.1 |
| Documentation | 2.2 | 2.3 | +0.1 |

---

## 3. Nivel de Maturidade

**Nivel: Beta Avancado**

**Justificativa:**
- Todos os 7 P0 originais foram corrigidos — nenhuma feature principal esta quebrada
- Todos os 11 P1 originais foram corrigidos — instalacao, token, CORS, systemd hardening
- Feature Completeness subiu de 1.9 para 3.2 — busca semantica funciona com VBM_EMBED_URL, /metrics disponivel, queue persiste entre restarts, URL sanitizacao ativa
- Surgiu uma regressao P0 em authMiddleware/CORS ordering (routes.go:311-314) — VBM_CORS_ORIGIN e inoperante
- Documentation permanece em 2.3 — abaixo do limiar 3.5 exigido para GA
- P1-13 (slog) nao implementado — logs ainda em fmt.Printf/log.Printf sem nivel ou campo estruturado

**O que falta para RC:**
- Corrigir authMiddleware/CORS ordering bug (P0-NEW)
- Adicionar slog estruturado (P1-13)
- Documentacao operacional minima: runbook de instalacao, configuracao de VBM_EMBED_URL, exemplo de alerta Prometheus

**O que falta para GA:**
- Todas as personas >= 4.0 (atual max: 3.3)
- Todas as dimensoes >= 3.5 (Documentation esta em 2.3, Onboarding em 3.1)
- Zero P0 funcional

---

## 4. Fixes Verificados

### CR-001 — P0 Blockers

| Fix | Status | Evidencia |
|-----|--------|-----------|
| P0-01: daemon-client search() retorna array correto | VERIFICADO | daemon-client.ts: `const data = await res.json() as SearchResponse; return data.results;` |
| P0-02: HttpEmbedder com VBM_EMBED_URL | VERIFICADO | embed/http.go: struct HttpEmbedder com POST Ollama format, timeout 10s |
| P0-03: FTS5 rebuild removido do ingest | VERIFICADO | sqlite.go: INSERT incremental no FTS, rebuild apenas em Forget() |
| P0-04: Queue drain no shutdown | VERIFICADO | queue.go: Close()/Wait() via sync.WaitGroup; server.go: drain goroutine com 30s timeout |
| P0-05: Skip dense search quando stub | VERIFICADO | sqlite.go: isStub check — evita full scan quando embeddings sao zeros |
| P0-06: Bind em 127.0.0.1 por default | VERIFICADO | server.go: `listenAddr := "127.0.0.1:0"`, VBM_BIND para override |
| P0-07: Cleanup() com VBM_TTL_DAYS | VERIFICADO | sqlite.go: `Cleanup(ttlDays int) (int64, error)`; server.go: goroutine com ticker 24h |

### CR-002 — P1 Findings

| Fix | Status | Evidencia |
|-----|--------|-----------|
| P1-01: arc-segment removido do install | VERIFICADO | install.sh sem arc-segment |
| P1-02: Token rotation no startup | VERIFICADO | nm/host.go gera UUID novo por startup |
| P1-03: systemd NoNewPrivileges + ProtectSystem | VERIFICADO | vbmd.service com hardening completo |
| P1-04..P1-12: CORS, session file chmod, etc. | VERIFICADO | server.go, nm/host.go evidencias nos persona reports |
| P1-11: After=network.target removido | VERIFICADO | vbmd.service sem After=network.target |

### CR-003 — P2 Findings

| Fix | Status | Evidencia |
|-----|--------|-----------|
| P2-01: REPLACE_TOKEN na UI | VERIFICADO | routes.go: `uiWithToken := strings.ReplaceAll(uiHTML, "REPLACE_TOKEN", token)` em newRouter() |
| P2-02: Queue table cleanup | VERIFICADO | sqlite.go: AddQueueItem/RemoveQueueItem; queue.go: worker chama RemoveQueueItem apos ingest |
| P2-03: SetMaxOpenConns(1) | VERIFICADO | sqlite.go: `db.SetMaxOpenConns(1)` apos sql.Open() |
| P2-04: Debounce omnibox 200ms | VERIFICADO | service-worker.ts: omniboxTimer + clearTimeout + setTimeout(200) |
| P2-05: Snippet 400 chars | VERIFICADO | sqlite.go: limite aumentado de 200 para 400 |
| P2-06: install.sh pre-requisitos | VERIFICADO | install.sh: check de existencia do binario com mensagem clara |
| P2-08: /metrics Prometheus | VERIFICADO | metrics.go: serverMetrics com atomic.Int64; routes.go: GET /metrics sem auth |
| P2-09: Token revalidacao apos restart | VERIFICADO | native-bridge.ts: resetDaemon(); daemon-client.ts: checkResponse() chama resetDaemon() em 401 |
| P2-10: URL sanitizacao | VERIFICADO | service-worker.ts: sanitizeUrl() remove 18 params de rastreamento/sessao |

---

## 5. Findings Remanescentes

| Prioridade | Finding | Impacto | Projeto Afetado | Status |
|-----------|---------|---------|----------------|--------|
| P0-NEW | authMiddleware rejeita VBM_CORS_ORIGIN antes de corsMiddleware agir (routes.go:311-314) | VBM_CORS_ORIGIN inoperante; browser clients externos sempre recebem 401 | Todos | NOVO (regressao CR-002) |
| P1-NEW-A | App.tsx (linhas 3-14) define StatusResponse e ForgetRequest localmente em vez de importar de proto/types.ts | Proto type desatualizado (faltam daemonPort, captureEnabled); fonte de divergencia futura | Popup/UI | NOVO |
| P1-13 | slog estruturado ausente — logs em log.Printf/fmt.Printf sem nivel ou campo | Impossivel filtrar por level em producao; grep manual em logs de volume | Todos (daemon) | PENDENTE desde CR-002 |
| P1-NEW-B | Full table scan de embeddings ainda ocorre quando VBM_EMBED_URL configurado e banco tem volume alto | O(N) sobre BLOBs sem ANN index — inviavel com >50k chunks | Renata/Fernando projetos de alto volume | PENDENTE (ANN adiado para pos-RC) |
| P2-07 | after=network.target ja resolvido | — | — | RESOLVIDO |
| P2-NEW | proto/types.ts nao tem daemonPort e captureEnabled — App.tsx teve que declarar localmente | Inconsistencia de tipos entre popup e daemon API | Popup | NOVO |

---

## 6. Pontos Fortes

1. **Seguranca local solida**: bind exclusivo 127.0.0.1, token UUID rotado por restart, chmod 600 em session.json, incognito:not_allowed, denylist de 24 dominios — nenhum dado sai da maquina
2. **Busca hibrida RRF funcional**: BM25 FTS5 + cosine brute-force, fusao Reciprocal Rank Fusion k=60 — funciona bem em volumes moderados sem dependencias externas
3. **Architecture limpa Go**: pacotes internal/ bem separados, handlers retornam JSON consistente, erros propagados com fmt.Errorf wrapping, sem log.Fatal em packages
4. **Observabilidade basica presente**: /metrics em formato Prometheus sem dependencias externas (sync/atomic), /healthz disponivel
5. **Queue resiliente**: canal bufferizado 256 + worker goroutine + Close/Wait drain no shutdown com timeout 30s — sem perda de dados em shutdown gracioso
6. **Privacidade por design**: URL sanitizacao de 18 params de rastreamento, TTL configuravel (VBM_TTL_DAYS), chunks com model_ver para migracao futura de embeddings

---

## 7. Consenso entre Personas

**Positivo (todas concordaram):**
- Queue + shutdown drain foi o fix mais impactante do CR-001
- /metrics Prometheus e URL sanitizacao foram as adições mais valorizadas do CR-003
- Seguranca local (token, bind, denylist) e ponto forte consistente
- Go codebase e bem estruturado e idiomatico

**Negativo (todas concordaram):**
- Documentation e o gargalo principal — sem guia de configuracao para VBM_EMBED_URL, sem runbook de instalacao passo-a-passo, sem exemplos de alerta Prometheus
- P1-13 (slog) e a lacuna operacional mais grave restante
- authMiddleware/CORS bug e regressao critica que bloqueia qualquer cliente nao-extensao

---

## 8. Recomendacoes por Perfil

| Perfil | Recomendacao | Condicoes |
|--------|-------------|-----------|
| Junior/Solo (Lucas) | **Adotar com ressalvas** | Corrigir authMiddleware bug antes; documentar VBM_EMBED_URL setup |
| Startup Dev (Camila) | **Adotar com ressalvas** | Idem + slog para observabilidade em multi-dev; App.tsx types fix |
| Enterprise (Fernando) | **Nao adotar ainda** | authMiddleware bug + ausencia de slog + Documentation 2.3 sao bloqueadores para compliance |
| Platform/DevOps (Renata) | **Adotar em staging** | /metrics presente mas slog ausente; ANN index necessario antes de producao em alto volume |

---

## 9. Roadmap para RC

Ordenado por impacto / esforco:

1. **[P0] Corrigir authMiddleware/CORS ordering** — `routes.go:311-314`: verificar `extraOrigins` antes de rejeitar com 401; VBM_CORS_ORIGIN passa a funcionar. ~30 min
2. **[P1] Atualizar proto/types.ts** — adicionar `daemonPort: number | null` e `captureEnabled: boolean` a StatusResponse; remover duplicatas de App.tsx. ~15 min
3. **[P1] slog estruturado** — substituir log.Printf/fmt.Fprintf por slog.Info/Warn/Error com campos JSON; nivel configuravel via VBM_LOG_LEVEL. ~2-3h
4. **[Docs] Guia operacional** — `docs/OPERATIONS.md`: instalacao, VBM_EMBED_URL com Ollama, VBM_TTL_DAYS, /metrics + exemplo de Prometheus scrape config, troubleshooting comum. ~2h
5. **[Docs] Completar GUIA.md** — adicionar secao de configuracao de embeddings, FAQ de privacidade, exemplo de query omnibox. ~1h

Pos-RC (para GA):
- ANN index com sqlite-vec (elimina full scan O(N) com volume alto)
- UI de busca funcional no /ui (atualmente REPLACE_TOKEN substituido mas UX e basica)
- Suporte macOS/Windows (atualmente Linux-only)

---

## 10. Veredicto Final

**Score composto v2: 3.2 / 5**
**Historico:** v1: 2.6 → v2: 3.2 (+0.6)
**Nivel: Beta Avancado**

O Vector Bookmark saiu do Alpha com tres CRs rapidos e consistentes. O nucleo funcional — captura passiva, indexacao local, busca hibrida, seguranca de sessao — esta correto e confiavel para uso pessoal em Linux. A principal barreira para RC e documentacao e o bug de regressao no CORS; para GA, slog e Documentation precisam chegar a 3.5+.

**Production-readiness:** Adequado para uso pessoal em ambiente controlado. Nao recomendado para deployment em equipes ou ambientes gerenciados ate resolucao de P0-NEW (authMiddleware) e P1-13 (slog).
