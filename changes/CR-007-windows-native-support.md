# CR-007 — Windows Native Build Support

**Data:** 2026-04-12
**Solicitante:** user
**App Version:** 0.1.0
**Status:** implemented
**Urgência:** medium
**Domínios afetados:** Daemon (Go), Install, Docs

---

## Descrição da mudança

Adicionar suporte nativo a Windows para o daemon `vbmd`. Go cross-compila sem mudanças de lógica de negócio — o trabalho principal é: (1) tornar os caminhos de arquivo cross-platform em vez de hardcoded Linux, (2) criar script de instalação PowerShell que registra o NM host manifest no local correto do Windows e configura auto-start via Task Scheduler, (3) adicionar target `make build-windows` ao Makefile.

A extensão Chrome não muda — ela já roda no Windows normalmente.

## Motivação

O daemon atualmente só roda em Linux por três razões: caminhos hardcoded (`~/.local/share/vbm/`), script de instalação em bash, e service via systemd. Nenhuma dessas limitações existe no código de negócio — são apenas convenções de deploy. Com Windows suportado, usuários que desenvolvem em Windows (maioria) podem testar o projeto sem WSL2.

## Comportamento atual

- `vbmd server` e `vbmd nm-host` só funcionam em Linux
- `daemon/internal/nm/host.go:37`: caminho `~/.local/share/vbm/session.json` hardcoded
- `daemon/internal/server/server.go:32`: caminho `~/.local/share/vbm/` hardcoded
- `daemon/install/install.sh`: bash puro, não executa no Windows
- Nenhum target Windows no `Makefile`

## Comportamento desejado

- `vbmd.exe server` roda no Windows, dados em `%APPDATA%\vbm\`
- `vbmd.exe nm-host` executa quando Chrome chama o NM host
- `install.ps1` instala NM manifest em `%LOCALAPPDATA%\Google\Chrome\User Data\NativeMessagingHosts\` e registra Task Scheduler para auto-start
- `make build-windows` gera `bin/vbmd.exe` via cross-compile
- Linux continua com o mesmo comportamento, sem regressão

---

## Análise de impacto

### Entidades afetadas

- `session.json` — mesmo formato, caminho diferente por OS
- `vbm.db` — mesmo schema SQLite, caminho diferente por OS
- `~/.config/vbm/env` → `%APPDATA%\vbm\env` no Windows (mesmo conceito, caminhos diferentes)

### Backend / Daemon

**`daemon/internal/nm/host.go` — `SessionPath()` (linha 37)**

Trocar o caminho hardcoded por função cross-platform:

```go
// Antes (Linux only):
return filepath.Join(home, ".local", "share", "vbm", "session.json"), nil

// Depois (cross-platform):
func vbmDataDir() (string, error) {
    if runtime.GOOS == "windows" {
        appData := os.Getenv("APPDATA")
        if appData == "" {
            return "", fmt.Errorf("APPDATA env not set")
        }
        return filepath.Join(appData, "vbm"), nil
    }
    home, err := os.UserHomeDir()
    if err != nil {
        return "", fmt.Errorf("home dir: %w", err)
    }
    return filepath.Join(home, ".local", "share", "vbm"), nil
}

func SessionPath() (string, error) {
    dir, err := vbmDataDir()
    if err != nil {
        return "", err
    }
    return filepath.Join(dir, "session.json"), nil
}
```

A função `vbmDataDir()` deve ser extraída para um pacote `internal/paths` ou colocada em `nm/host.go` e re-exportada para uso em `server/server.go`.

**`daemon/internal/server/server.go` (linha 28-32)**

Substituir lógica inline de `dataDir` pelo mesmo `vbmDataDir()` acima.

**`daemon/Makefile`**

Adicionar target:

```makefile
build-windows:
	GOOS=windows GOARCH=amd64 go build -o bin/vbmd.exe ./cmd/vbmd/

build-all: build build-windows
```

### Install

**Novo `daemon/install/install.ps1`** (PowerShell 5+)

Passos que o script deve executar:
1. Verificar se `bin\vbmd.exe` existe (senão orientar a rodar `make build-windows` ou baixar release)
2. Copiar `vbmd.exe` para `%LOCALAPPDATA%\vbm\vbmd.exe`
3. Perguntar Extension ID do Chrome
4. Criar NM host manifest em `%LOCALAPPDATA%\Google\Chrome\User Data\NativeMessagingHosts\com.vbm.daemon.json`:
   ```json
   {
     "name": "com.vbm.daemon",
     "description": "Vector Bookmark native daemon bridge",
     "path": "C:\\Users\\YOU\\AppData\\Local\\vbm\\vbmd.exe",
     "type": "stdio",
     "allowed_origins": ["chrome-extension://EXTENSION_ID/"]
   }
   ```
5. Criar Task Scheduler task para auto-start (sem admin via `schtasks /Create /SC ONLOGON /RL LIMITED`):
   ```
   schtasks /Create /TN "VectorBookmarkDaemon" /TR "%LOCALAPPDATA%\vbm\vbmd.exe server" /SC ONLOGON /RL LIMITED /F
   ```
6. Iniciar o daemon imediatamente: `Start-Process -FilePath "$env:LOCALAPPDATA\vbm\vbmd.exe" -ArgumentList "server" -WindowStyle Hidden`
7. Checar se `~/.config/vbm/env` equivalente existe em `%APPDATA%\vbm\env` e carregar vars

**Suporte a `%APPDATA%\vbm\env` no daemon**

O `EnvironmentFile` do systemd não existe no Windows. Alternativa: no startup do daemon (`server.go` ou `main.go`), checar e carregar `%APPDATA%\vbm\env` manualmente se `runtime.GOOS == "windows"`:

```go
if runtime.GOOS == "windows" {
    loadEnvFile(filepath.Join(os.Getenv("APPDATA"), "vbm", "env"))
}
```

`loadEnvFile` parse simples: uma `KEY=value` por linha, sem aspas, comentários com `#`.

### Frontend / Extensão Chrome

Sem mudanças. A extensão já funciona no Chrome/Windows nativamente — o único ponto de integração é o NM host manifest, que o `install.ps1` cria.

### Dados existentes

Sem migração — novos usuários Windows criam `%APPDATA%\vbm\` do zero. Usuários que migraram de WSL2: exportar via `GET /export`, reinstalar no Windows, re-indexar (não há ferramenta de importação, mas a API de export existe para LGPD).

### Integrações externas

`modernc.org/sqlite` é pure Go — funciona em Windows sem CGO. Nenhuma dependência nativa.

### Conflitos com decisões existentes

- `CLAUDE.md §OS`: declarado "Linux (systemd user unit)" como OS suportado. Atualizar para incluir Windows 10/11.
- `DECISIONS.md`: se existir decisão de "Linux-only em v0.1", marcar como superada por este CR.

### CRs relacionadas

- CR-001 a CR-006: nenhuma toca caminhos de arquivo de forma que conflite. CR-006 adicionou `~/.config/vbm/env` — este CR adiciona equivalente Windows.

---

## Decisões necessárias antes de implementar

1. **Localização do binário no Windows**: `%LOCALAPPDATA%\vbm\vbmd.exe` (proposto acima) ou `%PROGRAMFILES%\VectorBookmark\vbmd.exe`? — `%LOCALAPPDATA%` não requer admin; recomendado.

2. **Auto-start via Task Scheduler vs. startup folder**: Task Scheduler é mais robusto; startup folder (`%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup`) é mais simples. Proposta: Task Scheduler com `RL LIMITED` (sem elevação de privilégio).

3. **Versão do PowerShell mínima**: PS 5.1 (built-in no Windows 10+) é suficiente. Não usar PS Core (requer instalação separada).

4. **Chromium support**: o install.ps1 deve também instalar o manifest em `%LOCALAPPDATA%\Chromium\User Data\NativeMessagingHosts\`? Proposta: sim, sem perguntar — custo zero, benefício para usuários de Chromium.

---

## Critério de aceite

- [x] `GOOS=windows GOARCH=amd64 go build ./...` conclui sem erros
- [x] `go test ./...` passa em Linux (sem regressão)
- [ ] `vbmd.exe server` inicia no Windows, cria `%APPDATA%\vbm\session.json` e `vbm.db`
- [ ] `vbmd.exe nm-host` lê `session.json` e retorna `handshake_ok` via stdio
- [x] `install.ps1` cria NM manifest no caminho correto do Chrome
- [ ] Chrome no Windows + extensão carregada de `extension\dist\` → popup mostra "Conectado ao daemon"
- [ ] `%APPDATA%\vbm\env` com `VBM_PORT=7532` fixa a porta corretamente
- [x] Linux não regride: todos os paths Linux continuam funcionando, `go test ./...` verde

---

## Atualizações de documentação necessárias

Após implementação, os seguintes documentos devem ser atualizados:
- [x] `CLAUDE.md` — seção Stack: adicionar Windows 10/11 como OS suportado; seção "Como rodar localmente": adicionar variante Windows
- [x] `README.md` — adicionar seção "Quick start (Windows)" paralela ao "Quick start (Linux)"
- [x] `docs/OPERATIONS.md` — adicionar seção de paths Windows na tabela de variáveis de ambiente e §9 desinstalação Windows
- [x] `DECISIONS.md` — registrar decisão de paths cross-platform via `runtime.GOOS`

---

*CR-007 — gerado em 2026-04-12 — status: implemented*
