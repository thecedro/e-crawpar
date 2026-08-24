# e-crawpar

Crawler IMAP que descobre **em quais serviços você tem conta cadastrada**, analisando apenas os *headers* (From, Subject, Date) da sua própria caixa de e-mail. O corpo das mensagens nunca é baixado.

IMAP crawler that discovers **which services you have accounts with** by analyzing only the *headers* (From, Subject, Date) of your own mailbox. Message bodies are never downloaded.

---

## PT-BR

### Como funciona

```
IMAP (TLS) ──> produtora (ENVELOPE em lotes)
                    │  canal `jobs`
                    ▼
        pool de N workers ── classificam assunto (regex PT/EN)
                             normalizam domínio do remetente
                             filtram ruído
                    │  canal `results`
                    ▼
        coletora única ── agrega sem mutex
                    │
                    ▼
        relatório ordenado pela data da primeira ocorrência
```

- Conexão **somente-leitura** (`EXAMINE`): nenhuma mensagem é marcada como lida.
- Busca apenas o `ENVELOPE` — From, Subject e Date — nunca o corpo.
- Prioridade de classificação: **segurança > verificação > boas-vindas > recibo > política**.
- Agrupa variações do mesmo serviço: `no-reply@amazon.com` e `payments@amazon.com` viram `amazon.com`, com alerta quando há múltiplos remetentes distintos.

### Gerando uma senha de aplicativo

O programa usa **senha de aplicativo**, nunca sua senha principal.

<details>
<summary><strong>Gmail / Google Workspace</strong></summary>

1. Acesse https://myaccount.google.com/security
2. Ative a **verificação em duas etapas** (obrigatório).
3. Em "Senhas de app", gere uma nova (ex.: nome `e-crawpar`).
4. Copie a senha de 16 letras gerada — use-a em `IMAP_APP_PASSWORD`.
5. Host: `imap.gmail.com`.

</details>

<details>
<summary><strong>Outlook / Microsoft 365</strong></summary>

1. Acesse https://account.microsoft.com/security
2. Ative a verificação em duas etapas.
3. Crie uma **senha de aplicativo** em "Segurança avançada".
4. Host: `outlook.office365.com`.

</details>

<details>
<summary><strong>Outros provedores (Fastmail, Zoho, servidor próprio…)</strong></summary>

Procure por "app password" ou "senha de aplicativo" nas configurações de segurança da sua conta. O host IMAP costuma ser `imap.<provedor>` na porta **993** (TLS implícito).

</details>

### Variáveis de ambiente

| Variável | Obrigatória | Padrão | Descrição |
|---|---|---|---|
| `IMAP_HOST` | ✅ | — | Servidor IMAP (ex.: `imap.gmail.com`) |
| `IMAP_USER` | ✅ | — | Seu e-mail completo |
| `IMAP_APP_PASSWORD` | ✅ | — | Senha de aplicativo |
| `IMAP_PORT` | — | `993` | Porta TLS |
| `IMAP_MAILBOX` | — | `INBOX` | Caixa a varrer |
| `IMAP_SINCE` | — | — | Limite inferior da busca, RFC3339 (ex.: `2020-01-01T00:00:00Z`) — acelera caixas grandes |
| `WORKERS` | — | `8` | Goroutines de classificação |
| `BATCH_SIZE` | — | `200` | Mensagens por lote de FETCH |
| `IGNORE_DOMAINS` | — | — | Domínios extras a ignorar, separados por vírgula |

### Como rodar

```bash
go mod tidy && go build

export IMAP_HOST="imap.gmail.com"
export IMAP_USER="voce@gmail.com"
export IMAP_APP_PASSWORD="abcd efgh ijkl mnop"

./e-crawpar            # relatório em texto
./e-crawpar -json      # texto + JSON (para scripts)

# caixa grande? limite o período:
IMAP_SINCE="2019-01-01T00:00:00Z" ./e-crawpar -json > contas.json
```

Exemplo de saída:

```
FIRST SEEN  DOMAIN              OCCUR  CATEGORIES                     SENDERS
2016-11-03  stackoverflow.com      12  welcome, verification               1
2018-06-21  figma.com               7  welcome, security                   2  << MULTIPLE SENDERS
...

Sample subjects:
  stackoverflow.com
    "Verify your email address"
```

### O que é fácil de customizar (tudo em `main.go`)

| O quê | Onde | Observação |
|---|---|---|
| **Padrões de regex** | `categorySpecs` | Case-insensitive; adicione linhas PT-BR/EN por categoria. Ordem = prioridade (`Priority` menor vence). |
| **Domínios ignorados** | `defaultIgnoreDomains` + env `IGNORE_DOMAINS` | Big techs, bancos e monitoramento já vêm pré-carregados. |
| **Prefixos transacionais** | `transactionalPrefixes` | Labels removidos da esquerda do host (`mail.`, `billing.`…) para agrupar serviços. Nunca desce abaixo de 2 labels (`co.uk` seguro). |
| **Tamanho do pool/lotes** | env `WORKERS`, `BATCH_SIZE` | Sem recompilar. |

### Privacidade e segurança

- Credenciais só via variáveis de ambiente — nada é gravado ou logado.
- TLS obrigatório (porta 993); conexão em texto puro não existe no código.
- `EXAMINE` garante que nada muda na caixa.
- Só headers trafegam; corpos ficam no servidor.

---

## EN

### How it works

```
IMAP (TLS) ──> producer (batched ENVELOPE fetch)
                    │  `jobs` channel
                    ▼
        N-worker pool   ── classify subject (PT/EN regexes)
                           normalize sender domain
                           filter noise
                    │  `results` channel
                    ▼
        single collector ── aggregates with zero mutexes
                    │
                    ▼
        report ordered by first-seen date ("account birthday")
```

- **Read-only** connection (`EXAMINE`): no message is ever marked as read.
- Fetches only the `ENVELOPE` — From, Subject and Date — never bodies.
- Classification priority: **security > verification > welcome > receipt > policy**.
- Groups service variations: `no-reply@amazon.com` and `payments@amazon.com` both become `amazon.com`, with an alert when multiple distinct senders appear.

### Creating an app password

The tool uses an **app password**, never your main password.

<details>
<summary><strong>Gmail / Google Workspace</strong></summary>

1. Go to https://myaccount.google.com/security
2. Enable **2-Step Verification** (required).
3. Under "App passwords", create one (e.g. named `e-crawpar`).
4. Copy the generated 16-letter password into `IMAP_APP_PASSWORD`.
5. Host: `imap.gmail.com`.

</details>

<details>
<summary><strong>Outlook / Microsoft 365</strong></summary>

1. Go to https://account.microsoft.com/security
2. Enable two-step verification.
3. Create an **app password** under "Advanced security options".
4. Host: `outlook.office365.com`.

</details>

<details>
<summary><strong>Other providers (Fastmail, Zoho, self-hosted…)</strong></summary>

Look for "app password" in your account's security settings. The IMAP host is usually `imap.<provider>` on port **993** (implicit TLS).

</details>

### Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `IMAP_HOST` | ✅ | — | IMAP server (e.g. `imap.gmail.com`) |
| `IMAP_USER` | ✅ | — | Your full email address |
| `IMAP_APP_PASSWORD` | ✅ | — | App password |
| `IMAP_PORT` | — | `993` | TLS port |
| `IMAP_MAILBOX` | — | `INBOX` | Mailbox to scan |
| `IMAP_SINCE` | — | — | Search lower bound, RFC3339 (e.g. `2020-01-01T00:00:00Z`) — speeds up large mailboxes |
| `WORKERS` | — | `8` | Classification goroutines |
| `BATCH_SIZE` | — | `200` | Messages per FETCH batch |
| `IGNORE_DOMAINS` | — | — | Extra domains to ignore, comma separated |

### Running

```bash
go mod tidy && go build

export IMAP_HOST="imap.gmail.com"
export IMAP_USER="you@gmail.com"
export IMAP_APP_PASSWORD="abcd efgh ijkl mnop"

./e-crawpar            # text report
./e-crawpar -json      # text + JSON (for scripts)

# huge mailbox? limit the time range:
IMAP_SINCE="2019-01-01T00:00:00Z" ./e-crawpar -json > accounts.json
```

### What's easy to customize (all in `main.go`)

| What | Where | Notes |
|---|---|---|
| **Regex patterns** | `categorySpecs` | Case-insensitive; add PT-BR/EN lines per category. Order = priority (lower `Priority` wins). |
| **Ignored domains** | `defaultIgnoreDomains` + env `IGNORE_DOMAINS` | Big techs, banks and monitoring services preloaded. |
| **Transactional prefixes** | `transactionalPrefixes` | Leftmost host labels stripped (`mail.`, `billing.`…) to group services. Never goes below 2 labels (`co.uk` safe). |
| **Pool/batch size** | env `WORKERS`, `BATCH_SIZE` | No rebuild needed. |

### Privacy & security

- Credentials only via environment variables — nothing is written or logged.
- Mandatory TLS (port 993); plaintext connections are impossible in this code.
- `EXAMINE` guarantees your mailbox is untouched.
- Only headers travel; bodies stay on the server.
