# CR-009 — Current Page Status & Enhanced Domain Blacklist

**Data:** 2026-04-12
**Solicitante:** user
**App Version:** 0.1.0
**Status:** implemented
**Urgência:** medium
**Domínios afetados:** Extension (popup, service-worker), Daemon (store, routes)

---

## Descrição da mudança

Três melhorias relacionadas à aba atual no popup da extensão:

1. **Status da página atual**: o popup exibe se a página aberta já está indexada no daemon.
2. **Remover página do índice**: se a página está indexada, um botão "Remove" a apaga com um clique — sem precisar copiar/colar a URL na seção "Forget".
3. **Regex na blacklist**: a blacklist de domínios (CR-008) passa a aceitar padrões regex no formato `/pattern/` além de domínios simples com suffix match.

## Motivação

- Sem feedback visual, o usuário não sabe se uma página foi capturada ou não.
- Remover uma página específica exige copiar a URL manualmente para o campo "Forget" — fluxo desnecessariamente trabalhoso.
- A blacklist de domínios (CR-008) cobre apenas suffix match; domínios internos com padrões irregulares (ex: `*.corp-*.internal`) precisam de regex.

## Comportamento atual

- O popup não indica se a página atual está indexada.
- Para remover uma página, o usuário precisa copiar a URL e colar no campo "Forget".
- A blacklist aceita apenas domínios simples com suffix match.

## Comportamento desejado

- Popup exibe "✓ This page is indexed" + botão "Remove" quando a página está no índice.
- Clicar "Remove" apaga a página e atualiza o indicador imediatamente.
- Blacklist aceita entradas do tipo `/regex/` (ex: `/\.corp\.internal$/`) além de domínios simples.
- Entradas regex são armazenadas e exibidas como strings `/pattern/` na lista de domínios bloqueados.

---

## Análise de impacto

### Entidades afetadas
- `pages` table (leitura) — novo método `PageExists(url)`

### Backend
- `daemon/internal/store/sqlite.go` — novo método `PageExists(rawURL string) (bool, error)` usando `chunk.Hash(rawURL)` para lookup por `url_hash`
- `daemon/internal/server/routes.go` — novo endpoint `GET /page?url=<url>` retornando `{"indexed": bool}`

### Frontend / Extensão
- `extension/src/background/daemon-client.ts` — nova função `pageExists(url): Promise<boolean>`
- `extension/src/background/service-worker.ts` — novo handler `popup_page_exists`; função `isBlockedByUser` atualizada para suportar regex
- `extension/src/popup/App.tsx` — novo estado `pageIndexed`, indicador visual e botão "Remove"
- `extension/src/background/native-bridge.ts` — `normaliseDomain` preserva entradas regex (não strip `*.`)

### Dados existentes
Não requer migração. Apenas leitura da tabela `pages` existente.

### Integrações externas
Nenhuma integração afetada.

### Conflitos com decisões existentes
Nenhum conflito. Estende a blacklist do CR-008 de forma retrocompatível — entradas existentes continuam funcionando.

### CRs relacionadas
- CR-008 (user-managed domain blacklist) — esta CR estende a regex support sobre a blacklist implementada lá.

---

## Decisões necessárias antes de implementar

Nenhuma decisão pendente — pode implementar diretamente.

---

## Critério de aceite

- [x] Abrir página já indexada → popup exibe "✓ This page is indexed" + botão "Remove"
- [x] Clicar "Remove" → flash "Removed", indicador some, página removida do índice
- [x] Abrir página não indexada → nenhum indicador exibido (comportamento inalterado)
- [x] Adicionar `/\.internal$/` à blacklist → domínios `.internal` não são indexados
- [x] Entradas regex exibidas como `/pattern/` na lista de sites bloqueados
- [x] Entradas de domínio simples existentes continuam funcionando com suffix match
- [x] "Block this site" ainda adiciona domínio simples (sem regex)

---

## Atualizações de documentação necessárias

Após implementação, os seguintes documentos devem ser atualizados:
- [x] `CLAUDE.md` — §8 Privacidade: mencionar suporte a regex na blacklist
- [x] `docs/GUIA.md` — seção "Popup": adicionar linha para status da página; seção "Blocked sites": mencionar suporte a `/regex/`
- [x] `DECISIONS.md` — registrar suporte a regex na blacklist (formato `/pattern/`)

---

*CR-009 — gerado em 2026-04-12 — status: draft*
