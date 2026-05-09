# Vector Bookmark — Runbook Operacional (pt-BR)

Runbook para quem **opera** ou **integra** o Vector Bookmark. Para guia de usuário final, veja `docs/GUIA.md`.

Este documento cobre variáveis de ambiente, setup de busca semântica real (OpenRouter ou Ollama), retenção de dados (LGPD), observabilidade com Prometheus, logs estruturados e troubleshooting.

---

## 1. Pré-requisitos

| Componente | Mínimo | Observação |
|---|---|---|
| SO | Linux (kernel 5.x+) ou Windows 10/11 | macOS pendente |
| Init | systemd user sessions | `loginctl enable-linger $USER` para persistir após logout |
| Go | 1.22+ | Apenas para build — runtime é estático |
| Node.js | 20+ | Build da extensão |
| Chrome/Chromium | estável | Manifest V3 |
| Embedder *(opcional)* | — | OpenRouter (cloud, sem GPU) ou Ollama (local, CPU ok). Sem config usa `StubEmbedder` (zeros). |

---

## 2. Instalação passo-a-passo

Para instalação usuário final, veja `docs/GUIA.md §Instalação`. Os passos abaixo focam no que o **operador** precisa auditar.

```bash
# 1. Build daemon
cd daemon && make build
# Verificar: ls -l bin/vbmd  →  binário estático ~20 MB

# 2. Build extensão
cd ../extension && npm install && npm run build
# Verificar: ls extension/dist/manifest.json

# 3. Instalar daemon (systemd unit)
cd ../daemon && make install
# Faz:
# - install -Dm755 bin/vbmd ~/.local/bin/vbmd
# - cria ~/.config/systemd/user/vbmd.service
# - systemctl --user daemon-reload && systemctl --user enable --now vbmd

# 4. Carregar extensão em chrome://extensions → Load unpacked → extension/dist/

# 5. Sanidade
systemctl --user status vbmd
curl -s http://127.0.0.1:7532/healthz    # {"ok":true}
curl -s http://127.0.0.1:7532/metrics    # texto Prometheus
```

**Checklist pós-instalação:**
- [ ] `vbmd.service` rodando, sem restart loops em `systemctl --user status`
- [ ] `/healthz` retorna `{"ok":true}` (não `database unavailable`)
- [ ] Extensão carregada, popup abre sem erro
- [ ] Nenhum processo escutando fora de `127.0.0.1` — `ss -tlnp | grep vbmd` deve mostrar apenas loopback

---

## 3. Variáveis de ambiente

Todas as variáveis são lidas no startup do daemon (`daemon/internal/server/server.go` e `daemon/cmd/vbmd/main.go`). Use `systemctl --user edit vbmd` para setar via `Environment=KEY=value`.

| Variável | Default | Obrigatória? | Descrição |
|---|---|---|---|
| `VBM_PORT` | `7532` | Não | Porta de escuta. Override via env ou `~/.config/vbm/env`. Atualizar no popup se mudar. |
| `VBM_BIND` | `127.0.0.1` | Não | Interface de bind. **Nunca usar `0.0.0.0`** em host compartilhado. Apenas Docker com rede isolada deve alterar. |
| `VBM_EMBED_URL` | *(vazio → stub)* | Opcional | Endpoint de embeddings. Sem ele, `StubEmbedder` produz vetores zero e a busca cai para BM25 puro. |
| `VBM_EMBED_FORMAT` | `ollama` | Não | `openai` para usar formato OpenAI-compatible (`{"input":...}`) — necessário para OpenRouter/OpenAI. Setado automaticamente se `VBM_EMBED_API_KEY` estiver presente. |
| `VBM_EMBED_MODEL` | `nomic-embed-text` | Não | Nome do modelo. OpenRouter: `openai/text-embedding-3-small`. Ollama: `nomic-embed-text`. |
| `VBM_EMBED_API_KEY` | *(vazio)* | Condicional | API key para OpenRouter ou OpenAI. Vazio para Ollama local. |
| `VBM_TTL_DAYS` | *(sem retenção)* | Recomendada | Retenção LGPD: páginas com `visit_ts` mais antigo que N dias são removidas por um ticker 24h. |
| `VBM_LOG_LEVEL` | `info` | Não | `debug` / `info` / `warn` / `error`. Handler é slog JSON em stderr. |
| `VBM_CORS_ORIGIN` | *(vazio)* | Condicional | CSV de origens extras (além de `chrome-extension://*`) aceitas para requisições HTTP. Necessário para dashboards externos. |
| `VBM_AUTH_TOKEN` | *(vazio)* | **Obrigatória se exposto** | Bearer token exigido em todas as rotas exceto `/healthz` e `/metrics`. Vazio = acesso aberto — só aceitável quando `VBM_BIND=127.0.0.1`. WS aceita `?token=…` na query (browsers não enviam header custom no handshake). Configurar o mesmo valor no popup → Settings → Auth token. |

**Caminhos de dados por plataforma:**

| Plataforma | Data dir | Config file |
|---|---|---|
| Linux | `~/.local/share/vbm/` | `~/.config/vbm/env` (via systemd `EnvironmentFile`) |
| Windows | `%APPDATA%\vbm\` | `%APPDATA%\vbm\env` (carregado por `loadEnvFile()` no startup) |

**Configuracao persistente — Linux (`~/.config/vbm/env`):**

O servico carrega automaticamente `~/.config/vbm/env` no startup (via `EnvironmentFile` no `vbmd.service`). Este arquivo **nao e sobrescrito por `make install`**, entao configuracoes sobrevivem a upgrades.

```bash
mkdir -p ~/.config/vbm
# Editar com qualquer editor — uma variavel por linha, sem aspas necessarias
nano ~/.config/vbm/env
```

```ini
# Opção A — OpenRouter (sem GPU, requer API key)
VBM_EMBED_URL=https://openrouter.ai/api/v1/embeddings
VBM_EMBED_FORMAT=openai
VBM_EMBED_API_KEY=sk-or-xxxxxxxxxxxx
VBM_EMBED_MODEL=openai/text-embedding-3-small

# Opção B — Ollama local (sem API key, CPU ok)
# VBM_EMBED_URL=http://127.0.0.1:11434/api/embeddings
# VBM_EMBED_MODEL=nomic-embed-text

VBM_TTL_DAYS=90
VBM_LOG_LEVEL=info
VBM_CORS_ORIGIN=http://localhost:3000,http://localhost:8080
```

Aplicar sem reiniciar a maquina:

```bash
systemctl --user restart vbmd
```

**Configuracao persistente — Windows (`%APPDATA%\vbm\env`):**

```powershell
# Criar/editar arquivo de configuracao
notepad "$env:APPDATA\vbm\env"
```

```ini
# Opção A — OpenRouter
VBM_EMBED_URL=https://openrouter.ai/api/v1/embeddings
VBM_EMBED_FORMAT=openai
VBM_EMBED_API_KEY=sk-or-xxxxxxxxxxxx
VBM_EMBED_MODEL=openai/text-embedding-3-small

# Opção B — Ollama local
# VBM_EMBED_URL=http://127.0.0.1:11434/api/embeddings
# VBM_EMBED_MODEL=nomic-embed-text

VBM_TTL_DAYS=90
VBM_LOG_LEVEL=info
VBM_CORS_ORIGIN=http://localhost:3000
```

Reiniciar o daemon para aplicar:

```powershell
Stop-Process -Name "vbmd" -ErrorAction SilentlyContinue
Start-Process -FilePath "$env:LOCALAPPDATA\vbm\vbmd.exe" -ArgumentList "server" -WindowStyle Hidden
```

---

## 4. Setup de busca semântica

Sem `VBM_EMBED_URL`, o daemon usa `StubEmbedder` (vetores zero) — busca cai para BM25 puro (palavras exatas). Para ativar fusão BM25 + cosine há duas opções:

| | OpenRouter | Ollama |
|---|---|---|
| GPU necessária | Não | Não (CPU ok) |
| Privacidade | Texto enviado para API externa | 100% local |
| Modelo sugerido | `openai/text-embedding-3-small` | `nomic-embed-text` |
| Custo | ~$0.02 / 1M tokens | Gratuito |

### 4.1 Opção A — OpenRouter (sem GPU, sem Ollama)

```bash
mkdir -p ~/.config/vbm
cat >> ~/.config/vbm/env <<'EOF'
VBM_EMBED_URL=https://openrouter.ai/api/v1/embeddings
VBM_EMBED_FORMAT=openai
VBM_EMBED_API_KEY=sk-or-xxxxxxxxxxxx
VBM_EMBED_MODEL=openai/text-embedding-3-small
EOF
systemctl --user restart vbmd
```

Obter API key em https://openrouter.ai → API Keys. Tier gratuito disponível.

### 4.2 Opção B — Ollama (100% local, CPU ok)

```bash
# Instalar e baixar modelo
curl -fsSL https://ollama.com/install.sh | sh
ollama pull nomic-embed-text   # 274 MB, 768d

mkdir -p ~/.config/vbm
cat >> ~/.config/vbm/env <<'EOF'
VBM_EMBED_URL=http://127.0.0.1:11434/api/embeddings
VBM_EMBED_MODEL=nomic-embed-text
EOF
systemctl --user restart vbmd
```

Modelos alternativos: `mxbai-embed-large` (669 MB, 1024d), `all-minilm` (23 MB, 384d).

### 4.3 Formatos de protocolo

**OpenRouter / OpenAI (`VBM_EMBED_FORMAT=openai`):**

```json
// Request body
{ "model": "openai/text-embedding-3-small", "input": "texto" }
// Response
{ "data": [{ "embedding": [0.123, -0.456, ...] }] }
```

**Ollama (formato padrão):**

```json
// Request body
{ "model": "nomic-embed-text", "prompt": "texto" }
// Response
{ "embedding": [0.123, -0.456, ...] }
```

Timeout: **30s** (OpenAI) / **10s** (Ollama).

### 4.4 Re-embedar páginas já indexadas

Páginas capturadas antes da configuração têm `model_ver='stub-v0'` (vetores zero). Para re-embedá-las:

**Via popup da extensão:** botão **"Re-embed pages (semantic search)"** — exibido quando há páginas indexadas. Mostra progresso `N/M` em tempo real.

**Via curl:**

```bash
curl -s -X POST http://127.0.0.1:7532/admin/reindex
# → {"started":true}

# Acompanhar progresso
curl -s http://127.0.0.1:7532/admin/reindex/status
# → {"running":true,"done":42,"total":150}
# → {"running":false,"done":150,"total":150}
```

### 4.5 Verificar

```bash
# Confirmar model_ver nos chunks
sqlite3 ~/.local/share/vbm/vbm.db \
  "SELECT model_ver, count(*) FROM chunks GROUP BY model_ver;"
# openai-v0 ou http-v0 = embeddings reais; stub-v0 = ainda pendente

# Testar busca semântica
curl -s "http://127.0.0.1:7532/search?q=seguranca+hackers&limit=5" | jq
```

---

## 5. Retenção de dados (LGPD / direito ao esquecimento)

### 5.1 TTL automático

```ini
Environment=VBM_TTL_DAYS=90
```

Uma goroutine com ticker 24h (inicia no `server.go` após bind) chama `store.Cleanup(ttlDays)` que executa:

```sql
DELETE FROM pages WHERE visit_ts < :cutoff_ms
-- chunks são removidos em cascata via ON DELETE CASCADE
```

Log estruturado emitido a cada ciclo:

```json
{"time":"2026-04-11T03:00:00Z","level":"INFO","msg":"ttl cleanup","removed":142,"ttl_days":90}
```

### 5.2 Exportação (portabilidade)

```bash
curl -s "http://127.0.0.1:7532/export" > export.json
```

Retorna todas as páginas em JSON plano (sem embeddings). Útil para auditoria de compliance e para comprovar direito de acesso.

### 5.3 Esquecimento manual

```bash
# Por URL
curl -s -X DELETE -H "Content-Type: application/json" \
  -d '{"type":"url","value":"https://exemplo.com/pagina"}' \
  "http://127.0.0.1:7532/forget"

# Por domínio
curl -s -X DELETE -H "Content-Type: application/json" \
  -d '{"type":"domain","value":"exemplo.com"}' \
  "http://127.0.0.1:7532/forget"

# Por intervalo (Unix ms)
curl -s -X DELETE -H "Content-Type: application/json" \
  -d '{"type":"timerange","value":"1700000000000:1710000000000"}' \
  "http://127.0.0.1:7532/forget"
```

Remoção é **física** — `DELETE` + FTS5 rebuild em `Forget()` (único lugar onde o rebuild acontece, nunca em `/ingest`).

---

## 6. Observabilidade

### 6.1 Endpoints

| Endpoint | Auth | Uso |
|---|---|---|
| `GET /healthz` | Não | Liveness probe. Testa conectividade DB com `Ping()`. Retorna 503 se DB indisponível. |
| `GET /metrics` | Não | Prometheus text format. Usado para scrape externo. |
| `GET /status` | Não | JSON com `indexed`, `pending`, `version`. Usado pela extensão e pela UI. |

### 6.2 Métricas expostas

Fonte: `daemon/internal/server/metrics.go` (sync/atomic, zero dependências externas).

| Métrica | Tipo | Significado |
|---|---|---|
| `vbm_ingest_total` | counter | Total de requisições `/ingest` enfileiradas |
| `vbm_search_total` | counter | Total de requisições `/search` servidas |
| `vbm_forget_total` | counter | Total de requisições `/forget` processadas |
| `vbm_ws_connections_active` | gauge | Conexões WebSocket `/ws` ativas no momento |
| `vbm_pages_indexed` | gauge | Total de páginas atualmente no banco |
| `vbm_queue_pending` | gauge | Itens na fila aguardando processamento pelo worker |

### 6.3 Prometheus scrape config

```yaml
# prometheus.yml
scrape_configs:
  - job_name: vbm_daemon
    scrape_interval: 30s
    static_configs:
      - targets: ['127.0.0.1:7532']   # ajustar se VBM_PORT foi alterado
        labels:
          instance: laptop
```

### 6.4 Exemplos de alerta

```yaml
# alerts.yml
groups:
  - name: vbm
    rules:
      - alert: VbmQueueBackpressure
        expr: vbm_queue_pending > 200
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Queue com backpressure"
          description: "vbm_queue_pending={{ $value }} há 5 minutos. Worker pode estar travado ou embedder remoto lento."

      - alert: VbmDaemonDown
        expr: up{job="vbm_daemon"} == 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "Daemon vbmd não responde ao scrape"

      - alert: VbmNoIngest
        expr: rate(vbm_ingest_total[30m]) == 0
        for: 1h
        labels:
          severity: info
        annotations:
          summary: "Nenhum ingest em 1h — extensão pausada ou crash silencioso?"
```

---

## 7. Logs estruturados (slog)

O daemon usa `log/slog` com handler JSON em stderr desde o startup. Nível configurável via `VBM_LOG_LEVEL`.

### 7.1 Formato

```json
{"time":"2026-04-11T12:34:56.789Z","level":"INFO","msg":"server listening","addr":"127.0.0.1:34521","port":34521}
{"time":"2026-04-11T12:35:12.010Z","level":"DEBUG","msg":"ingest queued","url":"https://exemplo.com","bytes":4321}
{"time":"2026-04-11T12:35:12.095Z","level":"WARN","msg":"queue persist error","url":"...","err":"database locked"}
```

### 7.2 Filtrar por nível

```bash
# Só ERROR
journalctl --user -u vbmd -o cat | jq 'select(.level=="ERROR")'

# INFO + acima do último boot
journalctl --user -u vbmd -b -o cat | jq 'select(.level != "DEBUG")'

# Todos os ingests da última hora
journalctl --user -u vbmd --since "1 hour ago" -o cat \
  | jq 'select(.msg=="ingest queued") | {time, url}'
```

### 7.3 Habilitar DEBUG temporariamente

```bash
systemctl --user edit vbmd
# adicionar Environment=VBM_LOG_LEVEL=debug
systemctl --user daemon-reload && systemctl --user restart vbmd

# Depois de investigar, remover a linha e reiniciar.
```

---

## 8. Troubleshooting

### 8.1 Daemon não inicia

```bash
systemctl --user status vbmd
journalctl --user -u vbmd -n 50 -o cat | jq
```

Causas comuns:

| Sintoma | Causa | Fix |
|---|---|---|
| `bind: address already in use` | Porta 7532 em uso | Setar `VBM_PORT` para outra porta e atualizar no popup |
| `database locked` no startup | Outro processo com o DB aberto | `fuser ~/.local/share/vbm/vbm.db` e encerrar |

### 8.2 Extensão mostra erro de conexão

```bash
# Confirmar que daemon está rodando e escutando
curl http://127.0.0.1:7532/healthz
# {"ok":true} = daemon ativo

# Se VBM_PORT foi alterado, verificar qual porta está em uso
ss -tlnp | grep vbmd
```

Se usar porta diferente de 7532, abrir popup da extensão → seção **Daemon** → atualizar o campo de porta e salvar.

### 8.3 Busca retorna sempre os mesmos resultados

- Se `VBM_EMBED_URL` ausente → busca puramente BM25, scores idênticos em queries curtas. Configurar embedder (§4).
- Se embeddings presentes mas scores iguais → verificar `model_ver`:
  ```bash
  sqlite3 ~/.local/share/vbm/vbm.db "SELECT DISTINCT model_ver FROM chunks"
  ```
  Se todos são `stub-v0`, ingestão aconteceu sem embedder real — usar botão **"Re-embed pages"** no popup ou `POST /admin/reindex` (§4.4).

### 8.4 Dashboard externo recebe CORS error

Sintoma: browser dev tools mostram `blocked by CORS policy` em requests para o daemon.

1. Confirmar que `VBM_CORS_ORIGIN` inclui o origin exato do dashboard (protocolo + host + porta, sem trailing slash):
   ```bash
   systemctl --user show vbmd | grep VBM_CORS_ORIGIN
   ```

2. Testar manualmente:
   ```bash
   # Preflight
   curl -i -X OPTIONS "http://127.0.0.1:7532/status" \
     -H "Origin: http://localhost:3000" \
     -H "Access-Control-Request-Method: GET"
   # Esperado: 204 + Access-Control-Allow-Origin: http://localhost:3000

   # Request real
   curl -i "http://127.0.0.1:7532/status" \
     -H "Origin: http://localhost:3000"
   # Esperado: 200 + Access-Control-Allow-Origin: http://localhost:3000
   ```

### 8.5 Timeout do embedder

Logs mostram `embed request timeout` repetidamente:

- Modelo escolhido é grande demais para o hardware. Trocar `VBM_EMBED_MODEL` por `all-minilm`.
- Ollama está processando outras requests. Configurar `OLLAMA_NUM_PARALLEL=1` no ambiente do Ollama.
- Texto dos chunks muito longo. O chunker já limita a 512 tokens mas páginas muito denses podem gerar muitos chunks — monitorar `vbm_queue_pending`.

### 8.6 FTS5 inconsistente após `/forget`

`Forget()` executa rebuild do FTS5 sincronamente (única operação que faz rebuild). Se interrompido no meio, o índice fica parcial. Reparar com:

```bash
sqlite3 ~/.local/share/vbm/vbm.db "INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild');"
```

### 8.7 Banco corrompido

Sintomas: `/healthz` retorna 503, logs mostram `database disk image is malformed`.

```bash
systemctl --user stop vbmd
cd ~/.local/share/vbm
sqlite3 vbm.db ".recover" | sqlite3 vbm_recovered.db
mv vbm.db vbm.db.bad && mv vbm_recovered.db vbm.db
systemctl --user start vbmd
```

---

## 9. Desinstalação limpa

```bash
# 1. Parar e remover serviço
systemctl --user stop vbmd
systemctl --user disable vbmd
rm -f ~/.config/systemd/user/vbmd.service
systemctl --user daemon-reload

# 2. Remover binário
rm -f ~/.local/bin/vbmd

# 3. Remover configurações persistentes
rm -rf ~/.config/vbm/

# 4. Remover dados (LGPD — apagamento completo, opcional)
rm -rf ~/.local/share/vbm/

# 5. Remover extensão do Chrome manualmente em chrome://extensions
```

Alternativa: `cd daemon && make uninstall` (cobre passos 1 e 2; dados permanecem em `~/.local/share/vbm/` intencionalmente para evitar perda acidental).

---

## 10. Referências cruzadas

- `docs/GUIA.md` — guia de usuário final (pt-BR)
- `CLAUDE.md` — instruções arquiteturais e regras de negócio
- `CODEBASE_SNAPSHOT.md` — mapeamento técnico completo
- `DECISIONS.md` — decisões arquiteturais documentadas
- `changes/CR-*.md` — histórico de mudanças aprovadas
- `docs/maturity/` — relatórios de avaliação de maturidade
