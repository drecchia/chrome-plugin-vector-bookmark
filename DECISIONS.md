# DECISIONS.md

Decisões arquiteturais e de produto do projeto Vector Bookmark. Cada entrada
registra **o quê**, **por quê** e **quando**, para que decisões antigas não
sejam revertidas por engano em CRs futuros.

---

## D-001 — Tags em set-mode no ingest com `setTags=true`

**Data:** 2026-05-02 — CR-0001

Ingest aceita um flag `setTags` que controla a semântica de escrita de tags:

- `setTags=true` → a lista enviada é o estado final da página. Daemon faz
  `DELETE FROM page_tags WHERE page_id=? AND tag NOT IN (…)` seguido de
  `INSERT OR IGNORE` para as tags da lista, tudo dentro da mesma transação.
- `setTags=false`/ausente → merge (INSERT OR IGNORE apenas), preserva tags já
  gravadas.

**Por quê:** o popup precisa permitir ao usuário **remover** tags em re-indexes.
Acumular sem remoção dependeria de um endpoint separado para apagar tag por
tag e desalinharia a UI ("o campo CSV mostra X, mas ao salvar fica X+Y").
Set-mode mantém o popup como fonte da verdade durante o submit; merge-mode
fica disponível para outros consumidores que queiram comportamento aditivo
(scripts de import, etc.).

## D-002 — Único provider OpenAI-compat para embeddings + LLM

**Data:** 2026-05-02 — CR-0001

`internal/llm` reusa `VBM_EMBED_URL` e `VBM_EMBED_API_KEY`. A URL de chat é
derivada substituindo o sufixo `/embeddings` por `/chat/completions`. O modelo
de chat é controlado por `VBM_LLM_MODEL` (default `gpt-4o-mini`).

**Por quê:** evita uma segunda triple de env vars (`VBM_LLM_URL`/`KEY`/etc.)
quando 100% dos providers OpenAI-compat suportados (OpenAI, OpenRouter,
Ollama com `/v1`) já expõem ambos os endpoints sob a mesma base. Quem quiser
LLM diferente (Ollama local pra resumo + OpenAI pra embedding, p.ex.) pode
ainda assim setar `VBM_EMBED_URL` apontando pro provider que tem ambos, ou
abrir um CR para separar.

## D-003 — Modos de ingest expostos como enum no client

**Data:** 2026-05-02 — CR-0001

O modo de extração é decidido pelo popup, **não** pelo daemon, via campo
`mode` em `IngestRequest`:

- `full_text` (default) — Readability + meta concatenados.
- `llm_summary` — daemon chama LLM e indexa o resumo (substitui o texto).
- `manual` — popup envia o texto direto, content script é pulado.
- `meta_only` — apenas título + meta tags, sem corpo.

**Por quê:** dá controle ao usuário (ruído de boilerplate, custo, foco) sem
introduzir heurísticas no daemon. O daemon só dispatcha — e devolve 503 quando
`llm_summary` é pedido sem `VBM_EMBED_URL` configurado.

## D-004 — Extração específica de site fica no client; daemon permanece agnóstico

**Data:** 2026-05-02 — CR-0002

Os "modos" `selection`, `yt_transcript` e `yt_comments` rodam inteiramente no
content script da extensão. O daemon recebe `mode: "manual"` com o texto já
pronto. A mensagem entre popup e SW carrega um campo separado `intent`
(`ExtractIntent`) que o SW propaga ao content script via `force_extract`.

**Por quê:** scrapping de DOM é frágil (YouTube muda seletores), específico de
site, e depende de APIs do navegador (`window.getSelection()`) que o daemon não
tem. Manter isso no client significa: (1) daemon continua simples e estável;
(2) novos sites/intents podem ser adicionados editando só o content script;
(3) a API HTTP `/ingest` não cresce indefinidamente. Custo: o popup tem uma
camada extra de tradução (intent → mode=manual no envio), formalizada via
constante `INTENT_SET` em `App.tsx`.

## D-005 — Reuso de taxonomia via prompt-engineering

**Data:** 2026-05-02 — CR-0003

`SuggestTags` envia ao LLM a lista atual de tags em uso (`Store.ListTags()`)
como parte do prompt e instrui: "Reuse from the existing tag list when
applicable; otherwise create new tags that match the same style." Output é
JSON estrito `{"tags":[…]}`, normalizado por `store.NormalizeTag` no daemon
antes de devolver, e dedup-ado case-insensitive no popup antes de mesclar com
o que o usuário já tem no campo.

**Por quê:** sem essa instrução o LLM gera variantes (`read-later` vs
`readlater` vs `read_later`), pulverizando a taxonomia e quebrando o filtro
por tag na busca. A solução é puramente prompt-engineering — sem modelo
fine-tuned, sem regras hard-coded de mapeamento. Trade-off: depende do LLM
respeitar a instrução; quando ele não respeita, a normalização no daemon
ainda força lowercase + kebab-friendly antes de devolver. Não há reranking
contra o vocabulário existente porque seria complexidade desproporcional para
o ganho.

## D-006 — Distribuição via distroless/static + `VBM_DATA_DIR`

**Data:** 2026-05-02 — CR-0005

A imagem Docker do daemon é multi-stage: build em `golang:1.22-bookworm`,
runtime em `gcr.io/distroless/static-debian12:nonroot`. O binário é estático
(`CGO_ENABLED=0` — `modernc.org/sqlite` é Go puro). Diretório de dados é
configurado via `VBM_DATA_DIR=/data` (com volume mount). `VBM_BIND=0.0.0.0`
é setado dentro do container; o host fica responsável por restringir o port
mapping a loopback (`-p 127.0.0.1:7532:7532`).

**Por quê:** distroless dá superfície de ataque mínima (~2MB base, sem shell,
sem package manager, UID não-root) e zero conflito de glibc/musl. O binário
estático Go já tem tudo que precisa em runtime. `VBM_DATA_DIR` resolve o
problema de distroless não ter `$HOME` setado por padrão e centraliza onde o
volume é montado, evitando hacks tipo "set HOME=/data" no Dockerfile. Sem
docker-compose nesta CR — o `docker run` é simples o bastante e quem quiser
abstrai depois.

## D-007 — Prompts do LLM externalizáveis via env+file com fallback embedded

**Data:** 2026-05-02 — CR-0006

`Summarize` e `SuggestTags` (em `internal/llm`) usam prompts carregados no
startup do `Client`:

- `VBM_LLM_PROMPT_SUMMARIZE_FILE` — caminho absoluto pra arquivo Markdown.
- `VBM_LLM_PROMPT_SUGGEST_TAGS_FILE` — idem.

Quando setado e legível (≤ 16 KB, não-vazio), o conteúdo do arquivo é o system
prompt. Caso contrário (ausente, vazio, grande demais, erro de leitura), cai
no prompt hardcoded com `slog.Warn` indicando o motivo. Lido **uma vez** no
construtor do `Client`; trocar prompt = restart o daemon.

**Por quê:** afinar prompt é um ajuste fino frequente (idioma, tom, comprimento
do resumo, estilo das tags). Recompilar Go pra cada tweak é fricção
desproporcional. Externalizar via env+file mantém a UX simples (arquivo de
texto, sem template engine, sem frontmatter) e o fallback embedded garante
que quem não configurar nada continua funcionando exatamente como antes.
Markdown como formato porque permite usuário estruturar prompt com headers e
listas legíveis; modelos modernos lidam bem com markdown na system message.

**Trade-off:** sem reload em runtime — decisão consciente. Reload exigiria
file-watch ou SIGHUP, complicação desproporcional pra POC. Restart do daemon é
~50ms.

---

## D-008 — Feedback de indexação é confirmado por poll, não pelo `202` (CR-0010)

**O quê:** o sucesso de uma indexação manual (badge verde, toast "Indexed ✓",
incremento do contador) só é reportado **depois** que o service worker confirma,
via poll em `GET /page?url=`, que a página realmente foi escrita (`indexed=true`)
— não quando o `POST /ingest` responde `202`. O `202` passou a significar apenas
"aceito na fila". Estados intermediários explícitos: badge `indexing` (roxo)
enquanto o SW confirma, `error` (laranja) quando o embed falha em definitivo.

No daemon, uma falha definitiva de embed (após 3 retries com backoff 1s/4s/10s)
marca a queue row como `status='failed'` com `last_error`, em vez de deixá-la
`pending` para sempre. A row `failed` é consultável (`GET /page` devolve
`queueStatus`/`lastError`) e retentável (`POST /queue/retry`). A tabela `queue`
passou a persistir `tags_json`/`set_tags` para que o requeue no restart não perca
as tags digitadas pelo usuário.

**Por quê:** o embed roda assíncrono e depende de um provider remoto (OpenRouter)
que falha de forma transitória (429/5xx). Tratar o `202` como conclusão produzia
falso-sucesso: badge verde com página não indexada, contador que não incrementa,
e erros de provider invisíveis sem caminho de retry. Confirmar contra o estado
real do daemon elimina a classe inteira de bugs.

**Por que poll e não o WebSocket existente:** o daemon já expõe um WS de status
(`/ws`), mas ele carrega apenas contadores agregados (`indexed`/`pending`), não o
desfecho por-URL. Poll simples em `/page?url=` (1s→3s, timeout 45s) mantém o SW
como único consumidor, sem estender o protocolo WS nem manter conexão viva por
ingest. Para um daemon local, o custo do poll é irrelevante.

**Trade-off:** o retry de embed segura o slot do worker durante o backoff (até
~15s). Aceitável: 4 workers, daemon local. O poll do SW depende do SW MV3 ficar
vivo durante o processamento — o próprio poll (setTimeout recorrente) segura o
SW acordado; se o Chrome ainda assim suspender, o estado real sobrevive na coluna
`queue.status` e é reidratado quando o popup reabre (via `queueStatus` do `/page`)
ou na próxima navegação (via `updateTabBadge`).
