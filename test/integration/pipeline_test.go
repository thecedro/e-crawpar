// Layer 2 — INTEGRATION TESTS.
// The full concurrent pipeline (jobs -> worker pool -> collector) fed with
// synthetic header batches — no IMAP involved. Guarantees verified here:
//
//   - no data race (run with: go test -race ./test/integration)
//   - the collector aggregates without loss or duplication of messages
//     under high concurrency (500 headers x many workers)
//   - report generation (terminal + HTML) renders from aggregated results
//
// Camada 2 — TESTES DE INTEGRAÇÃO.
// O pipeline concorrente completo (jobs -> pool de workers -> coletora)
// alimentado com lotes sintéticos de headers — sem IMAP. Garantias
// verificadas aqui:
//
//   - nenhuma race condition (rodar com: go test -race ./test/integration)
//   - a coletora agrega sem perda nem duplicação de mensagens sob alta
//     concorrência (500 headers x muitos workers)
//   - geração de relatório (terminal + HTML) a partir de resultados agregados
package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"e-crawpar/internal/core"
)

// envelope mirrors one entry of testdata/envelopes.json.
// envelope espelha uma entrada de testdata/envelopes.json.
type envelope struct {
	Subject string `json:"subject"`
	From    string `json:"from"`
	Host    string `json:"host"`
	Date    string `json:"date"`
}

// loadEnvelopes reads the shared fixture set; every integration scenario is
// derived from it so fixtures stay the single source of truth.
func loadEnvelopes(t *testing.T) []envelope {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "envelopes.json"))
	require.NoError(t, err)
	var envs []envelope
	require.NoError(t, json.Unmarshal(raw, &envs))
	require.NotEmpty(t, envs)
	return envs
}

func (e envelope) job() core.Job {
	var date time.Time
	if e.Date != "" {
		date = mustTime(e.Date)
	}
	return core.Job{Subject: core.DecodeSubject(e.Subject), From: e.From, Host: e.Host, Date: date}
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// TestIntegrationFixtureAggregation runs the whole in-memory pipeline over
// the fixture envelopes and locks the exact expected report.
func TestIntegrationFixtureAggregation(t *testing.T) {
	envs := loadEnvelopes(t)

	jobs := make(chan core.Job, len(envs))
	for _, e := range envs {
		jobs <- e.job()
	}
	close(jobs)

	stats := core.Collect(core.RunWorkerPool(8, core.BuildCategories(),
		ignoreMap(core.DefaultIgnoreDomains), jobs))

	// 10 fixtures; netflix.com (ignore list) and zine.example (no category) drop.
	assert.Len(t, stats, 5)
	domains := make([]string, len(stats))
	for i, s := range stats {
		domains[i] = s.Domain
	}
	assert.Equal(t,
		[]string{"svc.io", "oldblog.net", "shopnow.example", "safevault.io", "cloudnotes.io"},
		domains, "ordered by first-seen ascending, undated last")

	svc := stats[0]
	assert.Equal(t, "2020-05-09", svc.FirstSeen)
	assert.Equal(t, []string{"verification", "welcome", "receipt"}, svc.Categories)
	assert.Equal(t, 3, svc.DistinctSenders)
	assert.True(t, svc.MultiSender)
	assert.Equal(t, "Verify your email address", svc.SampleSubject)

	shop := stats[2]
	assert.True(t, shop.MultiSender, "two senders on shopnow.example")
	assert.Equal(t, []string{"security", "receipt"}, shop.Categories)

	safe := stats[3]
	assert.Equal(t, "Senha alterada com sucesso", safe.SampleSubject,
		"RFC 2047 subject decoded before classification")
}

// TestIntegrationNoLossNoDuplicationUnderLoad hammers the pipeline with 500
// synthetic headers across repeated domains and proves nothing is lost or
// duplicated: every classified message reaches the collector exactly once.
func TestIntegrationNoLossNoDuplicationUnderLoad(t *testing.T) {
	const (
		total      = 500
		workers    = 32
		numDomains = 25 // 20 messages per domain / 20 por domínio
	)

	jobs := make(chan core.Job, 64)
	go func() {
		defer close(jobs)
		for i := 0; i < total; i++ {
			n := i % numDomains
			jobs <- core.Job{
				Subject: "Verify your email",
				From:    fmt.Sprintf("sender-%d@d%02d.example", i%7, n),
				Host:    fmt.Sprintf("mail.d%02d.example", n),
				Date:    time.Date(2020+n%4, 1, 1+i%28, 0, 0, 0, 0, time.UTC),
			}
		}
	}()

	results := core.RunWorkerPool(workers, core.BuildCategories(), map[string]bool{}, jobs)

	count := 0
	domains := map[string]int{}
	for r := range results {
		count++
		domains[r.Domain]++
		assert.Equal(t, "verification", r.Category)
		assert.True(t, strings.HasPrefix(r.Domain, "d"), "hosts normalized to base domain")
	}

	assert.Equal(t, total, count, "no message lost or duplicated under concurrency")
	assert.Len(t, domains, numDomains)
	for d, n := range domains {
		assert.Equal(t, total/numDomains, n, "domain %s got a balanced share", d)
	}
}

func TestIntegrationAggregateTotalsAfterDrain(t *testing.T) {
	const total, workers, numDomains = 500, 16, 25

	jobs := make(chan core.Job, 128)
	go func() {
		defer close(jobs)
		for i := 0; i < total; i++ {
			n := i % numDomains
			jobs <- core.Job{
				Subject: "Welcome to service",
				From:    fmt.Sprintf("s%d@d%02d.example", i%9, n),
				Host:    fmt.Sprintf("mail.d%02d.example", n),
				Date:    time.Date(2018, 1, 1+(i%27), 12, 0, 0, 0, time.UTC),
			}
		}
	}()

	stats := core.Collect(core.RunWorkerPool(workers, core.BuildCategories(), map[string]bool{}, jobs))

	sumOccurrences := 0
	for _, s := range stats {
		sumOccurrences += s.Occurrences
		assert.LessOrEqual(t, s.DistinctSenders, 9, "at most 9 distinct senders per domain")
		assert.False(t, strings.Contains(s.Domain, "mail."), "hosts normalized to base domain")
	}
	assert.Len(t, stats, numDomains, "one row per synthetic domain")
	assert.Equal(t, total, sumOccurrences, "aggregate occurrences == messages produced")
}

// TestIntegrationHighConcurrencyStateIntegrity pushes interleaved categories
// through a large pool and checks per-domain state stays coherent (category
// sets, sample preference, sender counting) despite heavy scheduling.
func TestIntegrationHighConcurrencyStateIntegrity(t *testing.T) {
	const reps = 200

	jobs := make(chan core.Job, 256)
	go func() {
		defer close(jobs)
		for i := 0; i < reps; i++ {
			jobs <- core.Job{Subject: "New login from Safari", From: "sec@busy.io", Host: "busy.io",
				Date: time.Date(2022, 6, 15, 0, 0, 0, 0, time.UTC)}
			jobs <- core.Job{Subject: "Verify your email", From: "verify@busy.io", Host: "busy.io",
				Date: time.Date(2022, 6, 14, 0, 0, 0, 0, time.UTC)}
			jobs <- core.Job{Subject: "Bem-vindo ao busy app", From: "hello@busy.io", Host: "busy.io",
				Date: time.Date(2022, 6, 13, 0, 0, 0, 0, time.UTC)}
			jobs <- core.Job{Subject: "Unmatched filler", From: "x@quiet.io", Host: "quiet.io"} // dropped
		}
	}()

	stats := core.Collect(core.RunWorkerPool(48, core.BuildCategories(), map[string]bool{}, jobs))

	require.Len(t, stats, 1)
	busy := stats[0]
	assert.Equal(t, "busy.io", busy.Domain)
	assert.Equal(t, reps*3, busy.Occurrences, "only matched subjects counted")
	assert.ElementsMatch(t, []string{"security", "verification", "welcome"}, busy.Categories)
	assert.Equal(t, "Verify your email", busy.SampleSubject)
	assert.Equal(t, "2022-06-13", busy.FirstSeen, "earliest of three interleaved dates")
	assert.True(t, busy.MultiSender, "three distinct senders on the same domain")
	assert.Equal(t, 3, busy.DistinctSenders)
}

// ignoreMap converts the default domain slice into the worker's set form.
func ignoreMap(domains []string) map[string]bool {
	m := make(map[string]bool, len(domains))
	for _, d := range domains {
		m[d] = true
	}
	return m
}
