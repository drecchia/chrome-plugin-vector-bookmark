# Vector Bookmark — Guia de Uso (pt-BR)

Memória semântica para tudo que você leu no navegador. Busca por significado, não por palavra-chave exata.

---

## O que é isso?

O Vector Bookmark captura automaticamente as páginas em que você passa tempo lendo, indexa o conteúdo em um banco de dados local e permite que você encontre qualquer coisa que já tenha lido usando linguagem natural.

Exemplo: você leu um artigo comparando `tokio` e `async-std` há três semanas e não lembrou de salvar. Basta digitar `@recall comparação tokio async-std` na barra de endereços do Chrome e o artigo aparece.

**Tudo roda na sua máquina.** Nenhum dado é enviado para servidores externos (v0.1).

---

## Componentes

```
Chrome Extension  ──────────────────▶  Daemon (vbmd)
(captura + busca)    localhost HTTP     (indexação + armazenamento)
```

- **Extensão Chrome** — captura páginas, exibe resultados, controla pausa/esquecer
- **Daemon `vbmd`** — processo Go rodando em background, dono do banco de dados, busca híbrida (BM25 + vetores)

---

## Instalação

Há duas formas de rodar o daemon: **nativa** (Go instalado na máquina) ou **Docker** (sem dependência de Go). A extensão Chrome é a mesma nos dois casos.

---

### Opção A — Instalação nativa (Go + systemd)

#### Pré-requisitos

- Linux (Ubuntu/Debian/Arch ou derivados)
- Go 1.22+ instalado
- Node.js 20+ instalado
- Google Chrome ou Chromium

#### 1. Compilar o daemon

```bash
cd daemon
make build
```

O binário é gerado em `daemon/bin/vbmd`.

##### 2. Compilar a extensão

```bash
cd extension
npm install
npm run build
```

Os arquivos prontos ficam em `extension/dist/`.

#### 3. Carregar a extensão no Chrome

1. Abra `chrome://extensions/`
2. Ative o **Modo do desenvolvedor** (canto superior direito)
3. Clique em **Carregar sem compactação**
4. Selecione a pasta `extension/dist/`
5. Copie o **ID da extensão** exibido (formato: `abcdefghijklmnopabcdefghijklmnop`)

#### 4. Instalar o daemon

```bash
cd daemon
make install
```

O instalador vai:
- Copiar o binário para `~/.local/bin/vbmd`
- Registrar e iniciar o serviço systemd do usuário

#### 5. Verificar a instalação

```bash
# Ver status do daemon
systemctl --user status vbmd

# Testar endpoint de saúde (porta padrão 7532)
curl http://127.0.0.1:7532/healthz
# Esperado: {"ok":true}
```

No Chrome, o ícone da extensão deve aparecer na barra. Clique nele — deve mostrar o contador de páginas indexadas.

---

### Opção B — Docker (sem Go na máquina)

Use esta opção se preferir não instalar Go ou quiser isolar o daemon em container.

#### Pré-requisitos

- Linux
- Docker Engine + Docker Compose plugin
- Node.js 20+ (apenas para compilar a extensão)
- Google Chrome ou Chromium

#### 1. Compilar a extensão

```bash
cd extension
npm install
npm run build
```

#### 2. Carregar a extensão no Chrome

1. Abra `chrome://extensions/`
2. Ative o **Modo do desenvolvedor**
3. **Carregar sem compactação** → selecione `extension/dist/`
4. Copie o **ID da extensão**

#### 3. Subir o daemon com Docker Compose

```bash
# Na raiz do projeto
docker compose up -d
```

O que acontece:
- Container sobe o `vbmd server` na porta `7532`
- Docker expõe exclusivamente `127.0.0.1:7532` no host (loopback apenas)
- A extensão conecta diretamente em `127.0.0.1:7532`

#### 5. Verificar

```bash
# Container rodando
docker compose ps

# Saúde do daemon
curl http://127.0.0.1:7532/healthz
# Esperado: {"ok":true}

# Logs em tempo real
docker compose logs -f vbmd
```

#### Gerenciamento do container

```bash
docker compose up -d       # subir
docker compose down        # parar
docker compose restart     # reiniciar
docker compose logs -f     # logs ao vivo
docker compose pull        # atualizar imagem (futuro)
```

#### Dados persistidos

O volume mapeia `~/.local/share/vbm/` do host para dentro do container:

```
~/.local/share/vbm/
├── vbm.db          # banco SQLite — persiste entre restarts do container
└── session.json    # porta e token — reescrito a cada restart
```

Para apagar todos os dados:

```bash
docker compose down
rm -rf ~/.local/share/vbm/
```

#### Diferenças em relação à instalação nativa

| Aspecto | Nativa | Docker |
|---|---|---|
| Dependência de Go | Sim (build) | Não |
| Porta do daemon | `7532` (padrão) | `7532` (fixo) |
| Isolamento | systemd user | Container Docker |
| Auto-start | `systemctl --user enable` | `restart: unless-stopped` |
| Logs | `journalctl --user -u vbmd` | `docker compose logs -f` |
| Dados | `~/.local/share/vbm/` | Mesmo path (volume)

---

## Localização dos dados

O daemon armazena tudo em um único diretório, dependendo do sistema operacional:

| Sistema | Diretório |
|---|---|
| Linux / macOS | `~/.local/share/vbm/` |
| Windows | `%APPDATA%\vbm\` (ex: `C:\Users\<você>\AppData\Roaming\vbm\`) |

Conteúdo do diretório:

```
vbm.db          # banco SQLite — páginas, chunks, embeddings, fila, blacklist
session.json    # porta ativa do daemon (reescrito a cada restart)
```

Para apagar todos os dados indexados (irreversível):

```bash
# Linux/macOS
rm -rf ~/.local/share/vbm/vbm.db

# Windows (PowerShell)
Remove-Item "$env:APPDATA\vbm\vbm.db"
```

---

## Busca semântica com embeddings

Por padrão, o daemon usa um **embedder stub** (vetores zero) — a busca usa apenas BM25 (palavras exatas). Para ativar busca semântica real (encontrar "artigo sobre segurança com IA" buscando por "hacker" ou "pentest"), configure um embedder.

**Não precisa de GPU.** Duas opções:

### Opção A — OpenRouter (mais fácil, sem instalar nada)

```bash
mkdir -p ~/.config/vbm
cat >> ~/.config/vbm/env <<'EOF'
VBM_EMBED_URL=https://openrouter.ai/api/v1/embeddings
VBM_EMBED_FORMAT=openai
VBM_EMBED_API_KEY=sk-or-xxxxxxxxxxxx
VBM_EMBED_MODEL=openai/text-embedding-3-small
EOF
systemctl --user restart vbmd
```

Obter API key gratuita em https://openrouter.ai.

### Opção B — Ollama (100% local, nenhum dado sai da máquina)

```bash
curl -fsSL https://ollama.com/install.sh | sh
ollama pull nomic-embed-text
mkdir -p ~/.config/vbm
cat >> ~/.config/vbm/env <<'EOF'
VBM_EMBED_URL=http://127.0.0.1:11434/api/embeddings
VBM_EMBED_MODEL=nomic-embed-text
EOF
systemctl --user restart vbmd
```

### Re-embedar páginas já capturadas

Páginas indexadas antes da configuração ficam sem embedding real. Clique no botão **"Re-embed pages (semantic search)"** no popup da extensão — ele aparece quando há páginas indexadas e mostra progresso em tempo real.

Para detalhes (modelos alternativos, curl, troubleshooting), veja `docs/OPERATIONS.md §4`.

---

## Uso diário

### Busca pela barra de endereços

Digite `@recall` seguido de espaço na barra de endereços do Chrome, depois sua busca em linguagem natural:

```
@recall artigo sobre tokio vs async-std
@recall tutorial de docker compose com postgres
@recall preço de passagem para Lisboa
```

Os 5 melhores resultados aparecem como sugestões com trecho do conteúdo, domínio e data da visita. Pressione Enter ou clique para abrir.

#### Dicas de query

A busca faz **fusão RRF** (BM25 + cosine). Query de 2 a 5 palavras dá o melhor resultado — termo único reduz o BM25 a ranking simples, e queries longas demais diluem a semântica.

**Exemplos que funcionam bem:**

```
@recall pagamento pix brasil
@recall react hooks useeffect cleanup
@recall comparação tokio async-std performance
@recall docker compose volume persistência
@recall tutorial prometheus scrape config
```

**Exemplos ruins** (muito curtos ou genéricos):

```
@recall docker          # muito amplo — milhares de páginas match
@recall configuração    # zero sinal semântico
```

Se a busca não achar o que você lembra ter lido:
- Tente termos do trecho específico que você lembra, não só o tópico
- Adicione o domínio mental: `@recall stackoverflow async await python`
- Se configurou embeddings reais (§Busca semântica), queries em linguagem natural longas funcionam melhor que palavras-chave soltas

### Interface local completa

Para uma visão mais detalhada, acesse a URL local do daemon:

```
http://127.0.0.1:PORTA/ui
```

A porta aparece no popup da extensão. A interface local oferece:
- Lista de todas as páginas indexadas
- Campo de busca com snippets completos
- Painel para apagar registros

### Popup da extensão

Clique no ícone da extensão na barra do Chrome para:

| Ação | O que faz |
|---|---|
| **Pausar / Retomar** | Suspende ou retoma a captura passiva |
| **Index this page now** | Força indexação imediata da aba atual (sem esperar 30s) |
| **Re-embed pages** | Re-embeda páginas já indexadas com o embedder ativo (busca semântica) |
| **Esquecer URL** | Remove uma página específica do índice |
| **Esquecer domínio** | Remove todas as páginas de um site |
| **✓ This page is indexed** | Indicador verde quando a página atual já está no índice; botão "Remove" apaga a página |
| **⭐ (botão estrela)** | Indexa a página com `star_rank=1` — aparece antes nos resultados de busca (boost 1.5×) |
| **Index this page now / ⭐** | Desabilitados quando o domínio está na blocklist ou no denylist estático |
| **Block this site** | Bloqueia o domínio da aba atual via daemon; gerenciamento completo em Open UI → Blocklist |
| **Abrir UI completa** | Abre `http://127.0.0.1:PORTA/ui` no navegador |

---

## Como a captura funciona

1. Você abre uma página e fica **30 segundos ou mais** lendo (tempo visível, não apenas aberto)
2. A extensão extrai o texto principal com Readability (ignora menus, rodapés, anúncios)
3. O texto é enviado ao daemon local via HTTP
4. O daemon divide em chunks de 512 tokens, gera embeddings e indexa no SQLite
5. A página fica disponível para busca semântica

### O que NÃO é capturado (por padrão)

A extensão bloqueia automaticamente:

- Janelas anônimas (incógnito) — **sempre bloqueado**
- Páginas de login, OAuth, SSO, MFA
- Campos com senha ou cartão de crédito — captura pausada enquanto focado
- Domínios sensíveis: bancos, `.gov`, `.mil`, portais de saúde, gerenciadores de senha (1Password, Bitwarden, etc.), webmail (Gmail, Outlook)
- URLs com `/login`, `/checkout`, `/payment`, `/password`, `/auth/`
- Domínios na **blacklist do usuário** — adicionados via popup (seção "Blocked sites"); usa suffix match, então `example.com` também bloqueia `sub.example.com`

Na primeira visita a qualquer domínio, a extensão pede confirmação antes de capturar.

---

## Privacidade

- **Local first**: todos os dados ficam em `~/.local/share/vbm/` na sua máquina
- **Sem telemetria**: nenhuma chamada externa é feita (v0.1)
- **Sem LLM externo por padrão**: com `StubEmbedder` ou Ollama, nenhum texto sai da máquina. Se usar OpenRouter (`VBM_EMBED_API_KEY`), chunks de texto são enviados à API para gerar embeddings
- **Comunicação segura**: o daemon só aceita conexões de `127.0.0.1` (bind exclusivo em loopback)
- **Direito ao esquecimento**: use o popup ou a UI local para apagar qualquer página, domínio ou intervalo de tempo; a remoção é física (não apenas soft-delete)

### Onde os dados ficam

```
~/.local/share/vbm/
└── vbm.db          # banco SQLite com chunks, embeddings e metadados
```

### FAQ de Privacidade

**Algum dado é enviado para servidores externos?**
Depende do embedder. O daemon em si não tem telemetria e faz bind exclusivo em `127.0.0.1`. Se você usar **Ollama** ou o stub padrão, nenhum dado sai da máquina. Se usar **OpenRouter** (`VBM_EMBED_API_KEY`), o texto dos chunks é enviado à API do OpenRouter para gerar embeddings — o conteúdo das páginas que você leu é transmitido. Escolha Ollama se isso for uma preocupação.

**E se eu abrir uma página sensível (banco, login, CPF, saúde)?**
Três camadas de proteção:
1. **Janelas anônimas nunca são capturadas** (`incognito: not_allowed` no manifest — não é configurável).
2. **Denylist de 24 domínios + padrões de URL** (`.gov`, `.mil`, `/login`, `/checkout`, bancos, webmail, password managers) bloqueia captura antes mesmo do envio ao daemon.
3. **Detecção de campos sensíveis no content script** — se a página tem `input[type=password]` ou campo de cartão focado, a captura fica suspensa enquanto o campo estiver em uso.

**Posso apagar uma página, domínio ou período específico?**
Sim, de três formas:
- **Popup da extensão**: campo "Esquecer URL" ou "Esquecer domínio".
- **UI local** em `http://127.0.0.1:PORTA/ui`: botão ao lado de cada página.
- **API direta** (útil para scripts): `DELETE /forget` com `{"type":"url|domain|timerange","value":"..."}` — veja `docs/OPERATIONS.md §5.3`.

A remoção é **física**: `DELETE FROM pages` + rebuild do FTS5, sem soft-delete, sem lixeira.

**Por quanto tempo meus dados ficam armazenados?**
Indefinidamente por padrão. Para retenção automática (LGPD-friendly), configure `VBM_TTL_DAYS=N` — páginas mais antigas que N dias são removidas por uma goroutine diária. Ver `docs/OPERATIONS.md §5`.

**Como faço backup / exportação dos meus dados?**

```bash
# Backup cru do banco
cp ~/.local/share/vbm/vbm.db ~/backup-$(date +%F).db

# Exportação estruturada via API (JSON, sem embeddings)
curl -s "http://127.0.0.1:7532/export" > export.json
```

**Outro usuário na mesma máquina consegue ler meus dados?**
Não por padrão — `~/.local/share/vbm/` herda permissões da home do usuário (tipicamente `700`). O `session.json` é explicitamente `chmod 600`. Um usuário com permissão `sudo`, entretanto, consegue ler — o Vector Bookmark não é defesa contra root.

**O que acontece se o daemon crashar no meio de uma ingestão?**
A fila tem capacidade 256 e é drenada com timeout de 30s no shutdown gracioso (SIGTERM). Crash forçado (SIGKILL) perde o que estiver em vôo. O banco tem WAL mode, então não corrompe.

---

## Desinstalação

```bash
cd daemon
make uninstall
```

Isso remove o binário e o serviço systemd.

Os dados em `~/.local/share/vbm/` **não são removidos** automaticamente. Para apagar tudo:

```bash
rm -rf ~/.local/share/vbm/
```

---

## Solução de problemas

### Popup mostra erro de conexão

```bash
# 1. Verificar se o daemon está rodando
systemctl --user status vbmd

# 2. Se parado, iniciar
systemctl --user start vbmd

# 3. Testar conectividade (porta padrão 7532)
curl http://127.0.0.1:7532/healthz

# 4. Verificar logs
journalctl --user -u vbmd -n 50
```

Se usar porta customizada, abra o popup da extensão → seção **Daemon** → ajuste o campo de porta.

### Busca não retorna resultados

- Verifique se a extensão não está pausada (ícone cinza = pausada)
- Confirme que o domínio foi aceito na lista de opt-in (popup → status do domínio)
- Aguarde pelo menos 30s em uma página antes de tentar buscar
- Verifique quantas páginas foram indexadas: popup → contador de páginas

### Daemon não inicia após reboot

```bash
# Habilitar início automático
systemctl --user enable vbmd
systemctl --user start vbmd

# Garantir que o systemd do usuário persiste após logout
sudo loginctl enable-linger $USER
```

---

## Roadmap

| Versão | Funcionalidades |
|---|---|
| **v0.1** *(atual)* | Captura passiva, busca semântica local, Linux |
| **v0.2** | Snapshots de páginas, síntese via Ollama (LLM local), UI com timeline e clusters, macOS |
| **v0.3** | Sincronização entre dispositivos com E2EE, servidor self-hosted (`docker-compose up`) |

---

## Atalhos rápidos

| Ação | Como fazer |
|---|---|
| Buscar | `@recall <consulta>` na barra de endereços |
| Pausar captura | Clique no ícone → Pausar |
| Ver UI completa | Clique no ícone → Abrir UI completa |
| Apagar uma página | UI completa → botão Esquecer ao lado da página |
| Ver logs do daemon | `journalctl --user -u vbmd -f` |
| Reiniciar daemon | `systemctl --user restart vbmd` |
| Ver status | `systemctl --user status vbmd` |
| Testar daemon | `curl http://127.0.0.1:7532/healthz` |
| Configurar porta | Popup → seção Daemon |
