# 🔎 e-crawpar

**Descubra em quais sites e serviços você tem conta — usando só os cabeçalhos dos seus próprios e-mails.**

Você já se perguntou *"em que sites eu me cadrei nesses anos todos?"*. O e-crawpar responde isso varrendo a sua própria caixa de entrada e montando uma lista dos serviços que mandaram boas-vindas, pedidos de verificação, alertas de segurança ou recibos para você.

> 🛡️ **Em resumo:** nada sai do seu computador. O programa lê apenas remetente, assunto e data das mensagens — nunca o conteúdo delas — direto do servidor do seu e-mail, por conexão criptografada.

---

## 🇧🇷 Português

### Como funciona (sem termos técnicos)

Seu e-mail guarda pistas: quando você cria conta num site, quase sempre chega um e-mail tipo *"Bem-vindo!"* ou *"Confirme seu e-mail"*. O e-crawpar procura esses padrões, descobre de qual serviço veio cada um e organiza tudo numa lista com a **data da primeira aparição** — praticamente a *data de nascimento* da sua conta naquele site.

### Instalação

**Opção A — Baixar pronto (recomendado para quem não programa):**

1. Abra a aba [Releases](https://github.com/OWNER/e-crawpar/releases) deste repositório *(troque OWNER pelo dono do repo ao publicar)*.
2. Baixe o arquivo do seu sistema:
   - Windows: `e-crawpar-windows-amd64.zip`
   - Mac (Intel): `e-crawpar-darwin-amd64.tar.gz`
   - Mac (M1/M2/M3…): `e-crawpar-darwin-arm64.tar.gz`
   - Linux: `e-crawpar-linux-amd64.tar.gz` ou `...-arm64.tar.gz`
3. Extraia o arquivo. Dentro há um programinha chamado `e-crawpar`.

> 💡 No Windows, se aparecer um aviso azul do SmartScreen, clique em **"Mais informações" → "Executar assim mesmo"** — acontece porque o programa não paga certificado digital, não porque haja problema.

**Opção B — Compilar você mesmo:** instale o [Go](https://go.dev/dl/) e rode `go mod tidy && go build`.

### Antes de usar: crie uma "senha de app"

Programas externos não podem entrar no seu e-mail com a senha normal (isso é bom!). Você precisa criar uma **senha de aplicativo**: uma senha especial, só para este programa, que pode ser revogada quando quiser. Escolha abaixo o passo a passo do seu provedor.

#### 📧 Gmail (google.com)

1. Acesse **myaccount.google.com** e faça login.
2. No menu lateral, clique em **Segurança** (ícone de escudo).
3. Localize **"Verificação em duas etapas"**. Se estiver desativada, ative-a seguindo as instruções na tela — é obrigatória para gerar senhas de app. Se já estiver ativa, siga para o passo 4.
4. Volte para a página **Segurança** e procure **"Senhas de app"**. Dica: digite "senhas de app" na barra de busca do topo da página.
5. Digite um nome para identificar, por exemplo `e-crawpar`, e clique em **Criar**.
6. Uma senha de **16 letras amarelas** aparecerá na tela (algo como `abcd efgh ijkl mnop`). Copie-a.
7. Pronto! Use essa senha quando o e-crawpar pedir. Para revogá-la um dia, volte nessa mesma tela.

> ⚠️ Contas de empresa/escola (Google Workspace): se "Senhas de app" não aparecer, o administrador da organização bloqueou o recurso — fale com ele.

#### 📧 Outlook / Hotmail / Microsoft 365

1. Acesse **account.microsoft.com** e faça login.
2. Clique em **Segurança** no menu superior.
3. Clique em **"Opções de segurança avançadas"** (pode estar dentro de "Gerenciar como eu entro").
4. Role até **Verificação em duas etapas** e ative-a, se ainda não estiver. É requisito para senhas de app.
5. Na mesma página, procure **"Senhas de aplicativo"** e clique em **Criar uma nova senha de aplicativo**.
6. Uma senha será gerada. Copie-a e use no e-crawpar.

> ⚠️ Algumas contas Microsoft pessoais mais recentes podem não exibir essa opção (a Microsoft está migrando para outro sistema de login). Nesse caso o e-crawpar ainda não conseguirá conectar nessa conta específica.

#### 📧 Yahoo Mail

1. Acesse **account.yahoo.com/security** e faça login.
2. Confirme que a **verificação em duas etapas** está ativada ("Chave de conta"). Se não estiver, ative primeiro.
3. Na mesma página de segurança, procure **"Gerar senha de aplicativo"** e clique.
4. Em "Selecionar seu aplicativo", escolha **Outro aplicativo** e digite `e-crawpar`.
5. Clique em **Gerar senha**. Copie a senha exibida e use no e-crawpar.

### Primeira execução (modo guiado)

Na primeira vez, o programa **pergunta tudo sozinho**: qual é o provedor, seu e-mail e a senha de app. Ele **testa a conexão antes de salvar** qualquer coisa e cria o arquivo `.env` automaticamente — nas próximas vezes você nem vê essas perguntas de novo.

```bash
# Windows: dê dois cliques no e-crawpar.exe OU abra o PowerShell na pasta e rode:
.\e-crawpar.exe

# Mac/Linux:
./e-crawpar
```

O assistente vai pedir (nesta ordem):

1. **Provedor** — digite `1` (Gmail), `2` (Outlook), `3` (Yahoo) ou `4` (outro).
2. **Seu e-mail completo** — ex.: `voce@gmail.com`.
3. **Senha de app** — cole aquela senha de 16 letras. Enquanto digita nada aparece: é normal, é proteção contra olhares alheios.

Se algo der errado, ele diz **o quê e como resolver**, em português, e deixa você tentar de novo (até 3 vezes). Só salva o `.env` depois que a conexão funcionar.

### Usando o programa

Depois da primeira configuração:

```bash
./e-crawpar              # relatório no terminal
./e-crawpar -html        # também gera e-crawpar-report.html (tabela bonita p/ navegador)
./e-crawpar -json        # também imprime JSON (para quem programa)
```

Abra o `e-crawpar-report.html` dando dois cliques nele: dá para **buscar** e **clicar nos títulos das colunas para ordenar**. Funciona offline.

### Entendendo o relatório

| Coluna | Significado |
|---|---|
| **Primeira vez** | Data do e-mail mais antigo encontrado daquele serviço ≈ quando sua conta nasceu lá |
| **Domínio** | O serviço (ex.: `figma.com`) |
| **E-mails** | Quantas mensagens daquele serviço bateram nos padrões |
| **Categorias** | Tipos de e-mail encontrados: `security` (alertas de login/senha), `verification` (confirmações), `welcome` (boas-vindas), `receipt` (recibos/pagamentos), `policy` (mudanças de termos) |
| **Remetentes distintos** | Se for maior que 1 com aviso ⚠, o serviço usou vários endereços para falar com você |

Big techs, bancos conhecidos e serviços de monitoramento vêm filtrados por padrão para o resultado ficar limpo (dá para mudar — ver abaixo).

### 🔒 Privacidade

- **Nada sai da máquina.** O programa conversa apenas com o servidor de e-mail do seu provedor, lê os cabeçalhos e mostra o resultado na sua tela. Não existe telemetria, upload, "sincronização" nem chamada para nenhum outro serviço.
- **Nunca lê o conteúdo dos e-mails.** Tecnicamente impossível: ele pede ao servidor somente o bloco `ENVELOPE` (remetente, assunto, data). O corpo da mensagem sequer é baixado.
- **Não marca nada como lido.** A caixa é aberta em modo somente-leitura (`EXAMINE`); suas mensagens continuam intocadas.
- **Sua senha fica só no `.env` local**, com permissão restrita ao seu usuário e fora do versionamento git. Revogue a senha de app no painel do provedor quando terminar — ela morre na hora.

### 🩹 Problemas comuns

| Mensagem | O que significa | O que fazer |
|---|---|---|
| "Login recusado…" | Senha incorreta ou senha normal usada | Gere/use a **senha de APP** de 16 letras, não a senha do e-mail. Confira espaços extras ao colar. |
| "O provedor bloqueou este tipo de login" | Verificação em duas etapas desativada | Ative a 2FA no painel de segurança e gere a senha de app depois disso. |
| "Servidor … não encontrado" | Endereço do servidor digitado errado | Gmail=imap.gmail.com · Outlook=outlook.office365.com · Yahoo=imap.mail.yahoo.com |
| "Tempo esgotado ao conectar" | Internet instável, firewall ou VPN | Desative VPN, teste outra rede, tente novamente. |
| "Recusou a conexão" | Porta errada | Mantenha a porta **993** (padrão do programa). |
| Relatório vazio ou muito curto | Filtro removeu tudo, ou a caixa não tem esses e-mails | Experimente `IGNORE_DOMAINS="" ./e-crawpar` para desligar o filtro e confira se é a caixa certa. |
| Caixa gigante demorando muito | Muitos anos de e-mails | Rode com limite de data: `IMAP_SINCE="2020-01-01T00:00:00Z" ./e-crawpar` |
| "Não consegui salvar o .env" | Pasta sem permissão de escrita | Mova o programa para uma pasta sua (ex.: Documentos) e rode de lá. |

### Customização avançada (para quem mexe com código)

Tudo fica em `main.go`, bem comentado e bilíngue:

| O quê | Onde |
|---|---|
| Padrões de regex por categoria (PT/EN) | `categorySpecs` — ordem = prioridade |
| Domínios ignorados | `defaultIgnoreDomains` + variável `IGNORE_DOMAINS=a.com,b.com` |
| Prefixos transacionais agrupados (`mail.`, `billing.`…) | `transactionalPrefixes` |
| Workers e tamanho de lote | variáveis `WORKERS`, `BATCH_SIZE` |

Detalhes técnicos, arquitetura do pipeline e guias de contribuição: [`CONTRIBUTING.md`](CONTRIBUTING.md).

---

## 🇺🇸 English

**Find out which services hold an account of yours — using only the headers of your own e-mails.**

Ever wondered *"where did I sign up all these years?"*. e-crawpar scans your own inbox, spots welcome / verification / security / receipt patterns and builds a list of services with the **first-seen date** — basically each account's *birthday*.

> 🛡️ **TL;DR:** nothing leaves your computer. It reads sender, subject and date only — never message bodies — straight from your provider over encrypted connection.

### Install

**Option A — Download a ready build (non-technical):**

1. Open the [Releases](https://github.com/OWNER/e-crawpar/releases) tab *(replace OWNER when publishing)*.
2. Grab your platform file: `e-crawpar-windows-amd64.zip`, `e-crawpar-darwin-arm64.tar.gz`, `e-crawpar-linux-amd64.tar.gz`…
3. Extract it and you'll find the `e-crawpar` executable.

> 💡 On Windows, if SmartScreen shows up, choose **"More info" → "Run anyway"** — that happens because we don't pay for a code-signing certificate, not because something is wrong.

**Option B — Build from source:** install [Go](https://go.dev/dl/), then `go mod tidy && go build`.

### Create an app password first

Third-party apps cannot log in with your regular password. You need an **app password** — a special, revocable one.

<details>
<summary><strong>Gmail step by step</strong></summary>

1. Go to **myaccount.google.com** and sign in.
2. Sidebar → **Security** (shield icon).
3. Find **"2-Step Verification"**. If off, turn it on following the on-screen steps — it's required for app passwords. If already on, go to step 4.
4. Back on **Security**, find **"App passwords"**. Tip: type "app passwords" in the page search bar.
5. Type a name such as `e-crawpar` and click **Create**.
6. A **16-letter password** appears (like `abcd efgh ijkl mnop`). Copy it.
7. Done! Use it whenever e-crawpar asks. Revoke anytime from the same screen.

> ⚠️ Work/school accounts (Google Workspace): if "App passwords" is missing, your admin disabled it — talk to them.

</details>

<details>
<summary><strong>Outlook / Hotmail / Microsoft 365 step by step</strong></summary>

1. Go to **account.microsoft.com** and sign in.
2. Top menu → **Security**.
3. Click **"Advanced security options"** (may be under "Manage how I sign in").
4. Scroll to **Two-step verification** and enable it if needed — required for app passwords.
5. On the same page find **"App passwords"** → **Create a new app password**.
6. Copy the generated password and use it in e-crawpar.

> ⚠️ Recent personal Microsoft accounts may not show this option (Microsoft is migrating to another sign-in system). In that case e-crawpar cannot connect to that particular account yet.

</details>

<details>
<summary><strong>Yahoo Mail step by step</strong></summary>

1. Go to **account.yahoo.com/security** and sign in.
2. Make sure **two-step verification** ("Account key") is enabled first.
3. On the same security page, click **"Generate app password"**.
4. Under "Select your app", pick **Other app** and type `e-crawpar`.
5. Click **Generate password**, copy it and use it in e-crawpar.

</details>

### First run (guided mode)

The program **asks everything itself**: provider, e-mail and app password. It **validates the connection before saving anything** and creates the `.env` file automatically — next runs skip the questions.

```bash
./e-crawpar        # Windows: .\e-crawpar.exe
```

The wizard asks, in order: **provider number**, **your full e-mail**, **app password** (hidden while typing — that's normal). On failure it explains what happened in plain words and lets you retry (up to 3 times).

### Usage

```bash
./e-crawpar              # terminal report
./e-crawpar -html        # also writes e-crawpar-report.html (searchable table)
./e-crawpar -json        # also prints JSON
```

Double-click `e-crawpar-report.html`: you can **search** and **click column titles to sort**. Works offline.

### Understanding the report

| Column | Meaning |
|---|---|
| **First seen** | Oldest matched email from that service ≈ when your account was born there |
| **Domain** | The service (e.g. `figma.com`) |
| **Emails** | How many messages matched the patterns |
| **Categories** | `security` (login/password alerts), `verification` (confirmations), `welcome`, `receipt`, `policy` |
| **Distinct senders** | >1 with ⚠ means the service used several addresses to reach you |

Big techs, known banks and monitoring services are filtered out by default.

### 🔒 Privacy

- **Nothing leaves your machine.** The tool only talks to your own e-mail server, reads headers and prints results locally. No telemetry, uploads or third-party calls exist in the code.
- **Never reads message bodies.** Technically impossible: it requests only the `ENVELOPE` block (sender, subject, date).
- **Marks nothing as read.** The mailbox is opened read-only (`EXAMINE`).
- **Your password stays in the local `.env`**, owner-only permissions, git-ignored. Revoke the app password at your provider when done — it dies instantly.

### 🩹 Troubleshooting

| Message | Meaning | Fix |
|---|---|---|
| "Login rejected…" | Wrong password or regular password used | Generate/use the 16-letter **APP PASSWORD**, check for extra spaces when pasting. |
| "Provider blocked this kind of login" | 2-step verification is off | Enable 2FA in your account security panel, then create the app password. |
| "Server … not found" | Typo in server address | Gmail=imap.gmail.com · Outlook=outlook.office365.com · Yahoo=imap.mail.yahoo.com |
| "Timed out connecting" | Unstable internet, firewall or VPN | Disable VPN, try another network, retry. |
| "Refused the connection" | Wrong port | Keep port **993** (the default). |
| Empty or short report | Filter removed everything, or mailbox lacks those emails | Try `IGNORE_DOMAINS="" ./e-crawpar` and double-check the mailbox. |
| Huge mailbox taking long | Many years of mail | Limit the range: `IMAP_SINCE="2020-01-01T00:00:00Z" ./e-crawpar` |
| "Could not save .env" | Folder is read-only | Move the binary to a writable folder (e.g. Documents) and run from there. |

### Advanced customization & development

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for architecture, code layout, tests and contribution guidelines. Regex patterns live in `categorySpecs`, ignored domains in `defaultIgnoreDomains` + env `IGNORE_DOMAINS`, transactional prefixes in `transactionalPrefixes`, pool sizing in `WORKERS`/`BATCH_SIZE`.

## License / Licença

[MIT](LICENSE)
