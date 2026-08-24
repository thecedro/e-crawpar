// Layer 1 — UNIT TESTS: RFC 2047 subject decoding and the worker's noise
// filter (unmatched subjects and ignored domains produce no result).
//
// Camada 1 — TESTES DE UNIDADE: decodificação RFC 2047 de assuntos e o
// filtro de ruído do worker (assuntos sem categoria e domínios ignorados não
// geram resultado).
package unit_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"e-crawpar/internal/core"
)

func TestDecodeSubject(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain subject", "plain subject"},
		{"=?utf-8?b?QmVtLXZpbmRvIGFvIFNwb3RpZnk=?=", "Bem-vindo ao Spotify"},
		{"=?iso-8859-1?q?Configura=E7=F5es=20de=20seguran=E7a?=", "Configurações de segurança"},
		{"Senha alterada com sucesso", "Senha alterada com sucesso"}, // already plain / já puro
		{"=?x-unknown?Q?weird?=", "=?x-unknown?Q?weird?="},           // raw fallback / texto bruto
		{"", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, core.DecodeSubject(tt.in), "input %q", tt.in)
	}
}

// d builds a UTC date helper for fixtures.
func d(y int, m time.Month, day int) time.Time {
	return time.Date(y, m, day, 0, 0, 0, 0, time.UTC)
}

func testIgnore(extra ...string) map[string]bool {
	m := map[string]bool{}
	for _, e := range extra {
		m[e] = true
	}
	return m
}

func drainResults(results <-chan core.Result) []core.Result {
	var got []core.Result
	for r := range results {
		got = append(got, r)
	}
	return got
}

// runOneWorker drives a single Worker to completion and closes results, so
// drainResults terminates. (Closing is RunWorkerPool's job in production;
// these tests exercise Worker in isolation.)
//
// runOneWorker executa um único Worker até o fim e fecha results, para que
// drainResults termine. (Fechar é papel do RunWorkerPool em produção; estes
// testes exercitam o Worker isolado.)
func runOneWorker(jobs <-chan core.Job, ignore map[string]bool) []core.Result {
	results := make(chan core.Result, 16)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		core.Worker(jobs, results, buildCats(), ignore)
	}()
	wg.Wait()
	close(results)
	return drainResults(results)
}

func TestWorkerFiltersUnmatchedSubjects(t *testing.T) {
	jobs := make(chan core.Job, 2)
	jobs <- core.Job{Subject: "Weekly digest", From: "news@zine.io", Host: "zine.io", Date: d(2024, 1, 1)}
	jobs <- core.Job{Subject: "Verify your email", From: "noreply@svc.io", Host: "svc.io", Date: d(2024, 1, 2)}
	close(jobs)

	got := runOneWorker(jobs, testIgnore())
	assert.Len(t, got, 1, "only the classified job survives")
	assert.Equal(t, "verification", got[0].Category)
	assert.Equal(t, "svc.io", got[0].Domain)
}

func TestWorkerFiltersIgnoredDomains(t *testing.T) {
	jobs := make(chan core.Job, 3)
	jobs <- core.Job{Subject: "Welcome to Netflix", From: "info@netflix.com", Host: "netflix.com", Date: d(2024, 1, 1)}
	jobs <- core.Job{Subject: "Welcome to Netflix", From: "info@news.netflix.com", Host: "news.netflix.com", Date: d(2024, 1, 2)} // news. strips -> netflix.com -> ignored
	jobs <- core.Job{Subject: "New login from Firefox", From: "sec@safevault.io", Host: "safevault.io", Date: d(2024, 1, 3)}
	close(jobs)

	got := runOneWorker(jobs, testIgnore("netflix.com"))
	assert.Len(t, got, 1)
	assert.Equal(t, "safevault.io", got[0].Domain)
}

func TestWorkerNormalizesBeforeFilter(t *testing.T) {
	// The ignore check runs on the NORMALIZED domain: a sender behind
	// mail.netflix.com is still filtered.
	// A checagem de ruído roda sobre o domínio NORMALIZADO: um remetente atrás
	// de mail.netflix.com também é filtrado.
	jobs := make(chan core.Job, 1)
	jobs <- core.Job{Subject: "Welcome to Netflix", From: "info@mail.netflix.com", Host: "mail.netflix.com", Date: d(2024, 1, 1)}
	close(jobs)

	assert.Empty(t, runOneWorker(jobs, testIgnore("netflix.com")))
}

func TestDefaultNoiseNeverReachesReport(t *testing.T) {
	// End-to-end-ish micro pipeline: every built-in noise domain must be
	// dropped by the worker stage before the collector sees it.
	// Mini pipeline quase ponta a ponta: todo domínio de ruído embutido deve
	// ser descartado pelo estágio de workers antes da coletora.
	ignore := map[string]bool{}
	for _, dom := range core.DefaultIgnoreDomains {
		ignore[dom] = true
	}

	jobs := make(chan core.Job, len(core.DefaultIgnoreDomains)+1)
	for _, dom := range core.DefaultIgnoreDomains {
		jobs <- core.Job{Subject: "Welcome to " + dom, From: "x@" + dom, Host: dom, Date: d(2024, 1, 1)}
	}
	jobs <- core.Job{Subject: "Verify your email", From: "noreply@keepme.example", Host: "mail.keepme.example",
		Date: d(2024, 1, 2)}
	close(jobs)

	stats := core.Collect(core.RunWorkerPool(4, buildCats(), ignore, jobs))
	assert.Len(t, stats, 1, "noise domains must never reach the report")
	assert.Equal(t, "keepme.example", stats[0].Domain)
}
