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

### Pré-requisitos

- Linux (Ubuntu/Debian/Arch ou derivados)
- Go 1.22+ instalado
- Node.js 20+ instalado
- Google Chrome ou Chromium

### 1. Compilar o daemon

```bash
cd daemon
make build
```

O binário é gerado em `daemon/bin/vbmd`.

### 2. Compilar a extensão

```bash
cd extension
npm install
npm run build
```

Os arquivos prontos ficam em `extension/dist/`.

### 3. Carregar a extensão no Chrome

1. Abra `chrome://extensions/`
2. Ative o **Modo do desenvolvedor** (canto superior direito)
3. Clique em **Carregar sem compactação**
4. Selecione a pasta `extension/dist/`
5. Copie o **ID da extensão** exibido (formato: `abcdefghijklmnopabcdefghijklmnop`)

### 4. Instalar o daemon

```bash
cd daemon
make install
```

O instalador vai:
- Copiar o binário para `~/.local/bin/vbmd`
- Perguntar o ID da extensão que você copiou no passo anterior
- Instalar o manifesto de Native Messaging em `~/.config/google-chrome/NativeMessagingHosts/`
- Registrar e iniciar o serviço systemd do usuário

### 5. Verificar a instalação

```bash
# Ver status do daemon
systemctl --user status vbmd

# Testar endpoint de saúde
PORT=$(python3 -c "import json; print(json.load(open('$HOME/.local/share/vbm/session.json'))['port'])")
curl http://127.0.0.1:$PORT/healthz
# Esperado: {"ok":true}
```

No Chrome, o ícone da extensão deve aparecer na barra. Clique nele — deve mostrar **"Conectado ao daemon"**.

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
| **Pausar** | Suspende a captura por 1 hora, 1 dia ou indefinidamente |
| **Esquecer URL** | Remove uma página específica do índice |
| **Esquecer domínio** | Remove todas as páginas de um site |
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

Na primeira visita a qualquer domínio, a extensão pede confirmação antes de capturar.

---

## Privacidade

- **Local first**: todos os dados ficam em `~/.local/share/vbm/` na sua máquina
- **Sem telemetria**: nenhuma chamada externa é feita (v0.1)
- **Sem LLM externo**: a busca usa modelos locais; nenhum texto é enviado ao OpenAI, Anthropic ou similar
- **Comunicação segura**: o daemon só aceita conexões de `127.0.0.1` e valida um token de sessão gerado a cada reinicialização
- **Direito ao esquecimento**: use o popup ou a UI local para apagar qualquer página, domínio ou intervalo de tempo; a remoção é física (não apenas soft-delete)

### Onde os dados ficam

```
~/.local/share/vbm/
├── vbm.db          # banco SQLite com chunks, embeddings e metadados
└── session.json    # porta e token da sessão atual (chmod 600)
```

---

## Desinstalação

```bash
cd daemon
make uninstall
```

Isso remove o binário, o serviço systemd e o manifesto de Native Messaging.

Os dados em `~/.local/share/vbm/` **não são removidos** automaticamente. Para apagar tudo:

```bash
rm -rf ~/.local/share/vbm/
```

---

## Solução de problemas

### Popup mostra "Daemon não conectado"

```bash
# 1. Verificar se o daemon está rodando
systemctl --user status vbmd

# 2. Se parado, iniciar
systemctl --user start vbmd

# 3. Verificar logs
journalctl --user -u vbmd -n 50

# 4. Confirmar que o ID da extensão está correto no manifesto NM
cat ~/.config/google-chrome/NativeMessagingHosts/com.vbm.daemon.json
```

### Busca não retorna resultados

- Verifique se a extensão não está pausada (ícone cinza = pausada)
- Confirme que o domínio foi aceito na lista de opt-in (popup → status do domínio)
- Aguarde pelo menos 30s em uma página antes de tentar buscar
- Verifique quantas páginas foram indexadas: popup → contador de páginas

### Erro de permissão no manifesto NM

```bash
# Reinstalar o manifesto com o ID correto
cd daemon
EXTENSION_ID=seu_id_aqui bash install/install.sh
```

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
