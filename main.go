// Package main implements an IMAP crawler that scans the headers
// (From, Subject, Date) of your own mailbox to discover which services
// you have accounts with — without ever downloading message bodies.
//
// O pacote main implementa um crawler IMAP que varre os headers
// (From, Subject, Date) da sua própria caixa de e-mail para descobrir
// em quais serviços você tem conta — sem nunca baixar o corpo das mensagens.
package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
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
	Host        string        // IMAP server hostname (IMAP_HOST)
	Port        string        // IMAP TLS port (default 993)
	User        string        // account login (IMAP_USER)
	Password    string        // app password (IMAP_APP_PASSWORD)
	Mailbox     string        // mailbox to scan (IMAP_MAILBOX, default INBOX)
	Since       *time.Time    // optional lower bound for search (IMAP_SINCE, RFC3339)
	Workers     int           // classification worker pool size (WORKERS)
	BatchSize   int           // messages per ENVELOPE fetch batch (BATCH_SIZE)
	Ignore      map[string]bool // normalized domains to skip (built-in + IGNORE_DOMAINS)
}

// loadConfig reads and validates all environment variables.
// loadConfig lê e valida todas as variáveis de ambiente.
func loadConfig() (*Config, error) {
	cfg := &Config{
		Host:     os.Getenv("IMAP_HOST"),
		Port:     envOr("IMAP_PORT", "993"),
		User:     os.Getenv("IMAP_USER"),
		Password: os.Getenv("IMAP_APP_PASSWORD"),
		Mailbox:  envOr("IMAP_MAILBOX", "INBOX"),
	}

	// Mandatory variables: fail fast with a clear message.
	// Variáveis obrigatórias: falha rápida com mensagem clara.
	for name, val := range map[string]string{
		"IMAP_HOST":         cfg.Host,
		"IMAP_USER":         cfg.User,
		"IMAP_APP_PASSWORD": cfg.Password,
	} {
		if val == "" {
			return nil, fmt.Errorf("missing required env var %s", name)
		}
	}

	var err error
	if cfg.Workers, err = envInt("WORKERS", 8); err != nil {
		return nil, err
	}
	if cfg.BatchSize, err = envInt("BATCH_SIZE", 200); err != nil {
		return nil, err
	}
	if cfg.Workers < 1 || cfg.BatchSize < 1 {
		return nil, fmt.Errorf("WORKERS and BATCH_SIZE must be >= 1")
	}

	// Optional date filter (RFC3339, e.g. 2024-01-01T00:00:00Z).
	// Filtro opcional por data (RFC3339, ex.: 2024-01-01T00:00:00Z).
	if raw := os.Getenv("IMAP_SINCE"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil, fmt.Errorf("invalid IMAP_SINCE (want RFC3339): %w", err)
		}
		cfg.Since = &t
	}

	// Noise filter: built-in defaults extended by IGNORE_DOMAINS.
	// Filtro de ruído: defaults embutidos estendidos por IGNORE_DOMAINS.
	cfg.Ignore = map[string]bool{}
	for _, d := range defaultIgnoreDomains {
		cfg.Ignore[d] = true
	}
	if extra := os.Getenv("IGNORE_DOMAINS"); extra != "" {
		for _, d := range strings.Split(extra, ",") {
			if d = strings.ToLower(strings.TrimSpace(d)); d != "" {
				cfg.Ignore[d] = true
			}
		}
	}

	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return n, nil
}

// ============================================================================
// CLASSIFICATION PATTERNS / PADRÕES DE CLASSIFICAÇÃO
// Subject regexes (case-insensitive) in priority order: the first category
// that matches a subject wins. Security has top priority.
// Regexes de assunto (case-insensitive) em ordem de prioridade: a primeira
// categoria que casa com o assunto vence. Segurança tem prioridade máxima.
// ============================================================================

// Category is one class of "account evidence" email.
// Category é uma classe de e-mail que evidencia cadastro em um serviço.
type Category struct {
	Name     string            // stable identifier used in reports
	Priority int               // lower = evaluated first
	Patterns []*regexp.Regexp  // compiled case-insensitive patterns
}

// categorySpecs maps category names to their PT-BR/EN patterns.
// categorySpecs mapeia nomes de categoria aos padrões PT-BR/EN.
var categorySpecs = []struct {
	Name     string
	Priority int
	Patterns []string
}{
	{
		Name: "security", Priority: 1,
		Patterns: []string{
			`new login`,
			`login from`,
			`new device`,
			`senha alterada`,
			`seguran[çc]a atualizad`,
			`password (was )?changed`,
		},
	},
	{
		Name: "verification", Priority: 2,
		Patterns: []string{
			`verify your email`,
			`confirme seu e-?mail`,
			`c[óo]digo de verifica[çc][ãa]o`,
			`ative sua conta`,
			`confirm your (email|account)`,
		},
	},
	{
		Name: "welcome", Priority: 3,
		Patterns: []string{
			`welcome to`,
			`bem-vindo ao?s?`,
			`obrigado por se registrar`,
			`sua conta foi criada`,
		},
	},
	{
		Name: "receipt", Priority: 4,
		Patterns: []string{
			`fatura (est[áa] )?dispon[íi]vel`,
			`pagamento aprovado`,
			`pedido recebido`,
			`recibo`,
		},
	},
	{
		Name: "policy", Priority: 5,
		Patterns: []string{
			`privacy policy update`,
			`termos de uso atualizados`,
			`terms of (service|use) (update|updated)`,
		},
	},
}

// buildCategories compiles all specs once at startup; workers only read it.
// buildCategories compila todas as especificações uma vez no startup;
// os workers apenas a leem.
func buildCategories() []Category {
	cats := make([]Category, 0, len(categorySpecs))
	for _, spec := range categorySpecs {
		c := Category{Name: spec.Name, Priority: spec.Priority}
		for _, p := range spec.Patterns {
			re, err := regexp.Compile(`(?i)` + p)
			if err != nil {
				// A bad pattern here is a programming bug, not user input.
				panic(fmt.Sprintf("invalid category pattern %q: %v", p, err))
			}
			c.Patterns = append(c.Patterns, re)
		}
		cats = append(cats, c)
	}
	// Enforce evaluation order security > verification > welcome > receipt > policy.
	// Garante a ordem segurança > verificação > boas-vindas > recibo > política.
	sort.Slice(cats, func(i, j int) bool { return cats[i].Priority < cats[j].Priority })
	return cats
}

// ============================================================================
// ENTRY POINT / PONTO DE ENTRADA
// ============================================================================

func main() {
	// Stage 0: load configuration (env vars only, never hardcoded).
	// Etapa 0: carrega a configuração (só env vars, nunca hardcoded).
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}
	categories := buildCategories()

	// Temporary scaffold output; replaced by the real pipeline in next commits.
	// Saída provisória do scaffold; substituída pelo pipeline nos próximos commits.
	fmt.Printf("host=%s port=%s mailbox=%s since=%v workers=%d batch=%d ignored=%d categories=%d\n",
		cfg.Host, cfg.Port, cfg.Mailbox, cfg.Since, cfg.Workers, cfg.BatchSize,
		len(cfg.Ignore), len(categories))
}

// ============================================================================
// SENDER NORMALIZATION INPUTS / INSUMOS DE NORMALIZAÇÃO DE REMETENTE
// ============================================================================

// transactionalPrefixes are leftmost DNS labels commonly used by bulk/
// transactional senders. When stripping from a host we never go below two
// labels, so "foo.co.uk" stays intact.
// transactionalPrefixes são labels DNS à esquerda comuns em remetentes
// transacionais/massivos. Ao remover do host nunca descemos abaixo de dois
// labels, então "foo.co.uk" permanece intacto.
var transactionalPrefixes = map[string]bool{
	"no-reply": true, "noreply": true, "donotreply": true,
	"mail": true, "email": true, "e": true, "smtp": true,
	"mkt": true, "marketing": true, "news": true, "newsletter": true,
	"billing": true, "payments": true, "payment": true, "invoice": true,
	"notifications": true, "notify": true, "alert": true, "alerts": true,
	"account": true, "accounts": true, "info": true, "msg": true,
	"message": true, "messages": true, "bounce": true, "bounces": true,
}

// defaultIgnoreDomains are excluded from the final report so it stays clean.
// Extend at runtime with IGNORE_DOMAINS (comma separated).
// defaultIgnoreDomains são excluídos do relatório final para mantê-lo limpo.
// Estenda em tempo de execução com IGNORE_DOMAINS (separado por vírgulas).
var defaultIgnoreDomains = []string{
	// big techs / big techs
	"google.com", "youtube.com", "facebook.com", "instagram.com",
	"whatsapp.com", "linkedin.com", "twitter.com", "x.com",
	"tiktok.com", "netflix.com", "spotify.com", "amazon.com",
	"amazon.com.br", "apple.com", "icloud.com", "microsoft.com",
	"live.com", "office.com", "outlook.com",
	// monitoring services / serviços de monitoramento
	"sentry.io", "datadoghq.com", "newrelic.com", "statuspage.io",
	// known banks (BR) / bancos conhecidos (BR)
	"nubank.com.br", "itau.com.br", "bradesco.com.br", "bb.com.br",
	"santander.com.br", "caixa.gov.br",
}

