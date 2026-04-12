# DECISIONS.md
## Decisões arquiteturais — gerado por bootstrap em 2026-04-11
## Fonte: código existente — revisar e complementar conforme necessário

---

## Stack

| Camada | Escolha | Justificativa observada no código |
|--------|---------|----------------------------------|
| Daemon | Go 1.22 | Single static binary, cross-compile, sem CGO |
| HTTP router | `chi/v5` | Leve, idiomático, middleware composable |
| Banco de dados | `modernc.org/sqlite` (pure Go) | Sem CGO, FTS5 incluso, arquivo único |
| WebSocket | `gorilla/websocket` | Maturidade, API simples |
| UUID | `google/uuid` | Geração de tokens de sessão |
| Logging | `log/slog` (stdlib Go 1.21+) | JSON estruturado, sem dependência externa; nível via `VBM_LOG_LEVEL` |
| Extensão | TypeScript 5.4 + Vite 5 + CRXJS | Bundle MV3 com hot-reload em dev |
| UI popup | React 18 | Componentes reativos, sem dependências extras |
| Extração HTML | `@mozilla/readability` | Padrão da indústria para extração de artigo |

## Suporte a plataformas (CR-007)

| OS | Suportado | Data dir | Config file | Auto-start |
|---|---|---|---|---|
| Linux | Sim (v0.1+) | `~/.local/share/vbm/` | `~/.config/vbm/env` via systemd `EnvironmentFile` | systemd user unit |
| Windows 10/11 | Sim (CR-007) | `%APPDATA%\vbm\` | `%APPDATA%\vbm\env` carregado por `loadEnvFile()` em `main.go` | Task Scheduler (sem admin) |
| macOS | Pendente | — | — | — |

**Decisão de paths:** usar `runtime.GOOS` em `nm.DataDir()` para retornar o diretório correto por plataforma. Linux usa `~/.local/share/vbm/` (XDG); Windows usa `%APPDATA%\vbm\` (AppData roaming). Ambos são resolvidos em `internal/nm/host.go` — ponto único de verdade consumido por `nm.SessionPath()` e `server.Run()`.

---

## Padrões globais observados

- **IDs**: `INTEGER PRIMARY KEY AUTOINCREMENT` no SQLite; sem UUID como PK de tabela
- **Timestamps**: Unix milliseconds (int64) — `visitTs`, `created_at`, `updated_at`
- **Sem soft delete**: `DELETE` físico; tombstones não implementados (⚠️ ver abaixo)
- **Sem multi-tenant**: single-user implícito — sem campo `user_id` em nenhuma tabela
- **Dedup por hash**: SHA1 de conteúdo normalizado, não por URL — mesma página em URLs diferentes pode ser indexada duas vezes
- **Model versioning**: campo `model_ver` em `chunks` e `pages` — preparado para migração de embedder
- **Configuração hardcoded**: sem variáveis de ambiente; thresholds (dwell, chunking) são constantes Go e TS

---

## DOMÍNIO 1 — Daemon (Go)

**Entidades:**

```
pages
  id          INTEGER PK AUTOINCREMENT
  url         TEXT NOT NULL
  url_hash    TEXT NOT NULL UNIQUE   -- sha1(url), chave de dedup por URL
  title       TEXT DEFAULT ''
  domain      TEXT DEFAULT ''
  visit_ts    INTEGER NOT NULL       -- Unix ms
  dwell_ms    INTEGER DEFAULT 0
  model_ver   TEXT DEFAULT 'stub-v0'
  created_at  INTEGER NOT NULL

chunks
  id          INTEGER PK AUTOINCREMENT
  page_id     INTEGER FK→pages(id) ON DELETE CASCADE
  chunk_idx   INTEGER NOT NULL
  text        TEXT NOT NULL
  text_hash   TEXT NOT NULL          -- sha1(normalize(text))
  embedding   BLOB                   -- float32 LE, 384 dims (stub = zeros)
  model_ver   TEXT DEFAULT 'stub-v0'
  UNIQUE(page_id, chunk_idx)

chunks_fts    VIRTUAL TABLE fts5
  text        (content='chunks', content_rowid='id')

queue
  id          INTEGER PK AUTOINCREMENT
  url, title, text, visit_ts, dwell_ms, domain
  status      TEXT DEFAULT 'pending'
  created_at  INTEGER NOT NULL
  updated_at  INTEGER
```

**Regras de negócio:**

- Ingest nunca bloqueia o caller HTTP — enfileira no canal (cap 256), worker consome em background
- Chunks com `text_hash` duplicado são descartados silenciosamente (`INSERT OR IGNORE`)
- Busca retorna máximo 20 resultados (limitado na rota); default 5
- Busca híbrida: BM25 (FTS5) + cosine similarity brute-force → RRF k=60
- Embedder atual (`stub-v0`) retorna vetor zero → busca densa é no-op; resultado depende 100% de BM25
- Daemon faz bind exclusivamente em `127.0.0.1` — recusa qualquer outra interface
- Token de sessão é UUID v4 gerado a cada startup do server; rotado implicitamente em restart
- `session.json` é removido no shutdown (defer); se daemon morrer abruptamente, arquivo fica stale

**Decisões observadas:**

- **Separação de modos no binário**: `vbmd server` vs `vbmd nm-host` — mesmo binário, dois comportamentos. NM host é efêmero (executa e sai), server é persistente (systemd).
- **Queue in-memory**: `queue.Queue` é um canal Go, não persiste no SQLite (a tabela `queue` existe mas o worker atual não a usa). Se o daemon reiniciar com itens no canal, eles são perdidos.
- **FTS5 rebuild explícito**: `INSERT INTO chunks_fts(...) VALUES('rebuild')` chamado após `Forget` — operação síncrona e bloqueante.
- **CORS configurável**: por padrão só aceita `Origin: chrome-extension://`. Origens adicionais (dashboard local, ferramentas de dev) configuradas via `VBM_CORS_ORIGIN=http://host1,http://host2`. Tanto `authMiddleware` quanto `corsMiddleware` verificam a lista — bug de ordering onde auth rejeitava antes de cors agir foi corrigido em CR-005.
- **Logging estruturado**: `log/slog` com `JSONHandler` para stderr — configurado em `main.go` antes de `server.Run()`. Nível padrão `INFO`; `VBM_LOG_LEVEL=debug` ativa nível `DEBUG`. Binário `nm-host` não inicializa slog (saída stderr seria texto puro, stdout reservado para protocolo NM binário).
- **UI placeholder**: `/ui/*` retorna HTML estático hardcoded na rota; sem sistema de templates, sem `go:embed` real ainda.
- **Port aleatório**: `net.Listen("tcp", "127.0.0.1:0")` — porta muda a cada restart, descoberta via `session.json`.

⚠️ DECISÕES NÃO CLARAS (revisar):

- Tombstones mencionados no PRD como requisito de `Forget` mas não implementados — `DELETE` é físico. Se sincronização cruzada for adicionada (v0.3), tombstones serão necessários.
- `model_ver` em `pages` e `chunks` são definidos como `DEFAULT 'stub-v0'` mas não há lógica de re-embed quando o valor muda — pipeline de migração ainda não implementado.
- `updated_at` na tabela `queue` nunca é atualizado pelo código atual.
- `SearchResponse.total` em `proto/types.ts` não tem campo correspondente retornado pela rota `/search` — o handler retorna `{results:[...]}` sem `total`.

---

## DOMÍNIO 2 — Chrome Extension (TypeScript)

**Entidades (estado em memória):**

```typescript
DaemonState   { port: number | null, token: string | null }
// Vive apenas na memória do Service Worker — resetado se SW morrer
```

**Regras de negócio:**

- Threshold de dwell: **30.000ms** de tempo visível — não tempo total desde navegação
- `sent` flag por instância de content script — uma página é enviada no máximo uma vez por carregamento
- Denylist aplicada no service worker após receber `page_viewed` — conteúdo já foi extraído antes da verificação
- Texto mínimo: `article.textContent.length < 200` cancela envio ao daemon
- Omnibox só sugere com query ≥ 2 caracteres

**Decisões observadas:**

- **Dwell rastreado no content script, não no SW**: evita o problema de morte do Service Worker MV3 (30s idle). O SW apenas recebe o evento final.
- **`daemonState` em memória do SW**: token nunca vai para `chrome.storage` — mais seguro, mas perdido se SW hibernar. Próxima chamada refaz o handshake NM automaticamente via `connectDaemon()`.
- **`incognito: "not_allowed"`**: proteção de privacidade garantida no manifest, não em código.
- **Opt-in de domínio**: o código em `service-worker.ts` tem comentário de "auto-opt-in" para MVP — o prompt real de confirmação por domínio não está implementado ainda.
- **Badge de feedback**: ícone pisca vermelho por 2s após ingest — único feedback visual de que a página foi capturada.

⚠️ DECISÕES NÃO CLARAS (revisar):

- **Opt-in por domínio não implementado**: o PRD e o GUIA descrevem confirmação por eTLD+1, mas o código atual faz `setOptIn(domain, true)` automaticamente. Decisão pendente: exibir prompt ou capturar silenciosamente.
- **Denylist aplicada depois da extração**: o content script extrai o texto antes do SW checar o denylist. Para domínios sensíveis que passarem pela verificação de URL (ex: domínio novo), o texto já foi lido do DOM. Avaliar se a checagem deve acontecer no content script também.
- **`chrome.storage` não usado para nada**: declarado nas permissões mas sem leitura/escrita no código atual.
- **`idle` permission declarada mas não usada**: `chrome.idle` está nas permissões mas não há chamada no código.

---

## DOMÍNIO 3 — IPC / Protocolo

**Decisões observadas:**

- **HTTP direto (sem Native Messaging)**: extensão conecta diretamente em `http://127.0.0.1:7532` (porta padrão). Sem handshake NM, sem token de sessão, sem manifesto de NM host. Porta e host configuráveis pelo usuário no popup → `chrome.storage.local`.
- **Porta padrão 7532**: daemon escuta em `127.0.0.1:7532` por default; override via `VBM_PORT`. Se mudado, usuário atualiza no popup.
- **Sem autenticação por token**: segurança é o bind exclusivo em `127.0.0.1` — apenas processos locais conseguem conectar. Token de sessão removido em CR-008.
- **CORS configurável**: `chrome-extension://` aceito por padrão. Origens extras via `VBM_CORS_ORIGIN` (CSV).
- **Sem mTLS, sem TLS**: canal local, sem criptografia. Aceitável para loopback; não aceitável se daemon eventualmente expor rede.
- **WebSocket push unidirecional**: daemon envia status a cada 5s; extensão não envia comandos via WS.
