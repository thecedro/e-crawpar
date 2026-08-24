//go:build e2e

// Layer 4 — END-TO-END TESTS (isolated by the `e2e` build tag; run with
// `go test -tags e2e ./test/e2e`). The production collection path
// (ClientAdapter) runs against a full in-memory IMAP server over real TCP,
// seeded from the same .eml fixtures that document the business scenarios.
// The complete binary pipeline is exercised: dial -> login -> EXAMINE ->
// batched ENVELOPE fetch -> worker pool -> collector -> rendered reports.
//
// What this layer guarantees / O que esta camada garante:
//   - the final report lists exactly the expected domains, in first-seen
//     order, with correct categories and multi-sender alerts;
//   - noise (ignored domains, unmatched subjects) never reaches the report;
//   - RFC 2047 subjects are decoded on the way;
//   - the mailbox is left untouched: every message keeps its original flags
//     (\Seen) and no body was ever requested.
//
// Camada 4 — TESTES PONTA A PONTO (isolados pela build tag `e2e`; rodar com
// `go test -tags e2e ./test/e2e`). O caminho de coleta de produção
// (ClientAdapter) roda contra um servidor IMAP em memória completo via TCP
// real, populado a partir dos mesmos fixtures .eml que documentam os cenários
// de negócio. O pipeline inteiro do binário é exercitado: conexão -> login ->
// EXAMINE -> fetch de ENVELOPE em lotes -> pool de workers -> coletora ->
// relatórios renderizados.

package e2e_test

import (
	"os"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"e-crawpar/internal/core"
	"e-crawpar/internal/report"
	"e-crawpar/test/harness"
)

func setupInbox(t *testing.T) string {
	t.Helper()
	msgs := harness.LoadFixtureDir(t, harness.FixtureDir())
	return harness.StartServer(t, msgs)
}

func runFullPipeline(t *testing.T, addr string) []core.DomainStat {
	t.Helper()
	client := harness.Dial(t, addr)
	opt := harness.Options(addr, "INBOX", 3) // tiny batches to force pagination / lotes mínimos
	return harness.RunPipeline(t, client, opt, 8, ignoreFromDefaults())
}

func ignoreFromDefaults() map[string]bool {
	m := make(map[string]bool, len(core.DefaultIgnoreDomains))
	for _, d := range core.DefaultIgnoreDomains {
		m[d] = true
	}
	return m
}

func rowByDomain(stats []core.DomainStat, domain string) (core.DomainStat, bool) {
	for _, s := range stats {
		if s.Domain == domain {
			return s, true
		}
	}
	return core.DomainStat{}, false
}

func TestE2EFullReportAgainstFakeServer(t *testing.T) {
	addr := setupInbox(t)
	stats := runFullPipeline(t, addr)

	require.Len(t, stats, 5, "exactly five domains survive the noise filters")

	// Ordering: dated rows ascending by first occurrence ("account
	// birthday"), undated last / Ordenação: datadas por primeira ocorrência,
	// sem data por último.
	domains := make([]string, len(stats))
	for i, s := range stats {
		domains[i] = s.Domain
	}
	assert.Equal(t,
		[]string{"musicsvc.com", "oldblog.net", "shopnow.com", "safevault.com", "cloudnotes.io"},
		domains)

	// musicsvc: verification (2018-03-09) beats welcome (2018-03-10) as
	// birthday AND as sample; two distinct senders => alert / alerta.
	music := stats[0]
	assert.Equal(t, "2018-03-09", music.FirstSeen)
	assert.Equal(t, []string{"verification", "welcome"}, music.Categories)
	assert.Equal(t, "Verify your email address", music.SampleSubject)
	assert.Equal(t, 2, music.DistinctSenders)
	assert.True(t, music.MultiSender)

	// oldblog: the policy-update mail reveals an account older than every
	// other evidence / o e-mail de política revela conta mais antiga que as demais.
	old := stats[1]
	assert.Equal(t, "2021-07-15", old.FirstSeen)
	assert.Equal(t, []string{"policy"}, old.Categories)
	assert.False(t, old.MultiSender)

	shop := stats[2]
	assert.Equal(t, "2023-11-05", shop.FirstSeen)
	assert.Equal(t, []string{"security", "receipt"}, shop.Categories)
	assert.True(t, shop.MultiSender)

	safe := stats[3]
	assert.Equal(t, "Senha alterada com sucesso", safe.SampleSubject,
		"encoded RFC 2047 subject arrives decoded")

	cloud := stats[4]
	assert.Empty(t, cloud.FirstSeen, "message without Date header sorts last")
	assert.Equal(t, []string{"welcome"}, cloud.Categories)

	// Noise must be absent / ruído deve estar ausente
	for _, noise := range []string{"netflix.com", "zine.example"} {
		_, ok := rowByDomain(stats, noise)
		assert.Falsef(t, ok, "noise domain %q leaked into the report", noise)
	}
}

func TestE2ETextAndHTMLReportsFromRealPath(t *testing.T) {
	addr := setupInbox(t)
	stats := runFullPipeline(t, addr)

	var b strings.Builder
	report.RenderText(&b, stats)
	out := b.String()
	for _, want := range []string{
		"musicsvc.com", "oldblog.net", "shopnow.com",
		"MULTIPLE SENDERS", "5 unique domains found.",
	} {
		assert.Contains(t, out, want)
	}
	for _, noise := range []string{"netflix.com", "zine.example"} {
		assert.NotContainsf(t, out, noise, "noise domain %q rendered", noise)
	}

	path, err := report.WriteHTMLReportTo(t.TempDir(), stats)
	require.NoError(t, err)
	html, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(html), "musicsvc.com")
	assert.NotContains(t, string(html), "netflix.com")
}

func TestE2EMailboxLeftUntouched(t *testing.T) {
	addr := setupInbox(t)
	_ = runFullPipeline(t, addr)

	flags := harness.FetchAllFlags(t, addr)
	msgs := harness.LoadFixtureDir(t, harness.FixtureDir())
	require.Len(t, flags, len(msgs))
	for i, f := range flags {
		assert.Equalf(t, []imap.Flag{imap.FlagSeen}, f,
			"message %d was touched during the crawl", i+1)
	}
}
