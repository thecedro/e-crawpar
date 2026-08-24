# Contributing / Contribuir

Thanks for helping improve **e-crawpar**! This project is intentionally friendly to non-technical users — keep that in mind when writing code, docs and error messages.

Obrigado por ajudar a melhorar o **e-crawpar**! Este projeto é deliberadamente amigável a pessoas não técnicas — lembre disso ao escrever código, documentação e mensagens de erro.

---

## 🇧🇷 Português

### Ambiente de desenvolvimento

```bash
git clone https://github.com/OWNER/e-crawpar.git   # OWNER = dono do repositório
cd e-crawpar
go mod tidy
make test-all        # todas as camadas de teste herméticas (com -race)
go vet ./...
gofmt -l .           # não deve imprimir nada
go build             # gera ./e-crawpar
```

Go **1.22 ou superior** é obrigatório. Nenhum serviço externo é necessário para desenvolver ou testar: tudo roda contra servidores IMAP em memória, dentro do próprio processo. Sem credenciais reais, sem rede externa.

Alvos úteis do `make`:

| Alvo | O que faz |
|---|---|
| `make test-unit` | Camada 1 — regras puras, com meta de cobertura em `internal/core` (≥85% por função) |
| `make test-integration` | Camada 2 — pipeline concorrente sob `-race`, sem rede |
| `make test-contract` | Camada 3 — contrato de segurança do cliente IMAP (**bloqueia merge**) |
| `make test-e2e` | Camada 4 — ponta a ponta contra servidor IMAP em memória |
| `make test-bdd` | Camada 5 — cenários Gherkin em português (`/test/features`) |
| `make test-mutation` | Camada 6 — mutação com gremlins focada em `internal/core` |

A estratégia completa de testes — o que cada camada garante e como rodar — está em [`README-TESTING.md`](README-TESTING.md).

### Como o código está organizado

| Caminho | Responsabilidade |
|---|---|
| `main.go` | Só fiação do pipeline: configuração → coleta → pool → coletora → relatório |
| `setup.go` | Assistente interativo da primeira execução + manuseio do `.env` |
| `internal/apperr/` | Tradução de erros brutos em mensagens bilíngues acionáveis |
| `internal/core/` | Lógica de negócio pura: classificação por regex, normalização de domínio, pool de workers, agregação |
| `internal/imapx/` | Interface `IMAPClient` + adapter real (somente-leitura, só ENVELOPE) |
| `internal/report/` | Saída no terminal e página HTML standalone |
| `test/unit` · `test/integration` · `test/contract` · `test/e2e` · `test/features` | As cinco camadas de teste automatizado |
| `test/harness/` | Servidor IMAP fake em memória compartilhado pelas camadas 3–5 |
| `testdata/` | Fixtures (.eml e envelopes sintéticos) usados por várias camadas |

O modelo de concorrência é fixo por design: **um produtor → N workers → uma coletora única**. Por favor, não introduza mutação de estado compartilhado entre workers; a coletora como única escritora é o que mantém tudo sem locks.

### Regras

1. **Nunca registre credenciais em log.** Nem em testes, nem em saída de debug.
2. **UX bilíngue**: mensagens visíveis ao usuário são PT-BR primeiro, inglês logo depois.
3. **Nunca leia o corpo das mensagens.** A ferramenta só pode pedir dados de ENVELOPE. A suíte de contrato (`make test-contract`) fiscaliza isso e **precisa continuar passando sempre** — qualquer mudança que a quebre será recusada.
4. Mantenha o comportamento padrão seguro: somente-leitura (`EXAMINE`) e apenas TLS.
5. Funcionalidade nova precisa de teste. Correção de bug precisa de um teste de regressão que falhe antes do conserto.
6. Commits no formato Conventional Commits (`feat:`, `fix:`, `docs:`, `chore:`, `ci:`, `test:`).

### Enviando mudanças

1. Faça um fork e crie uma branch: `feat/minha-funcionalidade` ou `fix/meu-bug`.
2. Rode localmente: `gofmt -w . && make test-all && go vet ./...`.
3. Abra um PR descrevendo *o que* mudou e *por quê*. O CI precisa ficar verde.

O CI roda em cada PR, separado por camada: formatação, `go vet`, `golangci-lint` (fixado na v2.13.1 — versões antigas produzem erros falsos com Go novo), unit, integration, bdd, e2e e o contrato de segurança, que é obrigatório para merge. Testes de mutação rodam semanalmente. Releases para Windows/macOS/Linux são geradas automaticamente quando uma tag `v*` é enviada.

### Reportando problemas

Inclua: sistema operacional, como executou, o texto exato do erro exibido (nunca cole sua senha de aplicativo!) e o que você esperava que acontecesse.

---

## 🇺🇸 English

### Development environment

```bash
git clone https://github.com/OWNER/e-crawpar.git   # OWNER = repository owner
cd e-crawpar
go mod tidy
make test-all        # every hermetic test layer (with -race)
go vet ./...
gofmt -l .           # must print nothing
go build             # produces ./e-crawpar
```

Go **1.22+** required. No external services are needed to develop or test: everything runs against in-memory IMAP servers inside the process itself. No real credentials, no external network.

Useful `make` targets:

| Target | Purpose |
|---|---|
| `make test-unit` | Layer 1 — pure rules, with coverage gate on `internal/core` (≥85% per function) |
| `make test-integration` | Layer 2 — concurrent pipeline under `-race`, no network |
| `make test-contract` | Layer 3 — IMAP client safety contract (**merge-blocking**) |
| `make test-e2e` | Layer 4 — end-to-end against an in-memory IMAP server |
| `make test-bdd` | Layer 5 — Portuguese Gherkin scenarios (`/test/features`) |
| `make test-mutation` | Layer 6 — gremlins mutation testing scoped to `internal/core` |

The full testing strategy — what each layer guarantees and how to run it — lives in [`README-TESTING.md`](README-TESTING.md).

### How the code is organized

| Path | Responsibility |
|---|---|
| `main.go` | Pipeline wiring only: config → collection → worker pool → collector → report |
| `setup.go` | Interactive first-run wizard + `.env` handling |
| `internal/apperr/` | Translates raw errors into actionable bilingual messages |
| `internal/core/` | Pure business logic: regex classification, domain normalization, worker pool, aggregation |
| `internal/imapx/` | `IMAPClient` interface + real adapter (read-only, ENVELOPE-only) |
| `internal/report/` | Terminal output and standalone HTML page |
| `test/unit` · `test/integration` · `test/contract` · `test/e2e` · `test/features` | The five automated test layers |
| `test/harness/` | In-memory fake IMAP server shared by layers 3–5 |
| `testdata/` | Fixtures (.eml and synthetic envelopes) used across layers |

The concurrency model is fixed by design: **one producer → N workers → one collector**. Please don't introduce shared-state mutations between workers; the single-writer collector is what keeps everything lock-free.

### Ground rules

1. **Never log credentials.** Not in tests, not in debug output.
2. **Bilingual UX**: user-visible messages are PT-BR first, English right after.
3. **Never read message bodies.** The tool may only request ENVELOPE data. The contract suite (`make test-contract`) enforces this and **must always keep passing** — any change that breaks it will be rejected.
4. Keep default behavior safe: read-only (`EXAMINE`), TLS only.
5. New features need tests. Bug fixes need a regression test that fails before the fix.
6. Conventional Commits (`feat:`, `fix:`, `docs:`, `chore:`, `ci:`, `test:`).

### Submitting changes

1. Fork & create a branch: `feat/my-feature` or `fix/my-bugfix`.
2. Run locally: `gofmt -w . && make test-all && go vet ./...`.
3. Open a PR describing *what* changed and *why*. CI must be green.

CI runs on every PR, split by layer: formatting, `go vet`, `golangci-lint` (pinned to v2.13.1 — older builds emit false positives on newer Go), unit, integration, bdd, e2e and the safety contract, which is required for merge. Mutation testing runs weekly. Releases for Windows/macOS/Linux are built automatically when a `v*` tag is pushed.

### Reporting bugs

Include: your OS, how you ran the tool, the exact error text shown (never paste your app password!) and what you expected to happen.

## License / Licença

[MIT](LICENSE)
