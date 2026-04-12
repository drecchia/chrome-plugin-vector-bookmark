# CR-010 — Star-rank indexing, blocked domain hint, blocklist em Open UI

**Data:** 2026-04-12
**Solicitante:** user
**App Version:** 0.1.0
**Status:** implemented
**Urgência:** medium
**Domínios afetados:** Extension (popup, service-worker, extract), Daemon (store, routes, ui)

---

## Descrição da mudança

Três melhorias ao popup e à UI local:

1. **"Index as reference"**: segundo botão de indexação que marca a página com `star_rank=1`, fazendo com que ela apareça mais cedo nos resultados de busca.
2. **Botão desabilitado em domínio bloqueado**: o botão "Index this page now" fica desabilitado (com tooltip) quando o domínio da aba atual está na blacklist do usuário ou no denylist estático.
3. **Blacklist na UI local**: a seção de gerenciamento da blacklist (lista + input) sai do popup e vai para uma nova aba "Blocklist" em `http://127.0.0.1:PORTA/ui`. A armazenagem migra de `chrome.storage.local` para o daemon (SQLite). O popup mantém apenas o botão de atalho "Block this site".

## Motivação

1. O usuário quer poder classificar certas páginas como referência de qualidade — páginas que devem rankar antes nos resultados mesmo que a busca semântica as posicione abaixo de páginas menos relevantes.
2. Tentar indexar uma página bloqueada é uma ação sem efeito — o botão deveria comunicar isso em vez de silenciosamente fazer nada.
3. A lista de domínios bloqueados pode crescer muito e comprometer a usabilidade do popup (340px de largura). A UI local (`/ui`) é o lugar adequado para gerenciar listas.

## Comportamento atual

- Existe apenas um botão de indexação sem distinção de qualidade.
- O botão "Index this page now" não indica quando está numa página bloqueada.
- A blacklist é gerenciada inteiramente no popup via `chrome.storage.local`.

## Comportamento desejado

- Popup tem dois botões: "Index this page now" (comportamento atual) e "⭐ Index as reference" (same flow + `star_rank=1`).
- Páginas com `star_rank=1` recebem multiplicador 1.5× no score RRF antes do sort final.
- Quando hostname da aba está na blacklist/denylist, ambos os botões ficam desabilitados com título "This domain is blocked".
- A seção "Blocked sites" some do popup; apenas o botão "Block this site" permanece.
- A aba "Blocklist" na UI local exibe a lista completa e permite adicionar/remover entradas.
- Armazenagem da blacklist: nova tabela SQLite `blocklist` no daemon; SW carrega do daemon no startup.

---

## Análise de impacto

### Entidades afetadas
- `pages` table — nova coluna `star_rank INTEGER NOT NULL DEFAULT 0`
- Nova tabela `blocklist (pattern TEXT PRIMARY KEY, created_at INTEGER NOT NULL)`

### Backend
- `daemon/internal/store/sqlite.go`:
  - Migration v3: `ALTER TABLE pages ADD COLUMN star_rank INTEGER NOT NULL DEFAULT 0`
  - Migration v4: `CREATE TABLE IF NOT EXISTS blocklist (...)`
  - `Ingest()`: aceita e persiste `star_rank` do request
  - `Search()`: após `rrf()`, multiplica score de entradas com `star_rank=1` por 1.5
  - Novos métodos: `GetBlocklist()`, `AddToBlocklist(pattern)`, `RemoveFromBlocklist(pattern)`
- `daemon/internal/server/routes.go`:
  - `GET /blocklist` → `{"patterns": [...]}`
  - `POST /blocklist {"pattern": "..."}` → `{"ok": true}`
  - `DELETE /blocklist {"pattern": "..."}` → `{"ok": true}`
  - UI: nova aba "Blocklist" na `uiHTML`

### Frontend / Extensão
- `proto/types.ts`: adicionar `starRank?: boolean` a `IngestRequest`
- `extension/src/content/extract.ts`: propagar `starRank` do `force_extract` para o `page_viewed` message
- `extension/src/background/service-worker.ts`:
  - `handlePageViewed()`: lê `msg.starRank`, passa para `ingest()`
  - `popup_force_index_star`: igual a `popup_force_index` mas envia `{ type: 'force_extract', starRank: true }`
  - Carregar blocklist do daemon no startup em vez de `chrome.storage.local`; manter `chrome.storage.onChanged` apenas para `vbmHost`/`vbmPort`
  - Novo handler `popup_is_blocked` para o popup verificar se hostname está bloqueado
- `extension/src/background/daemon-client.ts`:
  - `ingest()`: inclui `starRank` no body
  - Funções: `getBlocklist()`, `addToBlocklist(pattern)`, `removeFromBlocklist(pattern)`
- `extension/src/background/native-bridge.ts`: remover `getBlockedDomains`, `addBlockedDomain`, `removeBlockedDomain` (migram para daemon-client)
- `extension/src/popup/App.tsx`:
  - Remover estado `blockedDomains`, `blockInput`; remover seção "Blocked sites" (list + input)
  - Manter botão "Block this site" → chama `addToBlocklist` via daemon-client
  - Adicionar botão "⭐ Index as reference" → `popup_force_index_star`
  - Adicionar estado `isCurrentDomainBlocked` (via `popup_is_blocked` message)
  - Desabilitar ambos os botões de indexação quando `isCurrentDomainBlocked === true`

### Dados existentes
- `star_rank`: `ALTER TABLE` com `DEFAULT 0` — retrocompatível, sem migração de dados.
- Blacklist em `chrome.storage.local` (`vbmBlockedDomains`): não migrada automaticamente — o usuário precisará re-adicionar os domínios pela UI local. Documentar no GUIA.md.

### Integrações externas
Nenhuma integração afetada.

### Conflitos com decisões existentes
- CR-008 introduziu `vbmBlockedDomains` em `chrome.storage.local` — esta CR remove essa chave e move a fonte de verdade para o daemon. Retrocompatibilidade: a chave antiga é simplesmente ignorada.
- CR-009 adicionou suporte a regex na blacklist — o comportamento de matching é mantido; apenas a fonte de leitura muda (daemon em vez de chrome.storage).

### CRs relacionadas
- CR-008 (blacklist em chrome.storage) — esta CR migra a armazenagem para o daemon
- CR-009 (regex na blacklist) — comportamento mantido, fonte mudada

---

## Decisões necessárias antes de implementar

**Migração automática da blacklist**: ao iniciar o SW após o update, ler `vbmBlockedDomains` de `chrome.storage.local` e migrar automaticamente para o daemon (POST /blocklist para cada entrada), depois limpar a chave. Ou documentar que o usuário precisa re-adicionar manualmente. → Recomendação: migração automática no SW (one-shot na primeira vez que o daemon responder ao startup).

Nenhuma outra decisão pendente.

---

## Critério de aceite

- [x] "⭐ Index as reference" indexa a página e persiste `star_rank=1` no DB
- [x] Busca por termo presente nessa página retorna ela antes de páginas não estreladas com score similar
- [x] "Index this page now" e "⭐ Index as reference" ficam desabilitados quando hostname está na blacklist/denylist
- [x] Tooltip/title="This domain is blocked" visível ao passar o mouse sobre botões desabilitados
- [x] Seção "Blocked sites" (lista + input) não aparece mais no popup
- [x] Botão "Block this site" no popup permanece e adiciona via daemon
- [x] Aba "Blocklist" na UI local exibe lista + permite adicionar/remover entradas
- [x] Páginas adicionadas à blacklist no `/ui` são bloqueadas pelo SW imediatamente (próximo fetch do daemon)
- [x] Entradas regex (`/pattern/`) continuam funcionando na nova armazenagem

---

## Atualizações de documentação necessárias

Após implementação, os seguintes documentos devem ser atualizados:
- [x] `CLAUDE.md` — §8 Privacidade: nota sobre migração de armazenagem da blacklist; §Entidades: nova tabela `blocklist`; mencionar `star_rank` em `pages`
- [x] `docs/GUIA.md` — seção Popup: atualizar tabela; nota sobre migração manual da blacklist; seção UI local: nova aba Blocklist
- [x] `DECISIONS.md` — registrar migração da blacklist para daemon; registrar star_rank e boost 1.5×

---

*CR-010 — gerado em 2026-04-12 — status: draft*
