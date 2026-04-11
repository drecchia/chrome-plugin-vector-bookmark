# CLAUDE.md

## Visão geral do projeto

Vector Bookmark é um sistema de memória semântica de navegação composto por dois artefatos: uma extensão Chrome MV3 (TypeScript) e um daemon nativo Go (`vbmd`). A extensão captura passivamente páginas em que o usuário passa ≥30s, extrai o texto principal e envia ao daemon via HTTP local. O daemon indexa o conteúdo em SQLite com busca híbrida BM25+vetores e expõe uma API REST local autenticada por token de sessão. Toda a stack roda exclusivamente na máquina do usuário — sem servidores externos em v0.1.

## Stack

| Componente | Tecnologia |
|---|---|
| Daemon | Go 1.22, `chi/v5`, `modernc.org/sqlite` (FTS5, pure Go), `gorilla/websocket`, `google/uuid` |
| Extensão | TypeScript 5.4, Vite 5, CRXJS 2.0-beta, React 18, `@mozilla/readability` |
| Banco | SQLite (WAL mode, FTS5 virtual table, embeddings como BLOB float32) |
| IPC | Chrome Native Messaging (handshake) + localhost HTTP + WebSocket |
| OS | Linux (systemd user unit) — macOS/Windows não suportados em v0.1 |

## Estrutura do projeto

```
/
├── extension/               Chrome MV3 extension
│   ├── manifest.json        Permissões, omnibox, incognito:not_allowed
│   ├── src/background/      service-worker.ts (orquestrador), native-bridge.ts, daemon-client.ts
│   ├── src/content/         extract.ts — dwell tracking + Readability
│   ├── src/lib/             denylist.ts — domínios e URLs bloqueados
│   ├── src/popup/           App.tsx — status, pause, forget
│   └── dist/                build output (não comitar)
├── daemon/                  Go native daemon
│   ├── cmd/vbmd/main.go     Entrypoint: dispatch server vs nm-host
│   ├── internal/nm/         Native Messaging host protocol + session file
│   ├── internal/server/     HTTP router (chi), handlers, auth middleware
│   ├── internal/store/      SQLite: schema, ingest, search (BM25+cosine RRF), forget
│   ├── internal/embed/      Embedder interface + StubEmbedder (zeros)
│   ├── internal/chunk/      Sliding window chunker (512/64/40)
│   ├── internal/queue/      Canal Go bufferizado (cap 256) + worker
│   └── install/             install.sh, vbmd.service, native-messaging-host.json
├── proto/types.ts           Tipos TypeScript compartilhados da API HTTP/WS/NM
├── docs/
│   ├── GUIA.md              Guia de uso em pt-BR
│   └── bootstrap/           PLAN.md + PRD.md de produto (escopo/roadmap)
├── CODEBASE_SNAPSHOT.md     Mapeamento técnico da codebase
└── DECISIONS.md             Decisões arquiteturais documentadas
```

## Padrões obrigatórios

**Go (daemon):**
- Pacotes em `internal/` — nunca expor tipos entre pacotes via interface pública desnecessária
- Erros sempre propagados com `fmt.Errorf("contexto: %w", err)` — sem `log.Fatal` dentro de packages
- Todo handler HTTP retorna JSON; erros retornam `{"error":"mensagem"}` com status apropriado
- Auth middleware aplicado a todas as rotas exceto `/healthz`
- Bind exclusivo em `127.0.0.1` — nunca `0.0.0.0`

**TypeScript (extensão):**
- Tipos da API definidos em `proto/types.ts` — não duplicar em arquivos da extensão
- `daemonState` nunca persiste em `chrome.storage` — apenas memória do SW
- Todo envio ao daemon passa por `connectDaemon()` antes do fetch
- Denylist checada no SW **após** receber `page_viewed` — antes de qualquer chamada ao daemon

**Ambos:**
- Timestamps sempre em Unix milliseconds (int64/number)
- SHA1 hex para hashing de conteúdo (url_hash, text_hash)
- `model_ver` obrigatório em todo registro de chunk — valor atual: `"stub-v0"`

## Regras de negócio críticas

1. **Dwell mínimo**: 30.000ms de tempo visível acumulado (não tempo desde abertura). Rastreado no content script via Page Visibility API.
2. **Dedup de conteúdo**: `sha1(normalize(text))` — chunks com mesmo hash são descartados (`INSERT OR IGNORE`). Texto normalizado = lowercase + collapse whitespace.
3. **Dedup de página**: `sha1(url)` como `url_hash UNIQUE` — revisitas à mesma URL atualizam a página existente.
4. **Chunking**: janela de 512 tokens (whitespace split), overlap 64, mínimo 40 tokens por chunk. Texto < 200 chars é descartado antes do chunking (no content script).
5. **Queue com backpressure**: canal com capacidade 256. Se cheio, ingest é descartado com log — sem bloqueio do caller HTTP.
6. **Busca híbrida RRF**: BM25 via FTS5 + cosine brute-force sobre BLOBs → fusão Reciprocal Rank Fusion k=60. Limite: 5 por default, 20 por máximo.
7. **Segurança de sessão**: token UUID gerado no startup, armazenado em `~/.local/share/vbm/session.json` (chmod 600). Rotado a cada restart do daemon.
8. **Privacidade na captura**: `incognito:not_allowed` no manifest. Denylist de 24 domínios + `.gov`/`.mil` + 14 padrões de URL aplicados no SW.

## O que nunca fazer

- **Nunca** fazer bind do daemon em `0.0.0.0` — somente `127.0.0.1`
- **Nunca** persistir o token de sessão em `chrome.storage` ou qualquer arquivo acessível pela extensão
- **Nunca** adicionar lógica de negócio no `nm-host` mode — ele só lê `session.json` e responde
- **Nunca** remover `incognito: "not_allowed"` do manifest
- **Nunca** usar `log.Fatal` dentro de packages `internal/` — retornar erro para o caller
- **Nunca** fazer FTS5 rebuild síncrono em operações de ingest — apenas em `Forget`
- **Nunca** persistir embeddings sem o campo `model_ver` — é o mecanismo de migração futura
- **Nunca** duplicar tipos do `proto/types.ts` na extensão — importar de lá
- **Nunca** commitar `daemon/bin/`, `extension/dist/`, `extension/node_modules/`, `*.db`, `session.json` (protegidos pelo `.gitignore`)

## Entidades principais

```
pages       id, url, url_hash (UNIQUE), title, domain, visit_ts (ms), dwell_ms, model_ver
chunks      id, page_id (FK→pages CASCADE), chunk_idx, text, text_hash, embedding (BLOB f32), model_ver
chunks_fts  virtual FTS5 sobre chunks.text
queue       id, url, title, text, visit_ts, dwell_ms, domain, status, created_at
```

## Como rodar localmente

```bash
# Daemon
cd daemon
go mod tidy
make build        # gera daemon/bin/vbmd
make install      # instala + inicia via systemd (pede Extension ID)
make run          # dev sem systemd: ./bin/vbmd server

# Extensão
cd extension
npm install
npm run build     # gera extension/dist/
npm run dev       # watch mode

# Verificar daemon
systemctl --user status vbmd
curl http://127.0.0.1:$(python3 -c "import json; print(json.load(open('$HOME/.local/share/vbm/session.json'))['port'])")/healthz
```

Carregar extensão: `chrome://extensions/` → Developer mode → Load unpacked → `extension/dist/`

## Referências

- `DECISIONS.md` — decisões arquiteturais completas com ⚠️ pendências
- `CODEBASE_SNAPSHOT.md` — mapeamento técnico completo (entidades, endpoints, schemas)
- `docs/GUIA.md` — guia de uso em pt-BR
- `docs/bootstrap/PLAN.md` — plano técnico de implementação
- `docs/bootstrap/PRD.md` — requisitos de produto (escopo, roadmap, privacidade)
- `changes/` — histórico de change requests (criar com `mkdir changes`)
