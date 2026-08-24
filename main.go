// e-crawpar is an IMAP crawler that scans the headers
// (From, Subject, Date) of your own mailbox to discover which services
// you have accounts with — without ever downloading message bodies.
//
// e-crawpar é um crawler IMAP que varre os headers (From, Subject, Date)
// da sua própria caixa de e-mail para descobrir em quais serviços você tem
// conta — sem nunca baixar o corpo das mensagens.
//
// This file is only pipeline wiring. The stages live in internal packages:
// collection in internal/imapx, classification/aggregation in internal/core,
// output in internal/report and error translation in internal/apperr.
//
// Este arquivo é só a fiação do pipeline. Os estágios vivem nos pacotes
// internos: coleta em internal/imapx, classificação/agregação em
// internal/core, saída em internal/report e tradução de erros em
// internal/apperr.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"e-crawpar/internal/apperr"
	"e-crawpar/internal/core"
	"e-crawpar/internal/imapx"
	"e-crawpar/internal/report"
)

// ============================================================================
// PIPELINE STAGE 0 — CONFIGURATION / ETAPA 0 DO PIPELINE — CONFIGURAÇÃO
// All settings come from environment variables. Credentials are never
// hardcoded and are never printed or logged.
// Todas as configurações vêm de variáveis de ambiente. Credenciais nunca
// são hardcoded nem impressas/logadas.
// ============================================================================

// Config holds every runtime setting loaded from the environment.
// Config carrega todas as configurações de execução do ambiente.
type Config struct {
	Host      string          // IMAP server hostname (IMAP_HOST)
	Port      string          // IMAP TLS port (default 993)
	User      string          // account login (IMAP_USER)
	Password  string          // app password (IMAP_APP_PASSWORD)
	Mailbox   string          // mailbox to scan (IMAP_MAILBOX, default INBOX)
	Since     *time.Time      // optional lower bound for search (IMAP_SINCE, RFC3339)
	Workers   int             // classification worker pool size (WORKERS)
	BatchSize int             // messages per ENVELOPE fetch batch (BATCH_SIZE)
	Ignore    map[string]bool // normalized domains to skip (built-in + IGNORE_DOMAINS)
}

// loadConfig builds the runtime configuration from a merged view of the
// .env file and OS environment (OS wins), validating what it can.
//
// loadConfig monta a configuração a partir de uma visão mesclada do arquivo
// .env e do ambiente do SO (o SO vence), validando o que pode.
func loadConfig(file map[string]string) (*Config, error) {
	// lookup resolves each key: real environment first, then the .env file.
	// lookup resolve cada chave: ambiente primeiro, depois o arquivo .env.
	lookup := func(key string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return file[key]
	}

	cfg := &Config{
		Host:     lookup("IMAP_HOST"),
		Port:     lookupOr(lookup, "IMAP_PORT", "993"),
		User:     lookup("IMAP_USER"),
		Password: lookup("IMAP_APP_PASSWORD"),
		Mailbox:  lookupOr(lookup, "IMAP_MAILBOX", "INBOX"),
	}

	// Mandatory variables: fail fast with a clear message.
	// Variáveis obrigatórias: falha rápida com mensagem clara.
	var missing []string
	for _, k := range []string{"IMAP_HOST", "IMAP_USER", "IMAP_APP_PASSWORD"} {
		var val string
		switch k {
		case "IMAP_HOST":
			val = cfg.Host
		case "IMAP_USER":
			val = cfg.User
		case "IMAP_APP_PASSWORD":
			val = cfg.Password
		}
		if val == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return nil, &missingEnvError{Keys: missing}
	}

	var err error
	if cfg.Workers, err = lookupInt(lookup, "WORKERS", 8); err != nil {
		return nil, err
	}
	if cfg.BatchSize, err = lookupInt(lookup, "BATCH_SIZE", 200); err != nil {
		return nil, err
	}
	if cfg.Workers < 1 || cfg.BatchSize < 1 {
		return nil, fmt.Errorf("WORKERS and BATCH_SIZE must be >= 1")
	}

	// Optional date filter (RFC3339, e.g. 2024-01-01T00:00:00Z).
	// Filtro opcional por data (RFC3339, ex.: 2024-01-01T00:00:00Z).
	if raw := lookup("IMAP_SINCE"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil, fmt.Errorf("invalid IMAP_SINCE (want RFC3339): %w", err)
		}
		cfg.Since = &t
	}

	// Noise filter: built-in defaults extended by IGNORE_DOMAINS.
	// Filtro de ruído: defaults embutidos estendidos por IGNORE_DOMAINS.
	cfg.Ignore = map[string]bool{}
	for _, d := range core.DefaultIgnoreDomains {
		cfg.Ignore[d] = true
	}
	if extra := lookup("IGNORE_DOMAINS"); extra != "" {
		for _, d := range strings.Split(extra, ",") {
			if d = strings.ToLower(strings.TrimSpace(d)); d != "" {
				cfg.Ignore[d] = true
			}
		}
	}

	return cfg, nil
}

func lookupOr(lookup func(string) string, key, def string) string {
	if v := lookup(key); v != "" {
		return v
	}
	return def
}

func lookupInt(lookup func(string) string, key string, def int) (int, error) {
	raw := lookup(key)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s (want number): %w", key, err)
	}
	return n, nil
}

func main() {
	jsonOut := flag.Bool("json", false, "also print the report as JSON")
	htmlOut := flag.Bool("html", false, "also write a navigable HTML report (e-crawpar-report.html)")
	flag.Parse()

	// Stage 0: bootstrap configuration — .env + OS env, or the interactive
	// wizard on first run (which validates and saves credentials itself).
	// Etapa 0: bootstrap da configuração — .env + ambiente do SO, ou o
	// assistente interativo na primeira execução (que valida e salva as
	// credenciais sozinho).
	cfg, err := bootstrap()
	if err != nil {
		apperr.PrintFriendly(err)
		os.Exit(1)
	}
	categories := core.BuildCategories()

	// Stage 1: producer goroutine streams header jobs from IMAP.
	// Etapa 1: goroutine produtora transmite jobs de headers do IMAP.
	jobs := make(chan core.Job, cfg.BatchSize)
	errCh := make(chan error, 1)
	go func() {
		defer close(jobs)
		client, dialErr := imapx.DialTLS(cfg.Host, cfg.Port)
		if dialErr != nil {
			errCh <- apperr.FriendlyDialError(dialErr, cfg.Host+":"+cfg.Port) // translated for non-technical users
			return
		}
		errCh <- imapx.CollectHeaders(client, imapx.Options{
			User:      cfg.User,
			Password:  cfg.Password,
			Mailbox:   cfg.Mailbox,
			Since:     cfg.Since,
			BatchSize: cfg.BatchSize,
		}, jobs)
	}()

	// Stage 2: worker pool classifies/normalizes/filters concurrently.
	// Etapa 2: pool de workers classifica/normaliza/filtra em paralelo.
	results := core.RunWorkerPool(cfg.Workers, categories, cfg.Ignore, jobs)

	// Stage 3+4: single collector aggregates, then the report renders.
	// Etapas 3+4: coletora única agrega, depois o relatório é impresso.
	stats := core.Collect(results)

	// Collection errors only surface after the drain: any successfully
	// fetched batches were already processed, so we still show them.
	// (errCh always receives exactly one value — success or failure.)
	// Erros de coleta aparecem só após o dreno: lotes já buscados foram
	// processados, então ainda são exibidos.
	// (errCh sempre recebe exatamente um valor — sucesso ou falha.)
	if err := <-errCh; err != nil {
		apperr.PrintFriendly(err) // bilingual, no stack traces / bilíngue, sem stack trace
		os.Exit(1)
	}

	report.RenderText(os.Stdout, stats)
	if *jsonOut {
		out, err := json.MarshalIndent(stats, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "json error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(out))
	}
	if *htmlOut {
		path, err := report.WriteHTMLReport(stats)
		if err != nil {
			apperr.PrintFriendly(err)
			os.Exit(1)
		}
		fmt.Printf("\nHTML report: %s\n  PT Abra este arquivo no navegador para uma tabela com busca e ordenação.\n  EN Open this file in your browser for a searchable, sortable table.\n", path)
	}
}
