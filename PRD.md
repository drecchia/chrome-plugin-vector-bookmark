# PRD.md
## Product Requirements — gerado por bootstrap em 2026-04-11
## Estado atual do sistema — baseado no código implementado

---

## O que é este sistema

Vector Bookmark é uma ferramenta de memória pessoal de navegação. Ela captura automaticamente o conteúdo de páginas que o usuário realmente leu (medido por tempo de permanência visível), indexa esse conteúdo localmente e permite recuperá-lo por busca semântica em linguagem natural, diretamente da barra de endereços do Chrome.

O sistema não requer que o usuário salve nada manualmente. A captura é passiva e silenciosa. Toda a informação fica armazenada na máquina do usuário — sem contas, sem servidores externos, sem telemetria.

---

## Usuários e perfis

**Perfil único implementado: usuário individual, local**

- Não há autenticação de usuário — sistema single-tenant implícito
- Não há roles ou permissões por perfil
- A proteção de acesso é por localidade: daemon só aceita conexões de `127.0.0.1` com token de sessão
- Único "perfil" reconhecido é o próprio usuário do sistema operacional (dono do `~/.local/share/vbm/`)

---

## Funcionalidades implementadas

### Captura passiva de páginas

- O sistema monitora todas as abas abertas no Chrome
- Uma página é considerada "lida" após **30 segundos de tempo visível acumulado** (a aba estar em foco, sem o sistema estar escondido)
- O texto principal da página é extraído automaticamente, removendo menus, rodapés, anúncios e elementos de navegação
- Páginas com menos de 200 caracteres de conteúdo útil são ignoradas
- A mesma página visitada múltiplas vezes não gera duplicatas (deduplicação por URL e por conteúdo)

### Bloqueio automático de conteúdo sensível

O sistema **nunca captura** automaticamente:
- Janelas anônimas (incógnito)
- Páginas de login, autenticação OAuth/SSO/MFA, redefinição de senha
- Páginas de pagamento e checkout
- Qualquer página onde o usuário foca em um campo de senha ou cartão de crédito
- Domínios de bancos, corretoras, portais de saúde, órgãos de governo (`.gov`, `.mil`), gerenciadores de senha e webmail

### Indexação e busca semântica

- O conteúdo capturado é dividido em blocos de texto (chunks) e indexado localmente em um banco SQLite
- A busca é híbrida: combina busca por palavras-chave (BM25) com busca por similaridade semântica (vetores)
- A busca semântica atual usa um modelo stub (vetores zero) — funcional para BM25, pronto para receber modelo ONNX real
- Resultados retornam: título, trecho relevante, domínio, data da visita e score de relevância

### Busca pela barra de endereços

- O usuário digita `@recall` seguido de espaço na barra de endereços do Chrome
- Resultados aparecem em tempo real como sugestões (mínimo 2 caracteres)
- Clicar em um resultado abre a URL original da página
- Limite de 5 sugestões na omnibox; até 20 resultados na UI local

### Controle e esquecimento

- O usuário pode apagar registros por URL específica, por domínio inteiro, ou por intervalo de tempo
- A exclusão é permanente — remove tanto os metadados quanto os chunks indexados
- O popup da extensão oferece acesso rápido às ações de esquecer

### Popup de controle

- Exibe quantas páginas estão indexadas e quantas estão na fila de processamento
- Permite pausar a captura
- Oferece ação de "esquecer" por URL ou domínio
- Link para abrir a UI local completa no navegador

### Interface web local

- O daemon serve uma interface HTML simples em `http://127.0.0.1:PORTA/ui`
- Permite busca por texto com exibição de resultados (título + trecho + link)
- Estado atual: placeholder funcional — UI completa (timeline, clusters) está planejada para v0.2

### Status em tempo real

- O daemon envia atualizações de status via WebSocket a cada 5 segundos
- A extensão recebe e exibe o número de páginas indexadas e itens pendentes na fila

---

## Integrações externas

**Nenhuma integração externa em v0.1.** O sistema é completamente local:

- Sem APIs de LLM externas
- Sem servidores de sincronização
- Sem analytics ou telemetria
- Sem CDNs ou recursos carregados de fora

A única "integração" é com o próprio Chrome via Native Messaging API (comunicação local entre extensão e daemon).

---

## Fora do escopo atual

### Funcionalidades incompletas (código presente, lógica ausente)

| Funcionalidade | Estado |
|---|---|
| **Opt-in por domínio com prompt** | UI mencionada no GUIA mas não implementada — código faz auto-opt-in silencioso |
| **Pause de captura persistente** | Botão existe no popup mas SW não verifica estado de pausa ao receber `page_viewed` |
| **`chrome.storage`** | Declarado nas permissões mas sem uso no código |
| **`chrome.idle`** | Declarado nas permissões mas sem uso no código |
| **Tabela `queue` no SQLite** | Schema existe mas worker usa canal Go em memória — dados não persistem entre restarts |
| **Busca semântica real** | Infraestrutura pronta (BLOB, cosine, RRF) mas embedder retorna vetor zero — qualidade = BM25 puro |

### Planejado para v0.2 (não implementado)

- Snapshots de páginas (SingleFile HTML para itens marcados com estrela)
- Síntese via LLM local (Ollama bridge)
- UI local completa com timeline, clusters por tópico e visualização de dwell
- Extração de PDFs e transcrições do YouTube
- Suporte a macOS (LaunchAgent)

### Planejado para v0.3 (não implementado)

- Sincronização entre dispositivos com criptografia ponta-a-ponta (E2EE)
- Servidor self-hostable (`docker-compose up`)
- Suporte a Windows

### Limitações conhecidas da implementação atual

- **Busca densa inoperante**: embedder stub retorna zeros; toda a relevância vem do BM25
- **Perda de fila em restart**: itens no canal Go são perdidos se o daemon reiniciar inesperadamente
- **Tombstones ausentes**: exclusão é física; sem mecanismo de propagação futura para sync
- **Porta aleatória a cada restart**: URL da UI local muda; link no popup pode ficar desatualizado
- **ID de extensão no NM manifest**: troca de ID (ex: recarregar extensão em dev) exige rodar o instalador novamente
- **`SearchResponse.total` ausente**: o tipo TypeScript declara o campo, mas a API não o retorna
- **Linux only**: instalador e lifecycle (systemd) funcionam apenas em Linux
