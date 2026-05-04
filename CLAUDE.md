# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Visão geral do projeto

Vector Bookmark é um sistema de memória semântica de navegação composto por dois artefatos: uma extensão Chrome MV3 (TypeScript) e um daemon nativo Go (`vbmd`). A extensão captura passivamente páginas em que o usuário passa ≥10s (configurável), extrai meta tags HTML e envia ao daemon via HTTP local. O daemon indexa o conteúdo em SQLite com busca híbrida BM25+vetores e expõe uma API REST em `127.0.0.1:7532` (padrão). Toda a stack roda exclusivamente na máquina do usuário.

## Stack

| Componente | Tecnologia |
|---|---|
| Daemon | Go 1.22, `chi/v5`, `modernc.org/sqlite` (FTS5, pure Go), `gorilla/websocket` |
| Extensão | TypeScript 5.4, Vite 5, CRXJS 2.0-beta, React 18, `@mozilla/readability` |
| Banco | SQLite (WAL mode, FTS5 virtual table, embeddings como BLOB float32) |
| IPC | localhost HTTP (porta padrão 7532) + WebSocket |

## Comandos

```bash
# Daemon
cd daemon
make build        # compila → daemon/bin/vbmd
make run          # build + executa ./bin/vbmd server
make test         # go test ./...
make install      # instala + inicia via systemd
make tidy         # go mod tidy

# Daemon (Docker — CR-0005)
docker build -t vbmd:dev daemon/
docker run --rm -p 127.0.0.1:7532:7532 \
    -e VBM_BIND=0.0.0.0 \
    -e VBM_DATA_DIR=/data \
    -e VBM_EMBED_URL=… -e VBM_EMBED_API_KEY=… \
    -v vbm-data:/data \
    vbmd:dev

# Extensão
cd extension
npm install
npm run build     # gera extension/dist/
npm run dev       # watch mode (Vite)
npm run typecheck # tsc --noEmit sem gerar arquivos

# Verificar daemon rodando
curl http://127.0.0.1:7532/healthz
curl http://127.0.0.1:7532/status
curl http://127.0.0.1:7532/metrics

# Convenience scripts (raiz do repo — entrypoints reais do dia-a-dia)
./build-linux.sh    # build daemon (linux) + extension
./build-windows.sh  # cross-compile vbmd.exe a partir do WSL
./dev.sh            # build + start daemon, imprime caminho da extensão
./dev.ps1           # equivalente PowerShell para Windows
```

Carregar extensão: `chrome://extensions/` → Developer mode → Load unpacked → `extension/dist/`

## Configuração

Env file carregado no startup do daemon:
- Linux:   `~/.config/vbm/env`
- Windows: `%APPDATA%\vbm\env`

Vars consultadas: `VBM_PORT`, `VBM_BIND`, `VBM_EMBED_URL`, `VBM_EMBED_API_KEY`, `VBM_LLM_MODEL`, `VBM_TTL_DAYS`, `VBM_DATA_DIR`, `VBM_LOG_LEVEL`, `VBM_LLM_PROMPT_SUMMARIZE_FILE`, `VBM_LLM_PROMPT_SUGGEST_TAGS_FILE`. O banner de startup (`logEnvBanner`) imprime todas — manter o slice `envSpecs` em sync ao adicionar uma nova env.

Documentação adicional em `docs/`: `GUIA.md` (uso), `OPERATIONS.md` (operação), `bootstrap/` e `maturity/` (artefatos de processo).

## Arquitetura

### Fluxo de captura passiva (dwell)
```
Content script (extract.ts)
  → acumula tempo visível via Page Visibility API
  → após dwellThreshold (default 10s): envia page_visited {url, title, dwellMs, meta}
Service Worker (service-worker.ts)
  → verifica blacklist do usuário (isBlockedByUser)
  → POST /visit ao daemon
  → badge azul "visit recorded"
```

### Fluxo de index manual (popup)

O popup tem **um único botão** "Index this site now". Clicá-lo abre um painel inline com:
- Campo de **tags separadas por vírgula** (pré-preenchido com as tags atuais quando a página já está indexada — operação em **set-mode**: a lista enviada é o estado final).
- Seletor de **modo de indexação** (4 opções, ver abaixo).
- (condicional) `<textarea>` quando o modo é `manual`.
- Botão **Confirm** que dispara `popup_force_index` com `{tags, mode, manualText?}`.

Modos suportados (campo `mode` em `IngestRequest`):
1. `full_text` (default): SW envia `force_extract` → Readability + meta concatenados → `/ingest` chunka tudo.
2. `llm_summary`: pipeline igual ao full_text **até** o daemon, que então chama `internal/llm.Summarize()` (provider OpenAI-compat reusando `VBM_EMBED_URL`/`VBM_EMBED_API_KEY` + `VBM_LLM_MODEL`) e indexa apenas o resumo.
3. `manual`: SW dispara `force_extract` com intent `manual` (CR-0006) — content script extrai apenas título+meta e emite `page_viewed` com `text=""`. SW concatena `metaText + "\n\n" + manualText` antes do /ingest. Resultado: chunk indexado contém título, meta block, e o que o usuário digitou — simétrico com os outros modos.
4. `meta_only`: SW envia apenas título + meta tags (description/keywords/og*/author), sem corpo.

Além desses 4 modos do daemon, o popup oferece **3 intents de extração** (CR-0002) que rodam **no client** e mapeiam para `mode: "manual"` no `/ingest`:
- `selection` — `window.getSelection()` no content script. Erro "Nothing selected" se vazio.
- `yt_transcript` — abre o painel de transcript do YouTube e raspa `ytd-transcript-segment-renderer .segment-text`. Visível apenas em `youtube.com/watch`. Erro se o vídeo não tem CC.
- `yt_comments` — lê os top-50 `ytd-comment-thread-renderer #content-text` já carregados no DOM (sem auto-scroll). Visível apenas em `youtube.com/watch`.

Quando `intent` está presente em `popup_force_index`, o SW força `mode: "manual"` no /ingest porque o texto já vem pronto do content script. Em todos os modos, o badge fica verde após o /ingest retornar 202.

**Sugestão de tags via LLM (CR-0003):** o input CSV de tags tem um botão ✨ ao lado. Click → `popup_suggest_tags` → SW envia `force_extract` com `intent: 'suggest_tags'` (que extrai mas **não emite `page_viewed`** — devolve payload via `sendResponse`) → SW chama `POST /tags/suggest` → daemon roda `internal/llm.SuggestTags` recebendo a lista de tags existentes como contexto e devolve até 3 tags → popup faz merge dedup-ado no input. Não toca badge nem ingest.

### Badge state machine (service-worker.ts)
Estados: `tracking(0) < disconnected(1) < blocked(2) < visited(3) < indexed(4)`

`setBadge` nunca faz downgrade de um estado para prioridade menor. O estado é resetado em cada nova navegação (`onUpdated status=loading`). Isso evita que `dwell_started` sobreescreva um badge `visited` em revisitas.

### Daemon (Go)
- `cmd/vbmd/main.go` — entrypoint, modo `server`
- `internal/server/routes.go` — todos os handlers HTTP (chi router)
- `internal/store/sqlite.go` — schema, migrations, Ingest, RecordVisit, Search (BM25+cosine RRF), Forget, ListTags/ListPagesByTag/GetPageTags
- `internal/chunk/chunk.go` — sliding window chunker (512 tokens, overlap 64, mínimo 40)
- `internal/embed/` — interface Embedder + StubEmbedder (zeros para dev)
- `internal/llm/` — cliente OpenAI-compat para chat completions: `Summarize()` (resumo de página, CR-0001) e `SuggestTags()` (até 3 tags reusando taxonomia existente, CR-0003); usa o mesmo `VBM_EMBED_URL`/`VBM_EMBED_API_KEY` do embedder, modelo via `VBM_LLM_MODEL`. **Prompts externalizáveis** via `VBM_LLM_PROMPT_SUMMARIZE_FILE` e `VBM_LLM_PROMPT_SUGGEST_TAGS_FILE` (markdown, lidos no startup, fallback embedded — CR-0006).
- `internal/queue/queue.go` — canal bufferizado cap 256, worker de ingest

### Extensão (TypeScript)
- `src/content/extract.ts` — dwell tracking, extractMeta(), force extract via Readability + extractores específicos: `extractSelection`, `extractYouTubeTranscript`, `extractYouTubeComments` (CR-0002)
- `src/background/service-worker.ts` — orquestrador: badge, mensagens, blacklist
- `src/background/daemon-client.ts` — todas as chamadas HTTP ao daemon
- `src/background/native-bridge.ts` — config storage (host, port, dwell threshold)
- `src/background/default-blacklist.json` — blacklist padrão semeada no daemon (apenas padrões local/private IP)
- `src/popup/App.tsx` — UI React inline-styled (sem CSS externo)
- `proto/types.ts` — tipos compartilhados da API HTTP (importar daqui, nunca duplicar)

## Padrões obrigatórios

**Go (daemon):**
- Pacotes em `internal/` — nunca expor tipos entre pacotes via interface pública desnecessária
- Erros sempre propagados com `fmt.Errorf("contexto: %w", err)` — sem `log.Fatal` dentro de packages
- Todo handler HTTP retorna JSON; erros retornam `{"error":"mensagem"}` com status apropriado
- Bind exclusivo em `127.0.0.1` — nunca `0.0.0.0`
- `internal/llm` é opcional: só inicializa se `VBM_EMBED_URL` estiver setado. Sem ele, `mode=llm_summary` retorna 503.
- Variáveis de ambiente consultadas pelo daemon são listadas no banner do startup (`logEnvBanner` em `server.go`). Ao adicionar uma env nova, atualizar o slice `envSpecs` para mantê-la visível.
- `VBM_DATA_DIR` (CR-0005) sobrescreve a resolução de `nm.DataDir()`. Usado em containers (distroless não tem `$HOME`). Sem ele, fallback para `~/.local/share/vbm` (Linux/Mac) ou `%APPDATA%\vbm` (Windows).

**TypeScript (extensão):**
- Tipos da API definidos em `proto/types.ts` — não duplicar em arquivos da extensão
- Config do daemon (`host`/`port`) lida de `chrome.storage.local` via `getDaemonConfig()` em `native-bridge.ts`
- Blacklist checada no SW **após** receber `page_visited` via `isBlockedByUser(hostname)`

**Ambos:**
- Timestamps sempre em Unix milliseconds (int64/number)
- SHA1 hex para hashing de conteúdo (url_hash, text_hash)
- `model_ver` obrigatório em todo registro de chunk — valor atual: `"stub-v0"`

## Regras de negócio críticas

1. **Dwell mínimo**: 10.000ms de tempo visível acumulado (não tempo de clock). Configurável via popup → salvo em `chrome.storage.local` como `vbmDwellMs`. Content script escuta `storage.onChanged` para hot-update sem reload.
2. **Blacklist**: apenas a blacklist gerenciada pelo usuário (tabela `blacklist` no daemon). Não há denylist estática. Default seeds: apenas padrões de IP local/privado.
3. **Dedup de conteúdo**: `sha1(normalize(text))` — chunks com mesmo hash são descartados (`INSERT OR IGNORE`).
4. **Dedup de página**: `sha1(url)` como `url_hash UNIQUE` — revisitas atualizam a página existente.
5. **Chunking**: janela 512 tokens, overlap 64, mínimo 40 tokens. Texto < 200 chars descartado no content script.
6. **Queue com backpressure**: canal cap 256. Se cheio, ingest descartado com log.
7. **Busca híbrida RRF**: BM25 via FTS5 + cosine brute-force → RRF k=60. Limite: 20 default, 1000 max (clampado, mínimo 1). UI da aba Search expõe input numérico "Max results". Filtro opcional `?tag=…` restringe candidatos via `page_tags`.
8. **Modos de ingest**: `full_text` (Readability + meta) | `llm_summary` (resumo via LLM substitui o texto antes do chunking) | `manual` (texto fornecido pelo popup) | `meta_only` (apenas título + meta tags). Default = `full_text`. Em todos os modos, meta + título são prefixados ao corpo, exceto `meta_only` que é apenas o bloco de meta.
9. **Tags em set-mode**: quando o popup envia ingest com `setTags=true`, a lista de tags vira o estado final daquela página (DELETE + INSERT na mesma tx). `setTags=false`/ausente = merge (INSERT OR IGNORE). Tags são normalizadas para `[a-z0-9 \-_]`, max 64 chars.

## Entidades principais (SQLite)

```
pages       id, url, url_hash (UNIQUE), title, domain, visit_ts (ms), dwell_ms, model_ver, indexed, meta_json, created_at
chunks      id, page_id (FK→pages CASCADE), chunk_idx, text, text_hash, embedding (BLOB f32), model_ver
chunks_fts  virtual FTS5 sobre chunks.text (porter stemmer)
queue       id, url, title, text, visit_ts, dwell_ms, domain, status, created_at, updated_at
blacklist   pattern (PRIMARY KEY), created_at
page_tags   page_id (FK→pages CASCADE), tag, created_at — PK (page_id, tag); index em tag
```

Migrações vivem em `internal/store/sqlite.go` (slice `migrations`, gravadas em `schema_versions`). Esquema atual está em V7. Sempre adicionar nova migração em vez de editar uma existente.

## O que nunca fazer

- **Default** do bind do daemon é `127.0.0.1`. Bind em `0.0.0.0` só é aceitável **dentro de container Docker**, e nesse caso o port mapping do host **deve** estar restrito a loopback (ex.: `-p 127.0.0.1:7532:7532`). Nunca expor a porta do container em interface pública sem auth.
- **Nunca** remover `incognito: "not_allowed"` do manifest
- **Nunca** usar `log.Fatal` dentro de packages `internal/` — retornar erro para o caller
- **Nunca** fazer FTS5 rebuild síncrono em operações de ingest — apenas em `Forget`
- **Nunca** persistir embeddings sem o campo `model_ver`
- **Nunca** duplicar tipos do `proto/types.ts` na extensão
- **Nunca** commitar `daemon/bin/`, `extension/dist/`, `extension/node_modules/`, `*.db`

## Workflow de mudanças (CR + DECISIONS)

- Toda mudança não-trivial vira um arquivo `changes/CR-NNNN.md` (atual: 0001-0006). CRs descrevem o **quê** e o **como** da entrega.
- Decisões arquiteturais com **o quê / por quê / quando** vão em `DECISIONS.md` (D-001, D-002, ...). Antes de reverter ou contradizer um padrão, ler a entrada D-NNN correspondente — ela explica por que aquilo está como está.
- Use o skill `change-request` (`/cr` ou `/change`) para criar/editar CRs neste projeto — ele segue o template existente.
