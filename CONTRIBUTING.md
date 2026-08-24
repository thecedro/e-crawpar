# Contributing / Contribuir

Thanks for helping improve **e-crawpar**! This project is intentionally friendly to non-technical users — keep that in mind when writing code, docs and error messages.

Obrigado por ajudar a melhorar o **e-crawpar**! Este projeto é deliberadamente amigável a pessoas não técnicas — lembre disso ao escrever código, documentação e mensagens de erro.

## Dev setup / Ambiente de desenvolvimento

```bash
git clone https://github.com/OWNER/e-crawpar.git   # OWNER = repo owner
cd e-crawpar
go mod tidy
go test ./...        # unit tests must pass / testes unitários devem passar
go vet ./...         # static analysis
gofmt -l .           # must print nothing / não deve imprimir nada
go build             # produces ./e-crawpar
```

Go **1.22+** required. No external services needed for tests — everything runs offline.

Go **1.22+** obrigatório. Nenhum serviço externo é necessário para os testes — tudo roda offline.

## How the code is organized / Organização do código

| File | Responsibility |
|---|---|
| `main.go` | Pipeline: config, IMAP collection, worker pool, collector |
| `setup.go` | Interactive first-run wizard + `.env` handling |
| `report.go` | Text and HTML report rendering |
| `friendly.go` | User-facing error translation (PT-BR/EN) |
| `main_test.go` | Unit tests (classification, normalization, reports, errors) |

The concurrency model is fixed by design: one producer → N workers → one collector. Please don't introduce shared-state mutations between workers; the single-writer collector is what keeps it lock-free.

O modelo de concorrência é fixo por design: um produtor → N workers → uma coletora. Por favor, não introduza mutação de estado compartilhado entre workers; a coletora de escritor único é o que mantém tudo sem locks.

## Ground rules / Regras

1. **Never log credentials.** Not in tests, not in debug output.
2. **Bilingual UX**: user-visible messages are PT-BR first, EN right after.
3. **No message bodies.** The tool must only ever request ENVELOPE data.
4. Keep the default behavior safe: read-only (`EXAMINE`), TLS only.
5. New features need tests. Bug fixes need a regression test.
6. Conventional Commits (`feat:`, `fix:`, `docs:`, `chore:`, `ci:`, `test:`).

## Submitting changes / Enviando mudanças

1. Fork & create a branch: `feat/my-feature` or `fix/my-bugfix`.
2. Run `gofmt -w . && go test ./... && go vet ./...` locally.
3. Open a PR describing *what* changed and *why*. CI must be green.

CI runs on every PR: format check, `go vet`, `golangci-lint` and the test suite. Releases are built automatically for Windows/macOS/Linux when a `v*` tag is pushed.

CI roda em todo PR: checagem de formatação, `go vet`, `golangci-lint` e os testes. Releases são geradas automaticamente para Windows/macOS/Linux quando uma tag `v*` é enviada.

## Reporting bugs / Reportando problemas

Include: OS, how you ran the tool, the exact error text shown (never paste your app password!), and what you expected to happen.

Inclua: sistema operacional, como executou, o texto exato do erro (nunca cole sua senha de aplicativo!) e o que você esperava que acontecesse.
