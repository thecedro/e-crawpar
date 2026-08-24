// Layer 2 — INTEGRATION TESTS: report generation from simulated aggregates.
// The terminal table and the standalone HTML page must render the collector's
// output faithfully: ordering, multi-sender alerts, counters and HTML
// escaping of hostile subjects.
//
// Camada 2 — TESTES DE INTEGRAÇÃO: geração de relatório a partir de agregados
// simulados. A tabela do terminal e a página HTML standalone devem refletir
// fielmente a saída da coletora: ordenação, alertas de multi-remetentes,
// contadores e escape HTML de assuntos hostis.
package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"e-crawpar/internal/core"
	"e-crawpar/internal/report"
)

func sampleStats() []core.DomainStat {
	// Pre-sorted exactly as core.Collect returns them (dated ascending,
	// undated last), mirroring what production feeds the renderer.
	// Pré-ordenado exatamente como core.Collect retorna (datas crescentes,
	// sem data por último), espelhando o que a produção passa ao renderizador.
	return []core.DomainStat{
		{Domain: "svc.io", FirstSeen: "2020-05-09", Categories: []string{"verification", "welcome", "receipt"},
			Occurrences: 3, SampleSubject: `Verify <img src=x onerror=alert(1)>`,
			DistinctSenders: 3, MultiSender: true},
		{Domain: "oldblog.net", FirstSeen: "2021-07-15", Categories: []string{"policy"},
			Occurrences: 1, SampleSubject: "Termos de uso atualizados",
			DistinctSenders: 1, MultiSender: false},
		{Domain: "ghost.example", FirstSeen: "", Categories: []string{"security"},
			Occurrences: 1, SampleSubject: "New login from Tor",
			DistinctSenders: 1, MultiSender: false},
	}
}

func TestIntegrationTextReport(t *testing.T) {
	stats := sampleStats()
	var b strings.Builder
	report.RenderText(&b, stats)
	out := b.String()

	for _, want := range []string{
		"2020-05-09", "svc.io", // dated rows first / datados primeiro
		"MULTIPLE SENDERS",
		"?", "ghost.example", // unknown date renders as "?" / data ausente vira "?"
		"verification, welcome, receipt",
		"3 unique domains found.",
	} {
		assert.Contains(t, out, want)
	}
	// Table order follows the collector's ordering / ordem da tabela segue a coletora
	assert.Less(t, strings.Index(out, "svc.io"), strings.Index(out, "oldblog.net"))
	assert.Less(t, strings.Index(out, "oldblog.net"), strings.Index(out, "ghost.example"))
}

func TestIntegrationHTMLReportIntoTempDir(t *testing.T) {
	dir := t.TempDir()
	path, err := report.WriteHTMLReportTo(dir, sampleStats())
	require.NoError(t, err)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	html := string(raw)

	for _, want := range []string{
		"oldblog.net", "svc.io", "ghost.example",
		"2021-07-15", "2020-05-09",
		"múltiplos",                          // multi-sender badge / selo de múltiplos remetentes
		"&lt;img src=x onerror=alert(1)&gt;", // hostile subject escaped / assunto hostil escapado
		"3</b>",                              // total domains counter / contador de domínios
		"5</b>",                              // total occurrences counter / contador de ocorrências
	} {
		assert.Contains(t, html, want)
	}
	assert.NotContains(t, html, "<img src=x onerror=alert(1)>", "raw payload must never appear")

	// Written exactly where asked, not in CWD / gravado exatamente onde pedido
	assert.Equal(t, filepath.Join(dir, "e-crawpar-report.html"), path)
	_, err = os.Stat("e-crawpar-report.html")
	assert.True(t, os.IsNotExist(err), "CWD must stay clean / diretório corrente intocado")
}

func TestIntegrationEmptyReportRenders(t *testing.T) {
	var b strings.Builder
	report.RenderText(&b, nil)
	assert.Contains(t, b.String(), "0 unique domains found.")

	dir := t.TempDir()
	path, err := report.WriteHTMLReportTo(dir, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, path)
}
