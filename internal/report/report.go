// Package report renders the final e-crawpar output: a terminal table and a
// standalone navigable HTML page with client-side search and column sorting.
// The page has zero external assets, so it works offline and can be shared.
//
// O pacote report gera a saída final do e-crawpar: uma tabela no terminal e
// uma página HTML standalone navegável, com busca e ordenação no navegador.
// A página não tem nenhum recurso externo: funciona offline e pode ser
// compartilhada.
package report

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"e-crawpar/internal/apperr"
	"e-crawpar/internal/core"
)

// --- terminal table / tabela no terminal ---

// RenderText writes the terminal table to w. Sample subjects come after the
// table so long lines never break it.
//
// RenderText escreve a tabela do terminal em w. Assuntos exemplo vêm após a
// tabela para não quebrá-la.
func RenderText(w io.Writer, stats []core.DomainStat) {
	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "FIRST SEEN\tDOMAIN\tOCCUR\tCATEGORIES\tSENDERS\t")
	for _, s := range stats {
		first := s.FirstSeen
		if first == "" {
			first = "?"
		}
		line := fmt.Sprintf("%s\t%s\t%d\t%s\t%d\t",
			first, s.Domain, s.Occurrences,
			strings.Join(s.Categories, ", "), s.DistinctSenders)
		if s.MultiSender {
			line += "  << MULTIPLE SENDERS"
		}
		_, _ = fmt.Fprintln(tw, line)
	}
	_ = tw.Flush()

	_, _ = fmt.Fprintln(w, "\nSample subjects:")
	for _, s := range stats {
		_, _ = fmt.Fprintf(w, "  %s\n    %q\n", s.Domain, s.SampleSubject)
	}
	_, _ = fmt.Fprintf(w, "\n%d unique domains found.\n", len(stats))
}

// --- HTML report / relatório em HTML ---

//go:embed report.html.tmpl
var reportTemplateFS embed.FS

const htmlReportFile = "e-crawpar-report.html"

// WriteHTMLReport renders the navigable report next to the current working
// directory and returns the absolute path for the user.
//
// WriteHTMLReport renderiza o relatório navegável na pasta atual e retorna o
// caminho absoluto para o usuário.
func WriteHTMLReport(stats []core.DomainStat) (string, error) {
	return WriteHTMLReportTo("", stats)
}

// WriteHTMLReportTo renders the navigable report into dir ("" means the
// current working directory) and returns the absolute file path. The dir
// parameter exists so tests can write to t.TempDir() instead of the CWD.
//
// WriteHTMLReportTo renderiza o relatório navegável em dir ("" significa a
// pasta atual) e retorna o caminho absoluto. O parâmetro dir existe para que
// testes gravem em t.TempDir() em vez do diretório corrente.
func WriteHTMLReportTo(dir string, stats []core.DomainStat) (string, error) {
	tmplSrc, err := reportTemplateFS.ReadFile("report.html.tmpl")
	if err != nil {
		return "", apperr.NewErrf(
			"Arquivo interno do programa faltando.",
			"Internal program file missing.",
			"Reinstale o binário a partir de uma release oficial.",
			"Reinstall the binary from an official release.")
	}

	totalOccurrences := 0
	for _, s := range stats {
		totalOccurrences += s.Occurrences
	}
	data := struct {
		GeneratedAt      string
		TotalDomains     int
		TotalOccurrences int
		Rows             []core.DomainStat
	}{
		GeneratedAt:      time.Now().Format("2006-01-02 15:04"),
		TotalDomains:     len(stats),
		TotalOccurrences: totalOccurrences,
		Rows:             stats,
	}

	tmpl, err := template.New("report").Funcs(template.FuncMap{
		"join": strings.Join,
	}).Parse(string(tmplSrc))
	if err != nil {
		return "", fmt.Errorf("template parse: %w", err)
	}

	path := filepath.Join(dir, htmlReportFile)
	f, err := os.Create(path)
	if err != nil {
		return "", apperr.NewErrf(
			"Não consegui criar o arquivo do relatório HTML.",
			"Could not create the HTML report file.",
			fmt.Sprintf("Verifique permissões de escrita na pasta atual (%v) ou rode de outra pasta.", err),
			fmt.Sprintf("Check write permissions in the current folder (%v) or run from another folder.", err))
	}
	defer func() { _ = f.Close() }()

	if err := tmpl.Execute(f, data); err != nil {
		return "", fmt.Errorf("html render: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return abs, nil
}
