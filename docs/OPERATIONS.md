# Vector Bookmark — Runbook Operacional (pt-BR)

Runbook para quem **opera** ou **integra** o Vector Bookmark. Para guia de usuário final, veja `docs/GUIA.md`.

Este documento cobre variáveis de ambiente, setup de busca semântica real (Ollama), retenção de dados (LGPD), observabilidade com Prometheus, logs estruturados e troubleshooting.

---

## 1. Pré-requisitos

| Componente | Mínimo | Observação |
|---|---|---|
| SO | Linux (kernel 5.x+) | macOS/Windows não suportados em v0.1 |
| Init | systemd user sessions | `loginctl enable-linger $USER` para persistir após logout |
| Go | 1.22+ | Apenas para build — runtime é estático |
| Node.js | 20+ | Build da extensão |
| Chrome/Chromium | estável | Manifest V3 |
| Ollama *(opcional)* | 0.1.x+ | Para busca semântica real; sem ele o daemon usa `StubEmbedder` (zeros) |

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

# 3. Carregar extensão em chrome://extensions → copiar Extension ID

# 4. Instalar daemon + registrar NM host + systemd unit
cd ../daemon && make install
# O script pede o Extension ID e faz:
# - install -Dm755 bin/vbmd ~/.local/bin/vbmd
# - cria ~/.config/google-chrome/NativeMessagingHosts/com.vbm.daemon.json
# - cria ~/.config/systemd/user/vbmd.service
# - systemctl --user daemon-reload && systemctl --user enable --now vbmd

# 5. Sanidade
systemctl --user status vbmd
PORT=$(jq -r .port ~/.local/share/vbm/session.json)
curl -s http://127.0.0.1:$PORT/healthz    # {"ok":true}
curl -s http://127.0.0.1:$PORT/metrics    # texto Prometheus
```

**Checklist pós-instalação:**
- [ ] `~/.local/share/vbm/session.json` com chmod 600
- [ ] `vbmd.service` rodando, sem restart loops em `systemctl --user status`
- [ ] `/healthz` retorna `{"ok":true}` (não `database unavailable`)
- [ ] Ícone da extensão no Chrome mostra "Conectado ao daemon"
- [ ] Nenhum processo escutando fora de `127.0.0.1` — `ss -tlnp | grep vbmd` deve mostrar apenas loopback

---

## 3. Variáveis de ambiente

Todas as variáveis são lidas no startup do daemon (`daemon/internal/server/server.go` e `daemon/cmd/vbmd/main.go`). Use `systemctl --user edit vbmd` para setar via `Environment=KEY=value`.

| Variável | Default | Obrigatória? | Descrição |
|---|---|---|---|
| `VBM_PORT` | aleatória | Não | Porta fixa. Se ausente, kernel escolhe porta livre e grava em `session.json`. |
| `VBM_BIND` | `127.0.0.1` | Não | Interface de bind. **Nunca usar `0.0.0.0`** em host compartilhado. Apenas Docker com rede isolada deve alterar. |
| `VBM_EMBED_URL` | *(vazio → stub)* | Opcional | Endpoint Ollama-compat. Sem ele, `StubEmbedder` produz vetores zero e a busca cai para BM25 puro. |
| `VBM_EMBED_MODEL` | `nomic-embed-text` | Não | Modelo de embedding usado nas requests ao Ollama. |
| `VBM_TTL_DAYS` | *(sem retenção)* | Recomendada | Retenção LGPD: páginas com `visit_ts` mais antigo que N dias são removidas por um ticker 24h. |
| `VBM_LOG_LEVEL` | `info` | Não | `debug` / `info` / `warn` / `error`. Handler é slog JSON em stderr. |
| `VBM_CORS_ORIGIN` | *(vazio)* | Condicional | CSV de origens extras (além de `chrome-extension://*`) aceitas para requisições HTTP. Necessário para dashboards externos. |

**Exemplo de override via systemd drop-in:**

```bash
systemctl --user edit vbmd
```

```ini
[Service]
Environment=VBM_EMBED_URL=http://127.0.0.1:11434/api/embeddings
Environment=VBM_EMBED_MODEL=nomic-embed-text
Environment=VBM_TTL_DAYS=90
Environment=VBM_LOG_LEVEL=info
Environment=VBM_CORS_ORIGIN=http://localhost:3000,http://localhost:8080
```

Salvar, depois `systemctl --user daemon-reload && systemctl --user restart vbmd`.

---

## 4. Setup de busca semântica com Ollama

Sem `VBM_EMBED_URL`, o daemon usa `StubEmbedder` (vetores zero) e a busca fica dependente só de BM25 FTS5. Para ativar a fusão real BM25 + cosine:

### 4.1 Instalar Ollama

```bash
# Linux — instalador oficial
curl -fsSL https://ollama.com/install.sh | sh

# Garantir que o serviço está rodando
systemctl status ollama            # serviço system-wide
# ou
ollama serve &                      # foreground para teste
```

### 4.2 Baixar o modelo de embedding

```bash
ollama pull nomic-embed-text        # 274 MB, 768 dimensões
```

Modelos alternativos suportados (contanto que exponham `/api/embeddings`):
- `mxbai-embed-large` — 669 MB, 1024d (maior qualidade, mais lento)
- `all-minilm` — 23 MB, 384d (leve, menor qualidade)

### 4.3 Configurar o daemon

```bash
systemctl --user edit vbmd
```

```ini
[Service]
Environment=VBM_EMBED_URL=http://127.0.0.1:11434/api/embeddings
Environment=VBM_EMBED_MODEL=nomic-embed-text
```

```bash
systemctl --user daemon-reload && systemctl --user restart vbmd
```

### 4.4 Formato do protocolo

O `HttpEmbedder` faz POST ao endpoint configurado com:

```json
{
  "model": "nomic-embed-text",
  "prompt": "texto a ser vetorizado"
}
```

E espera resposta:

```json
{ "embedding": [0.123, -0.456, ...] }
```

Timeout do cliente: **10s**. Se o modelo escolhido demora mais que isso, o chunk entra no banco com vetor zero e a busca degrada para BM25 puro nesse documento.

### 4.5 Verificar

```bash
PORT=$(jq -r .port ~/.local/share/vbm/session.json)
TOKEN=$(jq -r .token ~/.local/share/vbm/session.json)

# Forçar ingest manual (via curl) ou aguardar captura real
# Depois:
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:$PORT/search?q=termo+semantico&limit=5" | jq
```

Se o campo `score` dos resultados varia entre documentos, a busca vetorial está ativa. Se todos retornam score idêntico, o `StubEmbedder` ainda está em uso — checar `journalctl --user -u vbmd | grep embed`.

### 4.6 Migração de modelo

Mudar `VBM_EMBED_MODEL` não re-vetoriza os chunks antigos — o campo `model_ver` em cada chunk preserva a versão original. Re-vetorização futura está planejada via ferramenta offline; por ora, para trocar modelo com vetores consistentes:

```bash
systemctl --user stop vbmd
rm ~/.local/share/vbm/vbm.db       # ⚠️ apaga histórico
systemctl --user start vbmd
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
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:$PORT/export" > export.json
```

Retorna todas as páginas em JSON plano (sem embeddings). Útil para auditoria de compliance e para comprovar direito de acesso.

### 5.3 Esquecimento manual

```bash
# Por URL
curl -s -X DELETE -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"type":"url","value":"https://exemplo.com/pagina"}' \
  "http://127.0.0.1:$PORT/forget"

# Por domínio
curl -s -X DELETE -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"type":"domain","value":"exemplo.com"}' \
  "http://127.0.0.1:$PORT/forget"

# Por intervalo (Unix ms)
curl -s -X DELETE -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"type":"timerange","value":"1700000000000:1710000000000"}' \
  "http://127.0.0.1:$PORT/forget"
```

Remoção é **física** — `DELETE` + FTS5 rebuild em `Forget()` (único lugar onde o rebuild acontece, nunca em `/ingest`).

---

## 6. Observabilidade

### 6.1 Endpoints

| Endpoint | Auth | Uso |
|---|---|---|
| `GET /healthz` | Não | Liveness probe. Testa conectividade DB com `Ping()`. Retorna 503 se DB indisponível. |
| `GET /metrics` | Não | Prometheus text format. Usado para scrape externo. |
| `GET /status` | Sim | JSON com `indexed`, `pending`, `version`. Usado pela extensão e pela UI. |

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
      - targets: ['127.0.0.1:7532']   # ajustar para VBM_PORT ou ler de session.json
        labels:
          instance: laptop
```

Se `VBM_PORT` é dinâmico (default), use um wrapper que lê `session.json` e atualiza `file_sd_configs`:

```bash
jq -n --arg port "$(jq -r .port ~/.local/share/vbm/session.json)" \
  '[{targets:["127.0.0.1:\($port)"],labels:{instance:"laptop"}}]' \
  > /var/lib/prometheus/vbm_targets.json
```

```yaml
  - job_name: vbm_daemon
    file_sd_configs:
      - files: ['/var/lib/prometheus/vbm_targets.json']
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
| `bind: address already in use` | `VBM_PORT` ocupado | Remover `VBM_PORT` (deixar dinâmico) ou escolher outra porta |
| `database locked` no startup | Outro processo com o DB aberto | `fuser ~/.local/share/vbm/vbm.db` e encerrar |
| `session.json` com chmod errado | Instalação corrompida | `chmod 600 ~/.local/share/vbm/session.json` |
| `no such file or directory: session.json` | Primeira execução | Normal — será criado pelo startup |

### 8.2 Extensão mostra "Daemon não conectado"

```bash
# Verificar handshake NM
cat ~/.config/google-chrome/NativeMessagingHosts/com.vbm.daemon.json
# allowed_origins deve conter chrome-extension://SEU_ID/

# Testar binário NM manualmente
echo -n '' | ~/.local/bin/vbmd nm-host
# Deve retornar erro de handshake (ok) — se der "command not found", reinstalar
```

### 8.3 Busca retorna sempre os mesmos resultados

- Se `VBM_EMBED_URL` ausente → busca puramente BM25, scores idênticos em queries curtas. Configurar Ollama (§4).
- Se embeddings presentes mas scores iguais → verificar `model_ver` dos chunks: `sqlite3 ~/.local/share/vbm/vbm.db "SELECT DISTINCT model_ver FROM chunks"`. Se todos são `stub-v0`, a ingestão aconteceu sem Ollama — apagar DB ou esperar re-ingest.

### 8.4 Dashboard externo recebe CORS error

Sintoma: browser dev tools mostram `blocked by CORS policy` OU `Failed to fetch` em requests para o daemon.

1. Confirmar que `VBM_CORS_ORIGIN` inclui o origin exato do dashboard (protocolo + host + porta, sem trailing slash):
   ```bash
   systemctl --user show vbmd | grep VBM_CORS_ORIGIN
   ```

2. Confirmar que o fix do CORS middleware ordering está aplicado (CR-006). Sem o fix, `VBM_CORS_ORIGIN` é silenciosamente inoperante — preflights funcionam mas requisições reais com token inválido retornam 401 sem headers CORS, confundindo o browser.

3. Testar manualmente:
   ```bash
   PORT=$(jq -r .port ~/.local/share/vbm/session.json)
   TOKEN=$(jq -r .token ~/.local/share/vbm/session.json)

   # Preflight
   curl -i -X OPTIONS "http://127.0.0.1:$PORT/status" \
     -H "Origin: http://localhost:3000" \
     -H "Access-Control-Request-Method: GET"
   # Esperado: 204 + Access-Control-Allow-Origin: http://localhost:3000

   # Request real
   curl -i "http://127.0.0.1:$PORT/status" \
     -H "Origin: http://localhost:3000" \
     -H "Authorization: Bearer $TOKEN"
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
rm ~/.config/systemd/user/vbmd.service
systemctl --user daemon-reload

# 2. Remover binário e NM host
rm ~/.local/bin/vbmd
rm ~/.config/google-chrome/NativeMessagingHosts/com.vbm.daemon.json

# 3. Remover dados (LGPD — apagamento completo)
rm -rf ~/.local/share/vbm/

# 4. Remover extensão do Chrome manualmente em chrome://extensions
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
