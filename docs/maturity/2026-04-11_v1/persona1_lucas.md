# Avaliacao Inicial v1: Lucas Ferreira — Junior/Solo Dev

---

## Perfil

**Nome:** Lucas Ferreira
**Experiencia:** 3 anos, desenvolvedor solo, side projects
**OS:** Linux (Ubuntu)
**Deploy preferido:** Railway/Render
**Filosofia:** zero boilerplate, ferramentas que funcionam out-of-the-box
**Motivacao:** lembrar do que leu online — artigos, docs de libs, tutoriais

---

## Projeto 1 — LembraLeitor

**Cenario:** indexar artigos de blog, docs de libs e tutoriais lidos durante estudos. Buscar "aquele artigo sobre React hooks que li semana passada". ~20-50 paginas/dia.

### O que funciona bem

- Captura passiva e automatica: `extract.ts` usa Page Visibility API (`visibilitychange`) para acumular dwell real — nao tempo de aba aberta, tempo de leitura visivel (linha 14-23). Exatamente o que Lucas precisa.
- Readability extrai texto principal, descartando menus/rodapes/ads (linha 41 de `extract.ts`). Artigos de blog sao o caso de uso ideal para Readability.
- Busca pelo omnibox via `@recall` e palavra-chave funciona sem abrir nenhuma UI — fluxo nao interrompe o trabalho (service-worker.ts linha 95-113).
- Dedup por `url_hash` (sqlite.go linha 161): revisitas ao mesmo artigo nao duplicam entradas.
- 30s de dwell minimo filtra scrolls rapidos — apenas o que foi realmente lido entra no indice.

### O que nao funciona / esta ausente

- **Busca semantica e fake**: `StubEmbedder` retorna zero-vectors (embedder.go linha 22-24). Cosine similarity entre zero-vectors e zero-vectors e zero — dense search contribui zero ao RRF. Na pratica, o produto e apenas BM25/FTS5, nao semantico. A promessa central do produto nao esta entregue.
- **`search()` retorna `SearchResult[]` mas o cliente espera `{results: SearchResult[]}`**: daemon-client.ts linha 67 faz `res.json() as Promise<SearchResult[]>`, mas o handler em routes.go linha 129-144 retorna `{results: [...]}`. Esse mismatch quebra a busca no omnibox silenciosamente (nenhum resultado exibido, nenhum erro visivel).
- Sem paginacao na busca — limite fixo de 20 resultados (routes.go linha 113). Para Lucas com 50 paginas/dia, em semanas o indice cresce e os resultados sao truncados.
- FTS rebuild sincrono em cada `Ingest` (sqlite.go linha 198): `INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild')` dentro da transacao. Viola a propria regra do projeto ("Nunca fazer FTS5 rebuild sincrono em operacoes de ingest"). Com 50 paginas/dia esse overhead cresce.
- Popup abre `/ui` na porta hardcoded `7700` (App.tsx linha 181) — se o daemon subiu em porta aleatoria, o link esta errado.

### Tabela de scores — Projeto 1

| Dimensao | v1 | Comentario |
|---|---|---|
| Onboarding | 3 | Guia claro em pt-BR, Docker como alternativa, mas requer Go + Node + Chrome devtools — 4 passos para um dev solo |
| API Ergonomics | 2 | Mismatch `search()` TS vs JSON do daemon quebra silenciosamente; naming consistente |
| Feature Completeness | 2 | Captura funciona; busca semantica e stub (zero-vector); sem paginacao |
| Security Confidence | 4 | Token UUID por sessao, bind 127.0.0.1, denylist de dominios — solido para v0.1 |
| Language Quality | 3 | Go idiomatico; TS tem mismatch de tipos nao detectado; FTS rebuild no ingest viola regra propria |
| Operational Readiness | 3 | Graceful shutdown (server.go linha 68-77); queue com backpressure; sem health check de reconexao no SW |
| Documentation | 4 | GUIA.md completo, pt-BR, troubleshooting, Docker e nativo cobertos |

---

## Projeto 2 — DevNotebook

**Cenario:** capturar docs de libs (MDN, docs.rs, pkg.go.dev) enquanto desenvolve. Voltar a um conceito especifico. ~30 paginas/dia, textos tecnicos.

### O que funciona bem

- Textos tecnicos de documentacao sao bem estruturados — Readability extrai bem de MDN e pkg.go.dev (paginas com artigo principal bem delimitado).
- Chunking por janela de 512 tokens com overlap 64 (chunk.go linha 17-20) preserva contexto entre chunks — bom para docs com exemplos de codigo.
- FTS5 BM25 funciona bem para queries tecnicas exatas: "ownership borrow rust", "goroutine channel select" — vocabulario especializado favorece keyword search.
- Dedup por `text_hash` (sqlite.go linha 189): revisitas a mesma doc com mesmo conteudo nao reindexam — eficiente para paginas que nao mudam.

### O que nao funciona / esta ausente

- **docs.rs e pkg.go.dev usam SPA/JS-heavy rendering**: Readability pode falhar em extrair o texto principal se o conteudo e renderizado pos-carregamento. O content script roda no `document_idle` mas nao aguarda conteudo dinamico.
- **Sem filtragem por tipo de conteudo**: paginas de indice de uma lib (lista de funcoes sem texto) passam pelo threshold de 200 chars mas geram chunks ruins. Nao ha mecanismo de qualidade de chunk.
- Busca retorna snippets de 200 chars (sqlite.go linha 328) — muito curto para contexto tecnico. Um snippet de doc precisa de mais contexto para ser util.
- Sem destacar os termos buscados no snippet — Lucas nao consegue ver por que o resultado foi retornado.
- Dense search e stub — para "conceito que vi mas nao lembro o nome exato" (caso classico de busca semantica em docs), o produto falha.

### Tabela de scores — Projeto 2

| Dimensao | v1 | Comentario |
|---|---|---|
| Onboarding | 3 | Mesmo processo; docs.rs/pkg.go.dev funcionam na pratica sem configuracao extra |
| API Ergonomics | 2 | Mismatch TS/Go persiste; snippet de 200 chars insuficiente para docs tecnicas |
| Feature Completeness | 2 | BM25 util para queries exatas; semantica ausente; SPA rendering e risco |
| Security Confidence | 4 | Idem projeto 1 |
| Language Quality | 3 | Idem; sem validacao de qualidade de chunk |
| Operational Readiness | 3 | FTS rebuild sincrono penaliza ingest com 30 paginas/dia |
| Documentation | 3 | Guia nao menciona limitacoes de SPA ou como verificar se uma pagina foi capturada |

---

## Projeto 3 — CursoTracker

**Cenario:** acompanhar progresso em cursos online (YouTube, Udemy, freeCodeCamp). A maioria e video com pouco texto. Alguns tem transcricoes. Precisa saber "ja vi essa parte?". ~5-10 paginas/dia.

### O que funciona bem

- Para paginas com transcricao (freeCodeCamp, alguns cursos Udemy com legenda em texto), captura funciona normalmente.
- Dwell de 30s e atingido facilmente em paginas de curso — o usuario fica na pagina enquanto assiste.
- URL dedup registra a visita mesmo que o conteudo seja repetido — `visit_ts` e `dwell_ms` sao atualizados.

### O que nao funciona / esta ausente

- **YouTube nao tem texto extraivel por Readability**: a pagina e SPA, o titulo e a descricao sao o unico texto. `article.textContent.length < 200` (extract.ts linha 43) vai descartar a maioria das paginas do YouTube — o content script nao envia nada.
- **Sem tracking de progresso de video**: o produto indexa texto, nao progresso de reproducao. "Ja vi essa parte?" e impossivel de responder para video sem transcricao.
- **Udemy usa iframe e restricao de CSP**: content script pode nao ter acesso ao iframe do player.
- Para "ja vi essa parte?" o usuario precisa de uma lista de URLs visitadas, nao busca semantica — funcionalidade ausente (sem endpoint `/pages/list` ou similar).
- Sem exportacao ou visualizacao da lista de paginas indexadas (a `/ui` e placeholder: "Full UI coming in v0.2", routes.go linha 44).

### Tabela de scores — Projeto 3

| Dimensao | v1 | Comentario |
|---|---|---|
| Onboarding | 3 | Instalacao ok, mas Lucas vai descobrir que YouTube nao funciona so depois de instalar |
| API Ergonomics | 1 | Sem endpoint de listagem de paginas; sem tracking de progresso; casos de uso de video nao cobertos pela API |
| Feature Completeness | 1 | Captura falha para o cenario principal (video); sem historico navegavel |
| Security Confidence | 4 | Idem |
| Language Quality | 3 | Idem |
| Operational Readiness | 2 | Sem feedback ao usuario de que uma pagina nao foi capturada (falha silenciosa) |
| Documentation | 2 | Nao menciona limitacao com video/SPA; usuario descobre na pratica |

---

## Projeto 4 — SetupRapido

**Cenario:** onboarding de um amigo sem experiencia em Go ou Chrome extensions. Avaliacao do `install.sh` + `GUIA.md`.

### O que funciona bem

- `GUIA.md` esta em pt-BR, bem estruturado, com secoes de troubleshooting e comandos copy-paste prontos.
- `install.sh` e idempotente — pode ser reexecutado com `EXTENSION_ID=<id>` para corrigir o manifesto NM (linha 70-75).
- Docker como Opcao B e excelente para quem nao quer instalar Go — conceito bem explicado no guia.
- Script detecta Chromium e instala o manifesto NM em ambos (install.sh linha 44-48).
- Mensagens de erro claras no troubleshooting: "Popup mostra Daemon nao conectado" com 4 passos de diagnostico.

### O que nao funciona / esta ausente

- **Ordem de passos e confusa**: o guia pede para carregar a extensao (passo 3) antes de instalar o daemon (passo 4), mas o `install.sh` pede o Extension ID interativamente. Um amigo sem experiencia vai se perder se nao tiver o Chrome aberto na hora certa.
- **`make build` requer Go 1.22 na maquina**: sem pre-built binaries. Para "amigo sem Go instalado", a Opcao B (Docker) e necessaria, mas o Docker path exige comandos manuais de `docker cp` nao triviais (GUIA.md linha 138-145).
- **`install.sh` nao verifica pre-requisitos**: nao checa se `go`, `node`, `systemctl` existem antes de tentar. Falhas sao crípticas para um iniciante.
- Popup tem bug de porta hardcoded (7700) que o amigo vai encontrar ao clicar "Open full UI".
- Sem verificacao automatica de sucesso apos instalacao — o script termina sem confirmar que o daemon esta respondendo.
- `make install` nao e documentado como dependente de `make build` primeiro — ordem implicita.

### Tabela de scores — Projeto 4

| Dimensao | v1 | Comentario |
|---|---|---|
| Onboarding | 2 | Guia claro mas ordem confusa, sem pre-built binaries, pre-requisitos nao validados |
| API Ergonomics | 3 | install.sh tem boa ergonomia para quem ja conhece bash |
| Feature Completeness | 3 | Instalacao cobre nativo + Docker; desinstalacao (`make uninstall`) documentada |
| Security Confidence | 4 | Manifesto NM configurado corretamente; chmod 600 no session.json |
| Language Quality | 3 | Shell script simples e correto; sem tratamento de erro pre-requisitos |
| Operational Readiness | 2 | Sem verificacao pos-instalacao; falha silenciosa se Extension ID errado |
| Documentation | 3 | GUIA.md bom mas lacunas de UX para iniciante (ordem, pre-requisitos) |

---

## Projeto 5 — BuscaOmnibox

**Cenario:** usar `vbm <query>` no omnibox do Chrome para buscar paginas indexadas em tempo real enquanto navega. Avaliar UX da busca integrada.

### O que funciona bem

- Omnibox listener implementado (service-worker.ts linha 95-113): `onInputChanged` busca com debounce implicito (min 2 chars), `onInputEntered` navega para a URL selecionada.
- Sugestoes formatadas com `<url>dominio</url> — <match>snippet</match>` — usa XML escaping correto (funcao `escapeXml` linha 17-22), evita XSS no omnibox.
- Erro de conexao e silenciado (`catch` vazio, linha 110) — omnibox nao quebra se daemon estiver offline, apenas nao sugere nada.
- Limite de 5 resultados no omnibox e adequado para UX de autocomplete.

### O que nao funciona / esta ausente

- **Mismatch critico**: `search()` em daemon-client.ts linha 67 faz `res.json() as Promise<SearchResult[]>` mas o servidor retorna `{"results": [...]}`. O `.map()` na linha 104 falha silenciosamente — `results` e `undefined`, nenhuma sugestao aparece. O omnibox esta **quebrado em producao**.
- **Keyword no manifest errado**: o guia menciona `@recall` como keyword (GUIA.md linha 237, 244) mas o footer do popup diz "Type @recall in address bar" (App.tsx linha 225). O manifest.json precisa declarar o `omnibox.keyword` — nao verificado se e `@recall` ou `vbm`.
- Sem debounce real — cada tecla dispara um fetch ao daemon. Para queries longas digitadas rapidamente, multiplas requests em voo.
- Snippet truncado a 80 chars na sugestao (service-worker.ts linha 106) — muito pouco contexto para escolher o resultado certo.
- `onInputEntered` so navega se `text.startsWith('http')` (linha 117) — se o usuario pressionar Enter em uma query sem selecionar sugestao, nada acontece.

### Tabela de scores — Projeto 5

| Dimensao | v1 | Comentario |
|---|---|---|
| Onboarding | 3 | Funcionalidade documentada no guia; keyword pode nao estar no manifest |
| API Ergonomics | 1 | Mismatch TS/Go quebra completamente o omnibox; sem debounce; Enter sem selecao nao funciona |
| Feature Completeness | 2 | Implementacao existe mas e nao-funcional por bug de tipo; formato de sugestao e bom |
| Security Confidence | 4 | XML escaping correto; erro silenciado adequadamente |
| Language Quality | 2 | Type assertion errada (`as Promise<SearchResult[]>` vs `{results: SearchResult[]}`); sem debounce |
| Operational Readiness | 2 | Feature principal quebrada em runtime sem erro visivel |
| Documentation | 3 | Guia documenta o fluxo corretamente, mas nao avisa sobre limitacoes |

---

## Findings

### P0 — Blockers

**P0-1: Mismatch de tipo na resposta `/search`**
- `daemon-client.ts` linha 67: `res.json() as Promise<SearchResult[]>`
- `routes.go` linha 129-144: retorna `{"results": [...]}`
- Consequencia: `search()` retorna o objeto wrapper, nao o array. `results.map(...)` no omnibox (service-worker.ts linha 104) explode silenciosamente. **Busca no omnibox nao funciona.**
- Fix: mudar daemon-client.ts para `(res.json() as Promise<{results: SearchResult[]}>).then(d => d.results)` ou alterar o handler para retornar array direto.

**P0-2: StubEmbedder — busca semantica nao existe**
- `embedder.go` linha 22: retorna `make([]float32, 384)` — vetor zero.
- Cosine similarity entre dois vetores zero e 0 (divisao por zero, retorna 0 — linha 56-64).
- Dense search contribui zero ao RRF. O produto e BM25-only, nao hibrido.
- A proposta de valor central ("busca semantica", "busca por significado, nao palavra-chave") nao e entregue em v0.1.

**P0-3: FTS5 rebuild sincrono no Ingest**
- `sqlite.go` linha 198: `INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild')` dentro da transacao de ingest.
- O proprio CLAUDE.md lista isso como regra: "Nunca fazer FTS5 rebuild sincrono em operacoes de ingest — apenas em Forget".
- Com paginas de 30-50 chunks (artigos longos), rebuild completo a cada ingest e O(n) sobre todos os chunks existentes. Degrada progressivamente.

### P1 — Importantes

**P1-1: Porta hardcoded no popup**
- `App.tsx` linha 181: `const port = 7700; // fallback; daemon provides real port`
- O daemon usa porta aleatoria (server.go linha 46). O link "Open full UI" sempre aponta para 7700 — quebrado na instalacao nativa padrao.
- Fix: o popup deve obter a porta do daemon via `getStatus()` ou `connectDaemon()`.

**P1-2: YouTube e SPAs nao sao capturados**
- Readability falha em paginas SPA onde conteudo e renderizado pos-load.
- `article.textContent.length < 200` descarta silenciosamente paginas de video.
- Sem feedback ao usuario (nenhuma mensagem de "pagina nao capturada").
- Afeta: YouTube, Udemy, freeCodeCamp player, qualquer SPA React/Vue.

**P1-3: Sem listagem de paginas indexadas**
- Nao ha endpoint `GET /pages` nem UI funcional (routes.go linha 44: "Full UI coming in v0.2").
- Usuario nao consegue ver o que foi indexado — impossivel auditar o proprio historico.
- Caso de uso "ja vi essa parte?" (CursoTracker) e inteiramente dependente desse recurso.

**P1-4: Pause no popup e cosmético**
- `App.tsx` linha 153: botao Pause apenas altera estado local React `paused` — nao envia mensagem ao service-worker, nao para captura.
- O service-worker nunca checa esse estado. Pause nao funciona.

### P2 — Nice-to-have

**P2-1: Sem debounce no omnibox**
- Cada keystroke dispara fetch ao daemon. Adicionar debounce de ~200ms.

**P2-2: Snippet de 200 chars insuficiente**
- `sqlite.go` linha 328-329: snippet truncado a 200 chars. Para docs tecnicas, 400-500 chars seria mais util.

**P2-3: Sem pre-requisitos check no install.sh**
- Script falha cripticamente se Go, Node ou systemctl nao estao presentes.
- Adicionar verificacoes iniciais com mensagens de erro acionaveis.

**P2-4: `onInputEntered` sem fallback de busca**
- Se usuario digitar query e pressionar Enter sem selecionar sugestao, nada acontece (service-worker.ts linha 117: `if (text.startsWith('http'))`).
- Deveria abrir uma pagina de resultados ou a `/ui` com a query preenchida.

**P2-5: Sem reconexao automatica no SW**
- `connectDaemon()` em native-bridge.ts linha 17: `if (daemonState.port !== null) return` — assume conexao permanente.
- Se daemon reiniciar (novo token), SW continua usando token antigo ate ser reiniciado.

---

## Conclusao

Vector Bookmark tem uma arquitetura local-first correta e bem pensada para o problema. A camada de privacidade (denylist, incognito bloqueado, token de sessao, bind loopback) e o ponto mais solido do projeto. O guia em pt-BR e melhor que a media de projetos solo.

Para Lucas, o blocker real e duplo: a busca semantica nao existe (stub), e a busca por keyword via omnibox esta quebrada por mismatch de tipos entre TS e Go. Isso significa que em v0.1 o produto nao entrega sua proposta central para nenhum dos cenarios de uso.

Os projetos de leitura de texto (LembraLeitor, DevNotebook) sao os mais proximos de funcionar, com a correcao do P0-1. CursoTracker e inutilizavel pela limitacao fundamental de video/SPA. BuscaOmnibox exige correcao do P0-1 antes de qualquer teste real.

Para um dev solo como Lucas, o custo de setup (Go + Node + Chrome devtools + systemd) e razoavel uma vez. O que vai frustrar nao e o onboarding, e descobrir que a funcionalidade principal (busca semantica) e um placeholder.

**Recomendacao:** corrigir P0-1 (mismatch de tipos) e P0-3 (FTS rebuild) para ter um produto basico funcional. P0-2 (embedder real) e o que transforma o produto de "search engine pessoal" para "memoria semantica" — sem isso, e um BM25 local com UI de Chrome extension.

---

## Score Medio

| Dimensao | Proj1 | Proj2 | Proj3 | Proj4 | Proj5 | Media |
|---|---|---|---|---|---|---|
| Onboarding | 3 | 3 | 3 | 2 | 3 | **2.8** |
| API Ergonomics | 2 | 2 | 1 | 3 | 1 | **1.8** |
| Feature Completeness | 2 | 2 | 1 | 3 | 2 | **2.0** |
| Security Confidence | 4 | 4 | 4 | 4 | 4 | **4.0** |
| Language Quality | 3 | 3 | 3 | 3 | 2 | **2.8** |
| Operational Readiness | 3 | 3 | 2 | 2 | 2 | **2.4** |
| Documentation | 4 | 3 | 2 | 3 | 3 | **3.0** |

**Score v1: 2.7 / 5**
