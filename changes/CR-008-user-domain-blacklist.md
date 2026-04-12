# CR-008 — User-Managed Domain Blacklist

**Data:** 2026-04-12
**Solicitante:** user
**App Version:** 0.1.0
**Status:** implemented
**Urgência:** medium
**Domínios afetados:** Extension (service-worker, popup), denylist

---

## Descrição da mudança

Adicionar suporte a uma blacklist de domínios gerenciada pelo usuário. Domínios na blacklist nunca são indexados. O usuário pode adicionar e remover domínios via popup. A blacklist persiste em `chrome.storage.local` e é verificada no service worker junto ao denylist existente.

## Motivação

O denylist atual é hardcoded no código da extensão — o usuário não pode adicionar seus próprios domínios sem recompilar. Útil para bloquear intranets corporativas, ferramentas internas, sites de trabalho que o usuário não quer indexar.

## Comportamento atual

Existe apenas um denylist estático compilado em `extension/src/lib/denylist.ts` com 24 domínios fixos + padrões de URL. Não há forma de o usuário bloquear domínios adicionais sem editar o código.

## Comportamento desejado

- Usuário pode adicionar qualquer domínio à blacklist via popup
- Domínios bloqueados não são indexados (nem enviados ao daemon)
- Lista é persistida em `chrome.storage.local` sob a chave `vbmBlockedDomains`
- Popup exibe a lista atual com botão de remoção por entrada
- Service worker verifica a blacklist do usuário em conjunto com o denylist estático
- Domínio atual da aba ativa pode ser bloqueado com um clique ("Block this site")

---

## Análise de impacto

### Entidades afetadas
- `chrome.storage.local` — nova chave `vbmBlockedDomains: string[]`

### Backend
- Nenhum. A blacklist vive inteiramente na extensão.

### Frontend / Extensão
- `extension/src/background/service-worker.ts` — carregar `vbmBlockedDomains` no startup e ao receber `storage.onChanged`; verificar junto ao `isDeniedDomain()` em `handlePageViewed`
- `extension/src/popup/App.tsx` — nova seção "Blocked sites": input + "Block" btn para adicionar; lista dos domínios bloqueados com "×" por entrada; botão "Block this site" que pega o domínio da aba ativa
- `extension/src/background/native-bridge.ts` — (ou novo helper) funções `getBlockedDomains()`, `addBlockedDomain(domain)`, `removeBlockedDomain(domain)`

### Dados existentes
Não requer migração. `vbmBlockedDomains` começa vazio se não existir.

### Integrações externas
Nenhuma integração afetada.

### Conflitos com decisões existentes
Nenhum conflito. Complementa o denylist estático existente — não o substitui.

### CRs relacionadas
Nenhuma diretamente. O denylist estático foi introduzido no projeto base.

---

## Decisões necessárias antes de implementar

**Escopo da correspondência:** exact match de domínio (ex: `example.com` bloqueia `example.com` e `sub.example.com`) ou apenas exact? → Recomendação: suffix match (igual ao denylist estático), para bloquear todos os subdomínios.

Nenhuma outra decisão pendente.

---

## Critério de aceite

- [x] Adicionar `github.com` à blacklist → páginas do GitHub não são mais indexadas
- [x] Subdomínios também bloqueados: `gist.github.com` não indexado
- [x] Remover `github.com` da blacklist → indexação volta a funcionar
- [x] Blacklist persiste após fechar e reabrir o Chrome
- [x] "Block this site" no popup bloqueia o domínio da aba atual com um clique
- [x] Domínios do denylist estático continuam bloqueados independentemente da blacklist do usuário

---

## Atualizações de documentação necessárias

Após implementação, os seguintes documentos devem ser atualizados:
- [x] `CLAUDE.md` — seção "Regras de negócio críticas" §8 Privacidade: mencionar blacklist gerenciável pelo usuário
- [x] `docs/GUIA.md` — seção "O que NÃO é capturado": adicionar blacklist do usuário; seção "Popup": novo campo "Blocked sites"
- [x] `DECISIONS.md` — registrar decisão de suffix match para blacklist do usuário

---

*CR-008 — gerado em 2026-04-12 — status: draft*
