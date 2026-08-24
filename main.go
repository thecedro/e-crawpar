// Package main implements an IMAP crawler that scans the headers
// (From, Subject, Date) of your own mailbox to discover which services
// you have accounts with — without ever downloading message bodies.
//
// O pacote main implementa um crawler IMAP que varre os headers
// (From, Subject, Date) da sua própria caixa de e-mail para descobrir
// em quais serviços você tem conta — sem nunca baixar o corpo das mensagens.
package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"mime"
	"net"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
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
// PIPELINE STAGE 1 — COLLECTION / ETAPA 1 DO PIPELINE — COLETA
// Connects over TLS, selects the mailbox READ-ONLY and streams only the
// ENVELOPE (From, Subject, Date) in fixed-size batches. Message bodies are
// never requested from the server.
// Conecta via TLS, seleciona a caixa em modo SOMENTE-LEITURA e transmite
// apenas o ENVELOPE (From, Subject, Date) em lotes de tamanho fixo.
// O corpo das mensagens nunca é solicitado ao servidor.
// ============================================================================

// job is one extracted header triplet handed to the worker pool.
// job é um trio de headers extraído entregue ao pool de workers.
type job struct {
	subject string
	from    string    // original first From address: "local@host"
	host    string    // lowercase domain part of From
	date    time.Time // envelope Date header
}

// decodeSubject decodes RFC 2047 encoded-words ("=?utf-8?...?=") commonly
// found in Subject headers. Falls back to the raw string on any error so a
// weird charset can never drop a message from the analysis.
// decodeSubject decodifica encoded-words RFC 2047 ("=?utf-8?...?=") comuns
// no Subject. Em caso de erro retorna o texto bruto, para que um charset
// esquisito jamais derrube uma mensagem da análise.
var wordDecoder = &mime.WordDecoder{
	CharsetReader: func(charset string, input io.Reader) (io.Reader, error) {
		switch strings.ToLower(charset) {
		case "iso-8859-1", "latin1":
			// Latin-1 maps bytes 1:1 to Unicode code points U+0000..U+00FF.
			data, err := io.ReadAll(input)
			if err != nil {
				return nil, err
			}
			var buf strings.Builder
			for _, b := range data {
				buf.WriteRune(rune(b))
			}
			return strings.NewReader(buf.String()), nil
		default:
			// windows-1252 etc. are not supported without x/text; caller falls back.
			return nil, fmt.Errorf("unsupported charset %q", charset)
		}
	},
}

func decodeSubject(s string) string {
	if !strings.Contains(s, "=?") {
		return s
	}
	if d, err := wordDecoder.DecodeHeader(s); err == nil && d != "" {
		return d
	}
	return s
}

// collectJobs runs the whole collection stage: dial, login, select read-only,
// list target sequence numbers and fetch ENVELOPEs batch by batch, pushing
// one job per usable message into jobs. Closes nothing — the caller owns the
// channel lifecycle.
//
// collectJobs executa todo o estágio de coleta: conexão, login, select
// somente-leitura, listagem dos sequence numbers alvo e busca dos ENVELOPEs
// lote a lote, empurrando um job por mensagem útil em jobs. Não fecha nada —
// o dono do canal é quem chama.
func collectJobs(cfg *Config, jobs chan<- job) error {
	addr := cfg.Host + ":" + cfg.Port

	// TLS is mandatory: credentials travel inside this session. A dial
	// timeout keeps the tool from hanging forever on a dead server.
	// TLS é obrigatório: as credenciais viajam dentro desta sessão. O timeout
	// de conexão evita que a ferramenta trave eternamente num servidor morto.
	c, err := imapclient.DialTLS(addr, &imapclient.Options{
		TLSConfig: &tls.Config{ServerName: cfg.Host},
		Dialer:    &net.Dialer{Timeout: 30 * time.Second},
	})
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer func() { c.Logout().Wait() }() // best-effort, never masks real errors

	if err := c.Login(cfg.User, cfg.Password).Wait(); err != nil {
		return fmt.Errorf("login %s: %w", cfg.User, err)
	}

	// Read-only select (EXAMINE): guarantees no \Seen flag is ever touched.
	// Select somente-leitura (EXAMINE): garante que nenhuma flag \Seen seja tocada.
	sel, err := c.Select(cfg.Mailbox, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return fmt.Errorf("select %q: %w", cfg.Mailbox, err)
	}
	total := sel.NumMessages

	// Build the ordered list of message sequence numbers to scan:
	// - IMAP_SINCE unset -> every message in the mailbox;
	// - IMAP_SINCE set   -> server-side SEARCH SINCE (internal date).
	// Monta a lista ordenada de sequence numbers a varrer:
	// - sem IMAP_SINCE -> todas as mensagens da caixa;
	// - com IMAP_SINCE -> SEARCH SINCE no servidor (data interna).
	var seqNums []uint32
	if cfg.Since == nil {
		seqNums = make([]uint32, 0, total)
		for n := uint32(1); n <= total; n++ {
			seqNums = append(seqNums, n)
		}
	} else {
		searchData, err := c.Search(&imap.SearchCriteria{Since: *cfg.Since}, nil).Wait()
		if err != nil {
			return fmt.Errorf("search since %s: %w", cfg.Since.Format(time.RFC3339), err)
		}
		switch all := searchData.All.(type) {
		case imap.SeqSet:
			nums, ok := all.Nums()
			if !ok {
				return fmt.Errorf("server returned an unbounded search result")
			}
			seqNums = nums
		default:
			return fmt.Errorf("unexpected search result type %T", searchData.All)
		}
	}

	// Batched ENVELOPE fetch. Each round trip asks for headers of at most
	// BATCH_SIZE messages, keeping memory flat on huge mailboxes.
	// Busca de ENVELOPE em lotes. Cada ida ao servidor pede os headers de no
	// máximo BATCH_SIZE mensagens, mantendo a memória estável em caixas grandes.
	fetchOpts := &imap.FetchOptions{Envelope: true}
	for start := 0; start < len(seqNums); start += cfg.BatchSize {
		end := min(start+cfg.BatchSize, len(seqNums))
		seqSet := imap.SeqSetNum(seqNums[start:end]...)

		cmd := c.Fetch(seqSet, fetchOpts)
	envelopeLoop:
		for {
			msg := cmd.Next() // nil when this batch is exhausted
			if msg == nil {
				break
			}
			for {
				item := msg.Next() // nil when all items of this message are done
				if item == nil {
					break
				}
				envData, ok := item.(imapclient.FetchItemDataEnvelope)
				if !ok {
					continue
				}
				env := envData.Envelope
				// Skip messages with no parsable From — nothing to attribute them to.
				// Descarta mensagens sem From parseável — nada a que atribuí-las.
				if env == nil || len(env.From) == 0 {
					continue
				}
				from := env.From[0]
				host := strings.ToLower(strings.TrimSuffix(from.Host, "."))
				if host == "" {
					continue
				}
				jobs <- job{
					subject: decodeSubject(env.Subject),
					from:    from.Mailbox + "@" + from.Host,
					host:    host,
					date:    env.Date,
				}
				continue envelopeLoop
			}
		}
		if err := cmd.Close(); err != nil {
			return fmt.Errorf("fetch batch [%d..%d): %w", start, end, err)
		}
	}
	return nil
}

// ============================================================================
// PIPELINE STAGE 2 — WORKER POOL / ETAPA 2 DO PIPELINE — POOL DE WORKERS
// N goroutines consume jobs from a shared channel, classify the subject,
// normalize the sender domain, apply the noise filter and emit results into
// a single channel. The collector (stage 3) is the only writer of the
// aggregate state, so no mutex is needed.
//
// N goroutines consomem jobs de um canal compartilhado, classificam o
// assunto, normalizam o domínio do remetente, aplicam o filtro de ruído e
// emitem resultados num único canal. A coletora (etapa 3) é a única
// escritora do estado agregado, portanto nenhum mutex é necessário.
// ============================================================================

// result is a fully processed job, ready for aggregation.
// result é um job totalmente processado, pronto para agregação.
type result struct {
	domain   string // normalized base domain ("amazon.com")
	sender   string // original distinct address ("no-reply@amazon.com")
	category string // matched category name ("verification")
	subject  string // decoded subject, kept as report sample candidate
	date     time.Time
}

// classify returns the first category matching the subject, or "" when none
// does. Categories are evaluated in priority order (security first), so a
// subject that matches two patterns keeps its strongest signal.
//
// classify retorna a primeira categoria que casa com o assunto, ou "" se
// nenhuma casar. As categorias são avaliadas em ordem de prioridade
// (segurança primeiro), então um assunto que case com dois padrões mantém
// seu sinal mais forte.
func classify(subject string, cats []Category) string {
	for _, c := range cats {
		for _, re := range c.Patterns {
			if re.MatchString(subject) {
				return c.Name
			}
		}
	}
	return ""
}

// normalizeDomain reduces a mail host to its base service domain:
// "mail.booking.com" -> "booking.com" only while the leftmost label is a
// known transactional prefix; unknown subdomains are preserved because we
// never strip blindly below two labels ("foo.co.uk" stays "foo.co.uk").
//
// normalizeDomain reduz um host de e-mail ao domínio base do serviço:
// "mail.booking.com" -> "booking.com" apenas enquanto o label mais à esquerda
// for um prefixo transacional conhecido; subdomínios desconhecidos são
// preservados porque nunca removemos às cegas abaixo de dois labels
// ("foo.co.uk" continua "foo.co.uk").
func normalizeDomain(host string) string {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	labels := strings.Split(host, ".")
	for len(labels) > 2 && transactionalPrefixes[labels[0]] {
		labels = labels[1:]
	}
	return strings.Join(labels, ".")
}

// worker consumes jobs until the jobs channel closes. Jobs whose subject
// matches nothing or whose domain is ignored produce no result — silence is
// the noise filter at work.
//
// worker consome jobs até o canal fechar. Jobs cujo assunto não casa com
// nada ou cujo domínio está na ignore-list não geram resultado — o silêncio
// é o filtro de ruído em ação.
func worker(jobs <-chan job, results chan<- result, cats []Category, ignore map[string]bool) {
	for j := range jobs {
		category := classify(j.subject, cats)
		if category == "" {
			continue // not an account-evidence email / não evidencia cadastro
		}
		domain := normalizeDomain(j.host)
		if ignore[domain] {
			continue // configured noise / ruído configurado
		}
		results <- result{
			domain:   domain,
			sender:   j.from,
			category: category,
			subject:  j.subject,
			date:     j.date,
		}
	}
}

// runWorkerPool spawns cfg.Workers workers and wires the pipeline so that
// closing jobs automatically drains and closes results. It blocks until all
// work is done.
//
// runWorkerPool cria cfg.Workers workers e conecta o pipeline de modo que
// fechar jobs drene e feche results automaticamente. Bloqueia até todo o
// trabalho terminar.
func runWorkerPool(cfg *Config, cats []Category, jobs <-chan job) <-chan result {
	results := make(chan result)

	var wg sync.WaitGroup
	wg.Add(cfg.Workers)
	for i := 0; i < cfg.Workers; i++ {
		go func() {
			defer wg.Done()
			worker(jobs, results, cats, cfg.Ignore)
		}()
	}

	// Single closer goroutine: results stays open until every worker exits,
	// giving the collector a clean termination signal without mutexes.
	// Goroutine única que fecha: results fica aberto até todos os workers
	// terminarem, dando à coletora sinal limpo de término sem mutexes.
	go func() {
		wg.Wait()
		close(results)
	}()
	return results
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
	Name     string           // stable identifier used in reports
	Priority int              // lower = evaluated first
	Patterns []*regexp.Regexp // compiled case-insensitive patterns
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

	// Stage 1: producer goroutine streams header jobs from IMAP.
	// Etapa 1: goroutine produtora transmite jobs de headers do IMAP.
	jobs := make(chan job, cfg.BatchSize)
	errCh := make(chan error, 1)
	go func() {
		defer close(jobs)
		if err := collectJobs(cfg, jobs); err != nil {
			errCh <- err
		}
	}()

	// Stages 2: worker pool classifies/normalizes/filters concurrently.
	// Etapa 2: pool de workers classifica/normaliza/filtra em paralelo.
	results := runWorkerPool(cfg, categories, jobs)

	// Temporary drain; replaced by the collector in the next commit.
	// Dreno temporário; substituído pela coletora no próximo commit.
	count := 0
	for r := range results {
		count++
		if count <= 5 {
			fmt.Printf("sample: date=%s domain=%s cat=%s from=%s subject=%.50q\n",
				r.date.Format("2006-01-02"), r.domain, r.category, r.sender, r.subject)
		}
	}

	if err := <-errCh; err != nil {
		fmt.Fprintf(os.Stderr, "collect error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("matched %d emails (workers=%d batch=%d categories=%d ignored=%d)\n",
		count, cfg.Workers, cfg.BatchSize, len(categories), len(cfg.Ignore))
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
