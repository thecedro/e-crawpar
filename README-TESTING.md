# Testing strategy / Estratégia de testes — e-crawpar

Six complementary layers, each with a distinct purpose and guarantee.
No test touches the real network or real credentials: everything runs
against in-memory fakes and an in-memory IMAP server over loopback TCP.

Seis camadas complementares, cada uma com propósito e garantia próprios.
Nenhum teste toca a rede real ou credenciais reais: tudo roda contra fakes
em memória e um servidor IMAP em memória via TCP no loopback.

```
/test/
  unit/          layer 1  pure rules (classifier, normalizer, collector)
  integration/   layer 2  concurrent pipeline, synthetic headers only
  contract/      layer 3  THE safety contract of imapx.IMAPClient
  e2e/           layer 4  full pipeline vs in-memory IMAP server   (tag: e2e)
  features/      layer 5  executable Gherkin specs in Portuguese
    steps/       layer 5  godog step definitions
/testdata/       fixtures shared by every layer (.eml + envelopes.json)
/test/harness/   shared hermetic infrastructure (fake IMAP server, builders)
```

---

## Layer 1 — Unit (`make test-unit`)

**What it guarantees / o que garante**
- Every subject pattern (PT+EN) classifies to exactly one category.
- Ambiguous subjects resolve by the documented priority:
  `security > verification > welcome > receipt > policy`.
- Domain normalization strips transactional prefixes (`no-reply`, `mail`,
  `mkt`, `billing`, `notifications`, …), handles stacked prefixes, keeps
  ccTLDs intact (`foo.co.uk` never becomes `foo.co`) and survives malformed
  hosts without panicking.
- Ignored domains (built-in noise list) never reach the report — even when
  hidden behind a strippable prefix (`news.netflix.com`).
- Collector rules: earliest date wins ("account birthday"), curated sample
  prefers verification then welcome, distinct-sender counting drives the
  multi-sender alert, dated rows sort ascending with undated rows last.

**Coverage gate / meta de cobertura**: every function in `internal/core`
must reach **≥85% statement coverage**, enforced by `make cover-check`
(current worst function ≈ 92%, total ≈ 99%).

Run / rodar:

```sh
make test-unit        # includes -race and the coverage gate
go tool cover -func=coverage.out | grep internal/core   # inspect per-function
```

## Layer 2 — Integration (`make test-integration`)

**What it guarantees**
- The complete concurrent pipeline (jobs → worker pool → collector) loses
  nothing and duplicates nothing under load (500 synthetic headers × up to
  48 workers) — always run under `go test -race`.
- High concurrency does not corrupt aggregate state (categories, samples,
  sender sets stay coherent).
- Terminal and HTML reports render faithfully from aggregated results,
  including HTML-escaping of hostile subjects (XSS-safe).

Run / rodar:

```sh
make test-integration
```

## Layer 3 — Contract (`make test-contract`) ⛔ merge-blocking

**What it guarantees** — the heart of the product's privacy promise. Any
implementation of `imapx.IMAPClient` must:

1. **Be read-only**: every `Select` uses EXAMINE mode (`ReadOnly`), so no
   flag — including `\Seen` — can ever change;
2. **Request only ENVELOPE**: fetch options are compared against exactly
   `&imap.FetchOptions{Envelope: true}`; any BODY/BODYSTRUCTURE/RFC822/
   BINARY item fails the suite;
3. **Paginate in bounded batches**: scanning N messages issues ceil(N/batch)
   FETCH commands, each ≤ batch size, covering 1..N with no gaps and no
   duplicates.

The same reusable suite — `contract.AssertIMAPClientContract(t, factory)` —
runs twice:

- against `contract.FakeHeaderClient`, an honest in-memory implementation
  that refuses non-read-only selects by construction;
- against the **production** `imapx.ClientAdapter` talking over real TCP to
  an in-memory IMAP server, plus a raw wire audit (the transcript must
  contain `ENVELOPE` and never `BODY[`/`RFC822`/`BINARY[`) and a post-run
  check that every message still carries exactly its original `\Seen` flag.

A failure here blocks CI: it means someone taught e-crawpar to open message
bodies or touch mailbox state.

Uma falha aqui bloqueia o CI: significa que alguém ensinou o e-crawpar a ler
corpos de mensagem ou mexer no estado da caixa.

```sh
make test-contract
```

Optional extension point / ponto de extensão opcional: build tag
`external_imap` is reserved for running the same suite against a REAL IMAP
server (Dovecot, Gmail…) when credentials are available. It is never run by
default.

## Layer 4 — End-to-end (`make test-e2e`, build tag `e2e`)

**What it guarantees**
The exact binary path a user takes — dial, login, EXAMINE, batched ENVELOPE
fetch, worker pool, collector, rendered reports — against a full IMAP
server seeded from the business `.eml` fixtures in `/testdata/eml`:

- final domain list, first-seen ordering ("account birthday"), categories,
  sample subjects and multi-sender alerts all match the documented scenarios;
- known noise (`netflix.com`, promo mail) stays out of the report;
- RFC 2047-encoded subjects arrive decoded;
- after the whole run, the mailbox is byte-for-byte untouched: every message
  still has exactly its seeded `\Seen` flag.

Isolated behind `//go:build e2e` so plain `go test ./...` stays fast.

```sh
make test-e2e     # = go test -race -tags e2e ./test/e2e
```

## Layer 5 — BDD (`make test-bdd`)

Executable specifications in Portuguese (Gherkin) living in
`test/features/*.feature`, driven by godog step definitions that reuse the
E2E harness — same fake server, same production pipeline. This layer makes
the *specification itself* runnable and readable by non-developers.

Cenários cobertos: política revelando conta antiga, prioridade de segurança
em assuntos ambíguos, alerta de múltiplos remetentes, filtragem de ruído,
ordenação por nascimento da conta, normalização de prefixo transacional e
e-mails que não evidenciam cadastro.

```sh
make test-bdd
# add new scenarios directly to test/features/*.feature
# adicione cenários novos direto em test/features/*.feature
```

## Layer 6 — Mutation testing (`make test-mutation`)

gremlins mutates the rule-dense code (regex boundaries, comparison operators,
return values) and the existing suites must kill the mutants. Weekly/manual
because it is slow.

Meta / goal: **mutation score ≥70%** on `internal/core`.

```sh
make test-mutation
```

### Surviving mutants log / registro de mutantes sobreviventes

Latest run / última execução: **efficacy 88.46%** (gate ≥70% ✔),
mutant coverage 96.30%, 23 killed, 3 lived, 1 not covered.

| Location | Mutation | Verdict | Why |
|---|---|---|---|
| `internal/core/categories.go:102` | `<` → `<=` in BuildCategories sort comparator | **equivalent — acceptable** | priorities are unique (1..5); no equal pair ever reaches the comparator, so both orders are identical |
| `internal/core/collector.go:73` | `<` → `<=` in category display sort | **equivalent — acceptable** | category ranks are unique; tie case impossible |
| `internal/core/collector.go:95` | `<` → `<=` in first-seen date ordering | **equivalent in practice — acceptable** | only observable when two domains share the exact same FirstSeen day; input order is map-iteration nondeterministic, so no deterministic assertion can kill it without also over-constraining the spec |
| `internal/core/pipeline.go:58` | charset-switch condition negation | **equivalent — acceptable** | the two switch arms are exhaustive for the tested inputs; flipping the condition only swaps which branch returns the documented fallback error |

Killed during development (kept as regression guards):
- `collector.go:55` `rank < a.sampleRank` → killed by
  `TestCollectFirstVerificationSampleWinsOnTie` (two verification subjects:
  the FIRST must remain the curated sample).

*(atualize esta tabela após cada execução)*

---

## What runs when / o que roda quando

| Event | Jobs |
|---|---|
| every PR / todo PR | lint · unit · integration · **contract (blocking)** · bdd · e2e |
| push to main | same as PR |
| weekly + manual | mutation (`mutation.yml`) |

Branch protection suggestion: mark the `contract` job as required for merge
— it IS the "never reads the body" guarantee.

Sugestão de proteção de branch: marque o job `contract` como obrigatório
para merge — ele É a garantia de "nunca lê o corpo".

## Layout notes / notas de estrutura

- `internal/core` holds ALL pure business logic — the primary target of
  layers 1 and 6.
- `internal/imapx` isolates IMAP behind one narrow interface; the production
  adapter and test fakes are interchangeable, which is what makes layers 3-5
  possible without any network.
- `internal/report` renders outputs; `internal/apperr` translates errors.
- Fixtures in `/testdata` are the single source of truth for scenarios across
  layers 2, 4 and 5.

- `internal/core` concentra TODA a lógica de negócio pura — alvo principal
  das camadas 1 e 6.
- `internal/imapx` isola o IMAP atrás de uma interface estreita; adapter de
  produção e fakes são intercambiáveis — é isso que torna as camadas 3-5
  possíveis sem rede alguma.
- `internal/report` gera saídas; `internal/apperr` traduz erros.
- Os fixtures em `/testdata` são a fonte única da verdade dos cenários nas
  camadas 2, 4 e 5.
