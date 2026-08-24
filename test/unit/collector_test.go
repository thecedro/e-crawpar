// Layer 1 — UNIT TESTS: the collector's aggregation rules — earliest-date
// tracking ("account birthday"), curated sample preference, distinct-sender
// counting, multi-sender alert, category display order and the
// dated-first/undated-last report ordering.
//
// Camada 1 — TESTES DE UNIDADE: regras de agregação da coletora — menor data
// ("aniversário da conta"), preferência de amostra curada, contagem de
// remetentes distintos, alerta de multi-remetentes, ordem de exibição das
// categorias e ordenação datado-primeiro/indefinido-por-último.
package unit_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"e-crawpar/internal/core"
)

func feed(results chan core.Result, rs ...core.Result) []core.DomainStat {
	for _, r := range rs {
		results <- r
	}
	close(results)
	return core.Collect(results)
}

func TestCollectAggregationAndOrdering(t *testing.T) {
	stats := feed(make(chan core.Result, 8),
		core.Result{Domain: "svc.io", Sender: "no-reply@svc.io", Category: "welcome",
			Subject: "Welcome to svc", Date: d(2020, 5, 10)},
		core.Result{Domain: "svc.io", Sender: "billing@svc.io", Category: "receipt",
			Subject: "Your invoice", Date: d(2021, 1, 1)},
		core.Result{Domain: "svc.io", Sender: "verify@svc.io", Category: "verification",
			Subject: "Verify your email", Date: d(2020, 5, 9)}, // earlier than welcome! / anterior às boas-vindas!
		core.Result{Domain: "old.example", Sender: "a@old.example", Category: "policy",
			Subject: "Terms updated"}, // zero date -> sorts last / sem data -> vai por último
	)

	assert.Len(t, stats, 2)
	first, second := stats[0], stats[1]

	assert.Equal(t, "svc.io", first.Domain)
	assert.Equal(t, "2020-05-09", first.FirstSeen, "earliest envelope date wins")
	assert.Equal(t, "Verify your email", first.SampleSubject, "sample prefers verification")
	assert.Equal(t, 3, first.Occurrences)
	assert.Equal(t, 3, first.DistinctSenders)
	assert.True(t, first.MultiSender)
	assert.Equal(t, []string{"verification", "welcome", "receipt"}, first.Categories,
		"categories in fixed priority order")

	assert.Equal(t, "old.example", second.Domain)
	assert.Empty(t, second.FirstSeen, "undated row has no first_seen")
	assert.False(t, second.MultiSender)
}

func TestCollectDatedRowsSortBeforeUndated(t *testing.T) {
	stats := feed(make(chan core.Result, 4),
		core.Result{Domain: "nodate.example", Sender: "a@x", Category: "security", Subject: "s"},
		core.Result{Domain: "z.example", Sender: "b@x", Category: "security", Subject: "s", Date: d(2024, 6, 1)},
		core.Result{Domain: "a.example", Sender: "c@x", Category: "security", Subject: "s", Date: d(2024, 1, 1)},
	)
	assert.Equal(t, []string{"a.example", "z.example", "nodate.example"},
		[]string{stats[0].Domain, stats[1].Domain, stats[2].Domain},
		"dated ascending, undated last")
}

func TestCollectSampleFallbackWhenOnlyUncurated(t *testing.T) {
	// security/receipt/policy are not curated samples; the collector still
	// keeps the FIRST subject seen as fallback.
	// segurança/recibo/política não são amostras curadas; a coletora mantém o
	// PRIMEIRO assunto visto como fallback.
	stats := feed(make(chan core.Result, 4),
		core.Result{Domain: "x.io", Sender: "a@x.io", Category: "policy", Subject: "first policy mail"},
		core.Result{Domain: "x.io", Sender: "a@x.io", Category: "receipt", Subject: "second receipt mail"},
	)
	assert.Equal(t, "first policy mail", stats[0].SampleSubject,
		"uncurated subjects fall back to first seen")
}

func TestCollectVerificationBeatsWelcomeAsSample(t *testing.T) {
	stats := feed(make(chan core.Result, 4),
		core.Result{Domain: "y.io", Sender: "a@y.io", Category: "welcome", Subject: "Welcome!", Date: d(2020, 1, 1)},
		core.Result{Domain: "y.io", Sender: "b@y.io", Category: "verification", Subject: "Verify now", Date: d(2020, 2, 1)},
		core.Result{Domain: "y.io", Sender: "c@y.io", Category: "welcome", Subject: "Another welcome", Date: d(2020, 3, 1)},
	)
	assert.Equal(t, "Verify now", stats[0].SampleSubject)
	assert.Equal(t, "2020-01-01", stats[0].FirstSeen, "birthday is independent of samples")
}

func TestCollectFirstVerificationSampleWinsOnTie(t *testing.T) {
	// Two verification subjects: the FIRST must stay the sample. This kills
	// the boundary mutant rank <= a.sampleRank (which would keep the last).
	// Dois assuntos de verificação: o PRIMEIRO deve permanecer como amostra.
	// Isso mata o mutante de borda rank <= a.sampleRank (que manteria o último).
	stats := feed(make(chan core.Result, 4),
		core.Result{Domain: "z.io", Sender: "a@z.io", Category: "verification", Subject: "first verify", Date: d(2020, 1, 1)},
		core.Result{Domain: "z.io", Sender: "b@z.io", Category: "verification", Subject: "second verify", Date: d(2020, 2, 2)},
	)
	assert.Equal(t, "first verify", stats[0].SampleSubject)
	assert.Equal(t, "2020-01-01", stats[0].FirstSeen)
}

func TestCollectEmptyInput(t *testing.T) {
	stats := core.Collect(func() <-chan core.Result {
		ch := make(chan core.Result)
		close(ch)
		return ch
	}())
	assert.Empty(t, stats)
}

func TestCollectZeroDateNeverBecomesBirthday(t *testing.T) {
	// A zero-date result must not erase an existing birthday nor create one.
	// Um resultado sem data não pode apagar nem criar aniversário.
	stats := feed(make(chan core.Result, 4),
		core.Result{Domain: "d.io", Sender: "a@d.io", Category: "security", Subject: "with date", Date: d(2019, 9, 9)},
		core.Result{Domain: "d.io", Sender: "a@d.io", Category: "receipt", Subject: "no date here"},
	)
	assert.Equal(t, "2019-09-09", stats[0].FirstSeen)
}
