package main

// ============================================================================
// REPORT RENDERING / GERAÇÃO DE RELATÓRIOS
// renderText produces the terminal table; writeHTMLReport produces a
// standalone HTML page with client-side search and column sorting. The
// page has zero external assets, so it works offline and can be shared.
//
// renderText produz a tabela no terminal; writeHTMLReport produz uma página
// HTML standalone com busca e ordenação no navegador. A página não tem
// nenhum recurso externo: funciona offline e pode ser compartilhada.
// ============================================================================

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
)

// --- terminal table / tabela no terminal ---

func renderText(w io.Writer, stats []domainStat) {
	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "FIRST SEEN\tDOMAIN\tOCCUR\tCATEGORIES\tSENDERS\t")
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
		fmt.Fprintln(tw, line)
	}
	tw.Flush()

	// Sample subjects come after the table so long lines never break it.
	// Assuntos exemplo vêm após a tabela para não quebrá-la.
	fmt.Fprintln(w, "\nSample subjects:")
	for _, s := range stats {
		fmt.Fprintf(w, "  %s\n    %q\n", s.Domain, s.SampleSubject)
	}
	fmt.Fprintf(w, "\n%d unique domains found.\n", len(stats))
}

// --- HTML report / relatório em HTML ---

//go:embed report.html.tmpl
var reportTemplateFS embed.FS

const htmlReportFile = "e-crawpar-report.html"

// writeHTMLReport renders the navigable report next to the current working
// directory and returns the absolute path for the user.
//
// writeHTMLReport renderiza o relatório navegável na pasta atual e retorna o
// caminho absoluto para o usuário.
func writeHTMLReport(stats []domainStat) (string, error) {
	tmplSrc, err := reportTemplateFS.ReadFile("report.html.tmpl")
	if err != nil {
		return "", newErrf(
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
		Rows             []domainStat
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

	f, err := os.Create(htmlReportFile) // cwd is predictable for users
	if err != nil {
		return "", newErrf(
			"Não consegui criar o arquivo do relatório HTML.",
			"Could not create the HTML report file.",
			fmt.Sprintf("Verifique permissões de escrita na pasta atual (%v) ou rode de outra pasta.", err),
			fmt.Sprintf("Check write permissions in the current folder (%v) or run from another folder.", err))
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return "", fmt.Errorf("html render: %w", err)
	}
	abs, err := filepath.Abs(htmlReportFile)
	if err != nil {
		abs = htmlReportFile
	}
	return abs, nil
}
