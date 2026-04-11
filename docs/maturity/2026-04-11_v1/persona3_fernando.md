# Avaliacao Inicial v1: Fernando Oliveira — Enterprise Architect

---

## Perfil

**Nome:** Fernando Oliveira
**Cargo:** Arquiteto de Sistemas — SaaS Enterprise
**Experiencia:** 10 anos em compliance LGPD/SOC2, auditoria, isolamento de dados
**Lema:** Zero surpresas em producao.
**Contexto de avaliacao:** O juridico/compliance precisa aprovar qualquer ferramenta que acessa dados de navegacao de colaboradores. A avaliacao e feita com ceticismo enterprise — o onus da prova e da ferramenta, nao do avaliador.

---

## Projeto 1 — ComplianceAudit

### O que funciona bem

- **Localidade total dos dados** (verificado): `server.go` L30 usa `~/.local/share/vbm/`, nenhum dado sai da maquina. `sqlite.go` L107-128 confirma banco local em `vbm.db`.
- **Incognito bloqueado** (verificado): `manifest.json` L6 — `"incognito": "not_allowed"`. Hard-coded no manifest, nao configuravel pelo usuario.
- **Token rotacionado a cada restart** (verificado): `server.go` L41 — `token := uuid.New().String()`. Nao persiste entre sessoes.
- **session.json chmod 600** (verificado): `host.go` L55 — `os.WriteFile(path, data, 0600)`. Diretorio criado com `0700` (L49).
- **Denylist funcional** (verificado): `denylist.ts` L44-78 — `.gov`, `.mil`, 24 dominios hardcoded, 14 padroes de URL. Checada no SW antes do ingest (`service-worker.ts` L40).
- **Forget implementado** (verificado): `sqlite.go` L355-381 — tres modalidades: `url`, `domain`, `timerange`. CASCADE delete por FK (`schema` L83: `ON DELETE CASCADE`).
- **Sem dados biometricos, financeiros proprios, ou PII estruturado** — apenas texto de paginas publicas visitadas.

### O que nao funciona / Gaps de compliance

1. **`VBM_PORT=... → 0.0.0.0` (P0):** `server.go` L47-49 — se a variavel de ambiente `VBM_PORT` for definida, o daemon faz bind em `0.0.0.0`, expondo o HTTP (com todos os dados de navegacao) em todas as interfaces de rede. O comentario diz "intended for Docker" mas nenhuma restricao impede uso em bare-metal. Um colaborador mal-informado ou um script de CI pode ativar isso acidentalmente.

2. **`/healthz` sem auth expoe existencia do daemon (P2):** `routes.go` L71-74 — qualquer processo local (malware, outro usuario no sistema) pode detectar que o daemon esta rodando fazendo port-scan e chamando `/healthz`. Baixo risco, mas relevante em ambiente corporativo com endpoint detection.

3. **Sem audit log de acessos (P1):** Nao existe registro de quem/quando consultou `/search` ou executou `/forget`. Para SOC2 e LGPD, o controlador deve poder demonstrar quem acessou dados pessoais de quem.

4. **Sem politica de retencao automatica (P1):** Dados acumulam indefinidamente. LGPD Art. 15 exige que dados sejam eliminados ao fim da finalidade. Nao ha TTL, nao ha limpeza automatica.

5. **Denylist nao cobre bancos brasileiros (P1):** `denylist.ts` lista apenas bancos americanos (BofA, Chase, WellsFargo). Itau, Bradesco, Nubank, Santander BR, BB, CEF — todos ausentes. Para uso enterprise no Brasil, isso e um gap direto de LGPD (dados financeiros).

6. **Token em memoria de SW sem timeout (P2):** `native-bridge.ts` L12-15 — `daemonState` persiste em memoria enquanto o SW viver. Se o SW nao morrer (tabs abertas), o token fica cacheado indefinidamente sem revalidacao.

7. **Sem mecanismo de exportacao de dados (P1):** LGPD Art. 18 garante portabilidade. Nao existe endpoint `/export` ou qualquer mecanismo para o titular obter seus dados em formato estruturado.

8. **`/ui` embutido serve `REPLACE_TOKEN` literal (P2):** `routes.go` L54 — o HTML da UI contem `'Authorization': 'Bearer REPLACE_TOKEN'`. Nao e um leak real (o token correto esta em memoria), mas e um artefato de desenvolvimento que nao deveria chegar em producao.

9. **Sem consentimento explícito do usuario no onboarding (P1):** A extensao comeca a capturar paginas imediatamente apos instalacao, sem tela de consentimento, sem EULA, sem explicacao do que e coletado. LGPD Art. 8 exige base legal clara; para ferramenta corporativa, ao menos um aceite formal e esperado.

### Tabela de Scores — Projeto 1

| Dimensao | v1 | Comentario |
|---|---|---|
| Onboarding | 2 | Install.sh funcional mas sem consentimento, sem doc de privacidade |
| API Ergonomics | 3 | Endpoints claros; falta `/export`; `/forget` com 3 tipos e bom |
| Feature Completeness | 2 | Falta exportacao, retencao automatica, audit log |
| Security Confidence | 2 | `VBM_PORT=0.0.0.0` e blocker; resto e razoavel para v0.1 |
| Language Quality | 4 | Go idiomatico, erros propagados, sem log.Fatal em packages |
| Operational Readiness | 2 | Sem monitoring, sem retention, sem audit log |
| Documentation | 2 | GUIA.md existe mas sem privacy notice, sem DPA template |

---

## Projeto 2 — PrivacyByDesign

### O que funciona bem

- **Privacy-by-default funcional:** incognito bloqueado (manifest L6), denylist checada antes de qualquer chamada ao daemon (service-worker L40), texto < 200 chars descartado antes do envio (extract.ts L43).
- **Deteccao de inputs sensiveis** (verificado): `extract.ts` L25-29 — `hasSensitiveInputs()` detecta `input[type="password"]`, `autocomplete="cc-*"`, `one-time-code`, etc. Se detectado, envia `page_sensitive` e **nao ingere** (L35-37).
- **Focusin em password** (verificado): `extract.ts` L63-70 — evento adicional de seguranca: se usuario foca em campo de senha apos o dwell, tambem cancela.
- **Readability para extracao** (verificado): `extract.ts` L40-41 — usa `@mozilla/readability` para extrair artigo principal, nao DOM completo. Reduz captura de menus/navbars/ads.
- **Dedup de conteudo** (verificado): `chunk.go` L39-43 — `sha1(normalize(text))` — chunks com mesmo hash sao `INSERT OR IGNORE` (sqlite L189).

### O que nao funciona / Gaps de privacidade

1. **Denylist nao e configuravel pelo usuario (P1):** A lista esta hardcoded em `denylist.ts`. Nao ha como o usuario adicionar dominios sensiveis corporativos (ex: ERP interno, sistema de RH, plataforma de saude). Para uso enterprise, a denylist precisa ser extensivel.

2. **`hasSensitiveInputs()` e verificado apenas no momento do `sendPage()` (P1):** `extract.ts` L31 — a verificacao e feita no momento do envio (apos 30s de dwell), nao continuamente. Um usuario pode visitar uma pagina que nao tem senha inicialmente, mas um componente React carrega um form de login dinamicamente apos os 30s. O `focusin` mitiga parcialmente (L63-70), mas ha uma janela.

3. **`location.href` enviado sem sanitizacao (P2):** `extract.ts` L47 — a URL completa e enviada, incluindo query strings que podem conter tokens, session IDs, parametros de busca sensíveis (ex: `?search=nome+cpf+empresa`). Denylist de URL so verifica pathname, nao query string.

4. **Texto extraido pelo Readability pode conter dados pessoais (P2):** Nao ha PII detection no pipeline. Um colaborador que visita uma pagina interna com listagem de funcionarios (nome, cargo, email) tera esses dados indexados. Para LGPD, isso cria obrigacoes de titular para terceiros.

5. **Sem granularidade de opt-out por dominio corporativo (P1):** O popup tem apenas "pause" global. Nao existe pause por dominio ou categoria. Usuario nao consegue dizer "nao indexe o gitlab corporativo" sem pausar tudo.

6. **Banco SQLite sem encryption at rest (P1):** `sqlite.go` L111 — `vbm.db` criado sem `PRAGMA key` (SQLCipher) ou qualquer encryption. Em ambiente corporativo com full-disk-encryption pode ser aceitavel, mas deve ser documentado explicitamente. BYOD sem FDE expoe dados de navegacao em plaintext.

### Tabela de Scores — Projeto 2

| Dimensao | v1 | Comentario |
|---|---|---|
| Onboarding | 2 | Sem explicacao de privacidade no primeiro uso |
| API Ergonomics | 3 | Denylist funcional mas nao extensivel via API |
| Feature Completeness | 2 | Sem encryption at rest, sem PII detection, sem opt-out granular |
| Security Confidence | 3 | Mecanismos basicos solidos; gaps em dinamismo e query strings |
| Language Quality | 4 | Codigo defensivo, verificacoes multiplas de sensibilidade |
| Operational Readiness | 2 | Sem logs de privacidade, sem relatorio de dados coletados |
| Documentation | 1 | Sem privacy notice, sem DPIA, sem documentacao de dados coletados |

---

## Projeto 3 — AuthSecurity

### O que funciona bem

- **Token UUID v4 por sessao** (verificado): `server.go` L41 — `uuid.New().String()`. Entropia adequada (~122 bits). Rotado a cada restart.
- **session.json chmod 600** (verificado): `host.go` L55. Diretorio pai `0700` (L49). Apenas o usuario dono le.
- **Native Messaging como canal seguro para token** (verificado): O token nunca trafega pela extensao diretamente — ele e obtido via `chrome.runtime.sendNativeMessage` (`native-bridge.ts` L21), que e um IPC autenticado pelo Chrome (verifica Extension ID no manifest NM).
- **Origin check no authMiddleware** (verificado): `routes.go` L234 — rejeita requests com Origin que nao comecam com `chrome-extension://`. Impede que paginas web normais chamem o daemon mesmo conhecendo o token.
- **CORS restrito** (verificado): `routes.go` L255 — `Access-Control-Allow-Origin` so e setado para origins `chrome-extension://`.
- **Token nunca em chrome.storage** (verificado): `native-bridge.ts` L12-15 — `daemonState` e variavel de modulo (memoria), nao persistida.

### O que nao funciona / Vetores de ataque

1. **`VBM_PORT=... → 0.0.0.0` bypassa isolamento de rede (P0):** `server.go` L47-49 — com `VBM_PORT` setado, o daemon escuta em todas as interfaces. Nesse caso, o Origin check (`routes.go` L234) se torna a unica linha de defesa — e Origin e um header HTTP trivialmente forjavel por qualquer cliente que nao seja um browser (curl, scripts, outros processos). O authMiddleware ainda valida o Bearer token, mas um atacante na rede local so precisa capturar o token uma vez.

2. **OPTIONS (CORS preflight) sem autenticacao (P1):** `routes.go` L226-229 — `authMiddleware` deixa OPTIONS passar sem verificar token. Isso e necessario para CORS funcionar, mas significa que qualquer origem pode fazer preflight e descobrir quais metodos e headers sao aceitos, confirmando que o daemon esta rodando e sua versao de API.

3. **Sem rate limiting no authMiddleware (P1):** Nao ha protecao contra brute-force do token. UUID v4 tem espaco enorme, mas a ausencia de rate limiting e um gap de hardening. Em ambiente com muitos processos locais, um atacante local poderia tentar tokens em alta velocidade.

4. **Token cacheado em SW sem expiracao (P2):** `native-bridge.ts` L18 — `if (daemonState.port !== null && daemonState.token !== null) return` — o token e cacheado para sempre enquanto o SW vive. Se o daemon reiniciar (novo token), o SW continua usando o token antigo ate o proximo restart do SW, resultando em falhas de auth silenciosas — mas nao em vulnerabilidade direta.

5. **NM manifest com Extension ID hardcoded (P1):** `install.sh` L38-41 — o NM manifest aceita apenas o Extension ID informado no install. Isso e correto para producao, mas o installer aceita Extension ID em branco e substitui por `REPLACE_ME` (L39), deixando o NM acessivel para qualquer extensao se o usuario nao preencher o campo.

6. **Sem mutual auth entre extensao e daemon (P2):** O daemon confia em qualquer cliente que apresente o token correto + Origin `chrome-extension://`. Nao ha verificacao de que o caller e especificamente a extensao VBM (ex: certificate pinning, extensao ID verificada no daemon). Outra extensao Chrome instalada no mesmo browser, se obtiver o token, teria acesso total.

### Tabela de Scores — Projeto 3

| Dimensao | v1 | Comentario |
|---|---|---|
| Onboarding | 3 | NM setup funciona; Extension ID em branco e risco |
| API Ergonomics | 4 | Auth flow limpo; Bearer token simples e correto |
| Feature Completeness | 2 | Sem rate limit, sem token expiration, sem mutual auth |
| Security Confidence | 2 | `VBM_PORT=0.0.0.0` e blocker; OPTIONS sem auth e gap |
| Language Quality | 4 | Middleware bem estruturado, sem secrets em variaveis globais |
| Operational Readiness | 2 | Sem alertas de auth failure, sem lockout |
| Documentation | 2 | Sem threat model documentado, sem security advisory |

---

## Projeto 4 — DataRetention

### O que funciona bem

- **Forget com 3 modalidades** (verificado): `sqlite.go` L355-381 — `url` (L359), `domain` (L361), `timerange` (L364-373). Cobre os principais casos de uso do direito ao esquecimento LGPD Art. 18.
- **CASCADE delete** (verificado): `schema` L83 — `REFERENCES pages(id) ON DELETE CASCADE` — deleta chunks automaticamente ao deletar pagina. FTS5 rebuild apos forget (L379).
- **Endpoint DELETE /forget autenticado** (verificado): `routes.go` L147-159 — dentro do grupo autenticado, corpo JSON com `type` e `value`.
- **Dedup impede acumulo de duplicatas** (verificado): `INSERT OR IGNORE` em chunks (sqlite L189) e `ON CONFLICT DO UPDATE` em pages (L159-167).

### O que nao funciona / Gaps de retencao

1. **Sem politica de retencao automatica (P0 para LGPD):** Nao existe nenhum mecanismo de TTL, cron job, ou limpeza automatica. Dados acumulam indefinidamente. LGPD Art. 15 exige que dados sejam eliminados quando a finalidade for cumprida ou a pedido do titular. Sem retencao maxima configuravel, a ferramenta nao tem como demonstrar compliance de ciclo de vida.

2. **Sem endpoint de exportacao (P1 — LGPD Art. 18 portabilidade):** Nao existe `/export` ou equivalente. O titular nao tem como obter seus dados em formato portatil (JSON, CSV). A unica forma de exportar e acessar diretamente `vbm.db` com SQLite CLI — inaceitavel para usuario nao tecnico e impossivel de auditar.

3. **`queue` table nao e limpa apos processamento (P2):** `sqlite.go` schema L92-103 — a tabela `queue` tem `status` (`pending`, provavelmente `done`), mas o worker (`queue.go` L31-37) nunca atualiza o status nem deleta registros processados. Com o tempo, a tabela acumula todo o historico de URLs ingeridas em plaintext — uma segunda copia dos dados sem nenhum controle.

4. **Forget nao limpa a `queue` table (P1):** `sqlite.go` L355-381 — `Forget` deleta apenas de `pages` (com CASCADE para `chunks`). A tabela `queue` nao e tocada. Um `/forget url=X` nao remove X da queue — os dados persistem la.

5. **Sem confirmacao de forget (P2):** `routes.go` L147-159 — o DELETE retorna `{"forgotten":true}` sem retornar quantos registros foram afetados. Para auditoria, o responsavel nao sabe se o forget teve efeito real.

6. **timerange forget usa `visit_ts` mas queue usa `created_at` (P2):** Inconsistencia de campo temporal entre tabelas — e o forget de timerange nao age sobre a queue de forma alguma.

### Tabela de Scores — Projeto 4

| Dimensao | v1 | Comentario |
|---|---|---|
| Onboarding | 2 | Sem documentacao de ciclo de vida de dados |
| API Ergonomics | 3 | Forget funcional mas sem feedback quantitativo; falta export |
| Feature Completeness | 1 | Sem TTL, sem export, queue nunca limpa, forget incompleto |
| Security Confidence | 2 | CASCADE correto; gaps na queue sao risco de compliance |
| Language Quality | 3 | Logica de forget correta para `pages`; lacunas na `queue` |
| Operational Readiness | 1 | Sem retention policy, sem scheduled cleanup, sem exportacao |
| Documentation | 1 | Sem documentacao de ciclo de vida, sem procedimento de DSAR |

---

## Projeto 5 — NetworkIsolation

### O que funciona bem

- **Bind padrao em 127.0.0.1:0 (porta aleatoria)** (verificado): `server.go` L46 — `listenAddr := "127.0.0.1:0"`. Porta aleatoria reduz superfície de ataque (nenhum servico pode fazer port-forward previsivel).
- **StubEmbedder completamente local** (verificado): `embedder.go` L20-23 — retorna vetor zero, sem chamada de rede. Comentario L22 explicita a intencao futura (ONNX local).
- **Sem dependencias de CDN ou endpoints externos no codigo** (verificado): nenhum `fetch` externo em nenhum arquivo Go ou TS analisado.
- **Extensao so faz fetch para `127.0.0.1`** (verificado): `native-bridge.ts` L53 — `http://127.0.0.1:${daemonState.port}`.
- **`modernc.org/sqlite` pure-Go** (verificado): sem CGO, sem binarios externos, sem FFI que poderia fazer chamadas de rede inesperadas.

### O que nao funciona / Gaps de isolamento

1. **`VBM_PORT` expoe em `0.0.0.0` (P0):** `server.go` L47-49 — critica. Com `VBM_PORT` definido, o daemon escuta em todas as interfaces. Em ambiente Docker, o isolamento depende de `-p 127.0.0.1:PORT:PORT` no `docker run` — configuracao externa ao codigo, fora do controle da ferramenta. Em bare-metal, qualquer processo ou usuario na rede local acessa o daemon com dados de navegacao completos. Nao ha warning no codigo, no installer, ou na documentacao sobre esse risco.

2. **`vbmd.service` tem `PrivateNetwork=false` (P1):** `vbmd.service` L10 — o systemd unit nao usa `PrivateNetwork=true` ou `IPAddressDeny=any` + `IPAddressAllow=127.0.0.1`. Mesmo que o codigo faca bind correto, o systemd poderia reforcar o isolamento de rede em nivel de OS — e nao o faz.

3. **Embedder futuro pode fazer requests externos (P1):** `embedder.go` L22 — `TODO: replace with onnxruntime-go + Snowflake/snowflake-arctic-embed-xs`. O ONNX local nao faz requests externos, mas o TODO nao especifica se o modelo sera baixado automaticamente (o que requereria requests externos na primeira execucao). Sem politica documentada, o juridico nao pode aprovar o comportamento futuro.

4. **`host_permissions: <all_urls>` na extensao (P1):** `manifest.json` L16 — permissao de content script em todas as URLs e necessaria para o funcionamento, mas e a permissao mais ampla possível. Em ambiente corporativo, o ideal seria uma allowlist de dominios relevantes. Chrome Web Store policies e departamentos de TI corporativos frequentemente bloqueiam extensoes com `<all_urls>`.

5. **WebSocket `/ws` sem timeout de conexao (P2):** `routes.go` L180-208 — o handler de WebSocket nao tem timeout, ping/pong, ou limite de conexoes simultaneas. Uma conexao aberta indefinidamente por um cliente malicioso poderia manter recursos alocados.

6. **`middleware.Logger` loga URLs de requests (P2):** `routes.go` L67 — chi Logger padrao loga metodo, path, status e latencia para stdout/journald. Requests para `/search?q=nome+cpf` ficam em logs do sistema — uma segunda fonte de dados pessoais fora do controle do forget.

### Tabela de Scores — Projeto 5

| Dimensao | v1 | Comentario |
|---|---|---|
| Onboarding | 3 | Instalacao local clara; Docker path documentado mas arriscado |
| API Ergonomics | 3 | API local correta; VBM_PORT deveria ter warning explicito |
| Feature Completeness | 2 | Sem systemd network hardening, sem allowlist de dominios |
| Security Confidence | 2 | `VBM_PORT=0.0.0.0` e blocker; systemd sem PrivateNetwork |
| Language Quality | 4 | Go network code correto; bind explícito e bom padrao |
| Operational Readiness | 2 | Sem network monitoring, logs de query podem vazar PII |
| Documentation | 2 | Docker path sem warning de seguranca; TODO de embedder sem politica |

---

## Findings Consolidados

### P0 — Blockers (impedem aprovacao imediata)

| ID | Finding | Arquivo | Linha |
|---|---|---|---|
| P0-01 | `VBM_PORT` env var faz bind em `0.0.0.0`, expondo dados de navegacao em todas as interfaces sem warning | `server.go` | 47-49 |
| P0-02 | Sem politica de retencao automatica de dados — LGPD Art. 15 requer eliminacao ao fim da finalidade | `sqlite.go` | — (ausencia) |

### P1 — Importantes (devem ser resolvidos antes de deploy corporativo)

| ID | Finding | Arquivo |
|---|---|---|
| P1-01 | Sem endpoint de exportacao de dados — LGPD Art. 18 portabilidade | `routes.go` (ausencia) |
| P1-02 | `Forget` nao limpa tabela `queue` — dados persistem apos exclusao | `sqlite.go` L355-381 |
| P1-03 | Denylist sem bancos brasileiros — Itau, Bradesco, Nubank, BB, CEF ausentes | `denylist.ts` L1-26 |
| P1-04 | Banco SQLite sem encryption at rest — BYOD sem FDE expoe dados em plaintext | `sqlite.go` L111 |
| P1-05 | Sem consentimento explícito no onboarding — LGPD Art. 8 | `manifest.json` / `extract.ts` |
| P1-06 | Sem audit log de acessos a dados pessoais — necessario para SOC2/LGPD | `routes.go` (ausencia) |
| P1-07 | `vbmd.service` sem `PrivateNetwork=true` — hardening de OS nao aplicado | `vbmd.service` L10 |
| P1-08 | Denylist nao extensivel — sem API para adicionar dominios corporativos | `denylist.ts` (hardcoded) |
| P1-09 | NM manifest aceita `REPLACE_ME` como Extension ID — abre NM para qualquer extensao | `install.sh` L39 |
| P1-10 | `hasSensitiveInputs()` nao e continuo — janela de captura antes de form dinamico | `extract.ts` L31 |
| P1-11 | `middleware.Logger` loga queries de busca no journald — PII em logs de sistema | `routes.go` L67 |
| P1-12 | Sem rate limiting no endpoint de autenticacao | `routes.go` (ausencia) |

### P2 — Nice-to-have / Melhorias

| ID | Finding |
|---|---|
| P2-01 | `REPLACE_TOKEN` literal no HTML da UI embutida |
| P2-02 | Token do SW sem expiracao/revalidacao |
| P2-03 | URL completa (incluindo query string) indexada sem sanitizacao |
| P2-04 | `/healthz` sem auth expoe existencia do daemon |
| P2-05 | `queue` table nunca limpa apos processamento |
| P2-06 | Forget sem retorno quantitativo (quantos registros afetados) |
| P2-07 | WebSocket sem timeout e sem limite de conexoes |
| P2-08 | TODO de embedder sem politica de download de modelo |

---

## Score Medio por Dimensao e Projeto

| Dimensao | P1 ComplianceAudit | P2 PrivacyByDesign | P3 AuthSecurity | P4 DataRetention | P5 NetworkIsolation | Media |
|---|---|---|---|---|---|---|
| Onboarding | 2 | 2 | 3 | 2 | 3 | **2.4** |
| API Ergonomics | 3 | 3 | 4 | 3 | 3 | **3.2** |
| Feature Completeness | 2 | 2 | 2 | 1 | 2 | **1.8** |
| Security Confidence | 2 | 3 | 2 | 2 | 2 | **2.2** |
| Language Quality | 4 | 4 | 4 | 3 | 4 | **3.8** |
| Operational Readiness | 2 | 2 | 2 | 1 | 2 | **1.8** |
| Documentation | 2 | 1 | 2 | 1 | 2 | **1.6** |

---

## Conclusao

### Avaliacao Geral

O Vector Bookmark tem uma **base arquitetural correta para privacidade**: localidade total dos dados, token rotacionado, chmod 600, incognito bloqueado, denylist funcional, forget com CASCADE. O codigo Go e TypeScript e de qualidade acima da media para um POC — erros propagados, sem log.Fatal em packages, tipagem adequada.

**Porem, dois blockers P0 impedem aprovacao pelo juridico:**

1. **`VBM_PORT=0.0.0.0`** e uma backdoor documentada no codigo que expoe todos os dados de navegacao dos colaboradores na rede local. Nao importa que seja "para Docker" — o codigo aceita a variavel sem restricao de ambiente, sem warning, e o installer nao documenta o risco. Um unico script de deploy mal configurado vaza dados de todos os usuarios.

2. **Ausencia de retencao automatica** significa que a empresa nao consegue demonstrar compliance com LGPD Art. 15 para um auditor. Dados de navegacao acumulam indefinidamente sem mecanismo de eliminacao automatica.

Alem dos P0, os gaps P1 de ausencia de exportacao (portabilidade LGPD), banco sem encryption at rest, e ausencia de consentimento formal sao exigencias minimas para qualquer ferramenta corporativa que acessa dados pessoais de colaboradores.

### Recomendacao

**REPROVADO** para deploy corporativo em v0.1.

**Condicoes para reaprovacao:**
1. Remover ou restringir `VBM_PORT` para aceitar apenas `127.0.0.1:PORT` (nunca `0.0.0.0`)
2. Implementar TTL configuravel com limpeza automatica (ex: 90 dias default)
3. Implementar `/export` para portabilidade LGPD
4. Corrigir `Forget` para incluir a tabela `queue`
5. Adicionar bancos brasileiros na denylist
6. Adicionar tela de consentimento no onboarding da extensao
7. Adicionar `PrivateNetwork=true` no systemd unit
8. Documentar privacy notice e procedimento de DSAR

A ferramenta e promissora como POC tecnico e pode ser aprovada **com ressalvas** apos resolver os P0 e os principais P1. A arquitetura local-first e o design de privacidade basico mostram que o time tem consciencia de privacidade — falta apenas fechar os gaps de maturidade enterprise.

---

**Score v1: 2.4 / 5**
