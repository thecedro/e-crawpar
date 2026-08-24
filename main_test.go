package main

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestClassifyPriority ensures the fixed priority order wins when a subject
// matches more than one category (security > verification > welcome > ...).
// TestClassifyPriority garante que a ordem fixa de prioridade vença quando o
// assunto casa com mais de uma categoria.
func TestClassifyPriority(t *testing.T) {
	cats := buildCategories()

	tests := []struct {
		subject string
		want    string
	}{
		{"New login from Chrome on Windows", "security"},
		{"Senha alterada com sucesso", "security"},
		{"Verify your email to finish signing up", "verification"},
		{"Código de verificação: 123456", "verification"},
		{"Welcome to Acme, thanks for joining!", "welcome"},
		{"Bem-vindo ao Spotify", "welcome"},
		{"Sua fatura está disponível", "receipt"},
		{"Pedido recebido #1234", "receipt"},
		{"Privacy Policy Update — please review", "policy"},
		{"Termos de uso atualizados", "policy"},
		{"Lunch tomorrow?", ""},
		{"Random newsletter content", ""},
		// multi-match: security beats verification / segurança vence verificação
		{"New device detected — confirm your account", "security"},
	}
	for _, tt := range tests {
		if got := classify(tt.subject, cats); got != tt.want {
			t.Errorf("classify(%q) = %q, want %q", tt.subject, got, tt.want)
		}
	}
}

// TestNormalizeDomain checks transactional-prefix stripping and the
// two-label floor that protects ccTLDs like co.uk / com.br.
// TestNormalizeDomain verifica a remoção de prefixos transacionais e o piso
// de dois labels que protege ccTLDs como co.uk / com.br.
func TestNormalizeDomain(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{"amazon.com", "amazon.com"},
		{"AMAZON.COM.", "amazon.com"}, // trailing dot + case handled inside
		{"mail.booking.com", "booking.com"},
		{"no-reply.ebay.de", "ebay.de"},
		{"billing.stripe.com", "stripe.com"},
		{"notifications.github.com", "github.com"},
		{"foo.co.uk", "foo.co.uk"},                           // never below two labels
		{"mail.foo.co.uk", "foo.co.uk"},                      // strip stops at two labels
		{"email.example.com.br", "example.com.br"},           // known prefix, ccTLD floor respected
		{"random.sub.example.com", "random.sub.example.com"}, // unknown sub preserved
	}
	for _, tt := range tests {
		if got := normalizeDomain(tt.host); got != tt.want {
			t.Errorf("normalizeDomain(%q) = %q, want %q", tt.host, got, tt.want)
		}
	}
}

func TestDecodeSubject(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain subject", "plain subject"},
		{"=?utf-8?b?QmVtLXZpbmRvIGFvIFNwb3RpZnk=?=", "Bem-vindo ao Spotify"},
		{"=?iso-8859-1?q?Configura=E7=F5es=20de=20seguran=E7a?=", "Configurações de segurança"},
		{"=?x-unknown?Q?weird?=", "=?x-unknown?Q?weird?="}, // raw fallback
	}
	for _, tt := range tests {
		if got := decodeSubject(tt.in); got != tt.want {
			t.Errorf("decodeSubject(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWorkerFiltersNoise(t *testing.T) {
	cats := buildCategories()
	jobs := make(chan job, 4)
	results := make(chan result, 8)

	jobs <- job{subject: "Verify your email", from: "noreply@svc.io", host: "mail.svc.io",
		date: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)}
	jobs <- job{subject: "Welcome to Netflix", from: "info@netflix.com", host: "netflix.com",
		date: time.Date(2024, 3, 2, 0, 0, 0, 0, time.UTC)} // ignored domain
	jobs <- job{subject: "Unrelated promo", from: "a@b.co", host: "b.co",
		date: time.Date(2024, 3, 3, 0, 0, 0, 0, time.UTC)} // no category match
	close(jobs)

	go worker(jobs, results, cats, map[string]bool{"svc.io": false, "netflix.com": true})

	r := <-results // only the first job must survive / só o primeiro job sobrevive
	if r.domain != "svc.io" || r.category != "verification" || r.sender != "noreply@svc.io" {
		t.Fatalf("unexpected result: %+v", r)
	}
	select {
	case extra := <-results:
		t.Fatalf("expected no second result, got %+v", extra)
	default: // channel drained, as expected / canal drenado, como esperado
	}
}

// TestCollectAggregationAndOrdering covers the collector: earliest-date
// tracking, sample preference (verification > welcome > first seen),
// distinct-sender counting and first-seen ordering with unknown dates last.
//
// TestCollectAggregationAndOrdering cobre a coletora: menor data, preferência
// de amostra (verificação > boas-vindas > primeira vista), contagem de
// remetentes distintos e ordenação por primeira ocorrência com datas
// desconhecidas por último.
func TestCollectAggregationAndOrdering(t *testing.T) {
	d := func(y int, m time.Month, day int) time.Time {
		return time.Date(y, m, day, 0, 0, 0, 0, time.UTC)
	}
	results := make(chan result, 8)
	results <- result{domain: "svc.io", sender: "no-reply@svc.io", category: "welcome",
		subject: "Welcome to svc", date: d(2020, 5, 10)}
	results <- result{domain: "svc.io", sender: "billing@svc.io", category: "receipt",
		subject: "Your invoice", date: d(2021, 1, 1)}
	results <- result{domain: "svc.io", sender: "verify@svc.io", category: "verification",
		subject: "Verify your email", date: d(2020, 5, 9)} // earlier than welcome!
	results <- result{domain: "old.example", sender: "a@old.example", category: "policy",
		subject: "Terms updated"} // zero date -> must sort last
	close(results)

	stats := collect(results)
	if len(stats) != 2 {
		t.Fatalf("want 2 domains, got %d: %+v", len(stats), stats)
	}

	first, second := stats[0], stats[1]
	if first.Domain != "svc.io" || first.FirstSeen != "2020-05-09" {
		t.Errorf("svc.io row wrong: %+v", first)
	}
	if first.SampleSubject != "Verify your email" {
		t.Errorf("sample should prefer verification, got %q", first.SampleSubject)
	}
	if !first.MultiSender || first.DistinctSenders != 3 {
		t.Errorf("multi-sender alert wrong: %+v", first)
	}
	if got := strings.Join(first.Categories, ","); got != "verification,welcome,receipt" {
		t.Errorf("category order wrong: %q", got)
	}
	if second.Domain != "old.example" || second.FirstSeen != "" {
		t.Errorf("undated domain should sort last without a date: %+v", second)
	}

	var buf bytes.Buffer
	renderText(&buf, stats)
	out := buf.String()
	for _, want := range []string{"MULTIPLE SENDERS", "2020-05-09", "2 unique domains"} {
		if !strings.Contains(out, want) {
			t.Errorf("text report missing %q:\n%s", want, out)
		}
	}
}

// TestFriendlyErrorClassification ensures common network/IMAP failures map
// to bilingual user errors instead of raw stack traces.
//
// TestFriendlyErrorClassification garante que falhas comuns de rede/IMAP
// virem erros bilíngues em vez de stack traces brutos.
func TestFriendlyErrorClassification(t *testing.T) {
	refused := &net.OpError{Op: "dial", Err: errors.New("connect: connection refused")}
	dns := &net.DNSError{Err: "no such host", Name: "wrong.host", IsNotFound: true}

	if _, ok := friendlyDialError(refused, "h:1").(*UserError); !ok {
		t.Error("refused connection should yield a UserError")
	}
	ue := friendlyDialError(dns, "wrong.host:993").(*UserError)
	if !strings.Contains(ue.MsgEN, "not found") {
		t.Errorf("DNS error misclassified: %q", ue.MsgEN)
	}
	if !strings.Contains(ue.HintEN, "imap.gmail.com") {
		t.Errorf("DNS hint should list provider hosts: %q", ue.HintEN)
	}

	auth := errors.New("AUTHENTICATIONFAILED: Invalid credentials")
	ue = friendlyAuthError(auth, "a@b.c").(*UserError)
	if !strings.Contains(ue.MsgPT, "Login recusado") || !strings.Contains(ue.HintPT, "SENHA DE APP") {
		t.Errorf("auth error misclassified: %+v", ue)
	}
	if strings.Contains(ue.Error(), "a@b.c-password") { // no secrets ever leak
		t.Error("error message must never contain credentials")
	}

	badBox := errors.New("EXAMINE: Mailbox doesn't exist")
	ue = friendlySelectError(badBox, "FOO").(*UserError)
	if !strings.Contains(ue.MsgEN, "FOO") {
		t.Errorf("select error should mention mailbox: %q", ue.MsgEN)
	}

	// Unknown errors still become UserErrors (generic fallback, no panic).
	if _, ok := friendlyDialError(errors.New("weird"), "h:1").(*UserError); !ok {
		t.Error("fallback should also be a UserError")
	}
}

// TestDotEnvRoundTrip covers parsing quirks and the quoted write-back.
// TestDotEnvRoundTrip cobre peculiaridades de parse e a escrita entre aspas.
func TestDotEnvRoundTrip(t *testing.T) {
	parsed := parseDotEnv("# comment\n\nIMAP_HOST=imap.gmail.com\n  IMAP_PORT = 993 \n" +
		"IMAP_USER=\"user@gmail.com\"\nIMAP_APP_PASSWORD='abcd efgh ijkl mnop'\nbroken line")
	want := map[string]string{
		"IMAP_HOST":         "imap.gmail.com",
		"IMAP_PORT":         "993",
		"IMAP_USER":         "user@gmail.com",
		"IMAP_APP_PASSWORD": "abcd efgh ijkl mnop",
	}
	for k, v := range want {
		if parsed[k] != v {
			t.Errorf("parse %q = %q, want %q", k, parsed[k], v)
		}
	}
	if _, ok := parsed["broken"]; ok {
		t.Error("line without '=' must be ignored")
	}

	path := filepath.Join(t.TempDir(), ".env")
	values := map[string]string{
		"IMAP_HOST": "imap.test", "IMAP_PORT": "993", "IMAP_USER": "a@b.c",
		"IMAP_APP_PASSWORD": "pass with spaces",
	}
	if err := writeDotEnv(path, values); err != nil {
		t.Fatal(err)
	}
	back := readDotEnv(path)
	for k, v := range values {
		if back[k] != v { // spaces survive quoting / espaços sobrevivem às aspas
			t.Errorf("roundtrip %q = %q, want %q", k, back[k], v)
		}
	}
	if back["IMAP_APP_PASSWORD"] != values["IMAP_APP_PASSWORD"] {
		t.Error("password altered in roundtrip") // never acceptable
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf(".env permission = %v, want -rw-------", info.Mode().Perm())
	}
}

// TestRunSetupWizard drives the interactive flow through piped stdin,
// including the retry loop, ending when valid credentials are accepted.
//
// TestRunSetupWizard conduz o fluxo interativo via stdin canalizado,
// incluindo o loop de repetição, terminando quando credenciais válidas
// são aceitas.
func TestRunSetupWizard(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(oldWd) })
	os.Chdir(dir) // wizard writes .env to cwd; keep test sandboxed

	// Stub the network probe: fail twice, then succeed. No real IMAP traffic.
	// Simula a validação: falha duas vezes, depois funciona. Sem tráfego real.
	attempts := 0
	origProbe := probeAccountFn
	probeAccountFn = func(host, port, user, pass string) error {
		attempts++
		if attempts < 3 {
			return friendlyAuthError(errors.New("AUTHENTICATIONFAILED"), user)
		}
		if host != "imap.gmail.com" || port != "993" || user != "me@gmail.com" || pass != "goodpass" {
			t.Errorf("probe got unexpected args: %s %s %s %s", host, port, user, pass)
		}
		return nil
	}
	t.Cleanup(func() { probeAccountFn = origProbe })

	// provider=1 (Gmail), email, bad password x2, then good one.
	// provedor=1 (Gmail), e-mail, senha ruim x2, depois a boa.
	in := strings.NewReader("1\nme@gmail.com\nwrongpass\nme@gmail.com\nwrongpass2\nme@gmail.com\ngoodpass\n")
	var out bytes.Buffer
	_, err := runSetup(bufio.NewReader(in), &out)
	logs := out.String()
	if err != nil {
		t.Fatalf("wizard failed: %v\noutput:\n%s", err, logs)
	}
	if !strings.Contains(logs, "Testing the connection") {
		t.Errorf("wizard should validate before saving:\n%s", logs)
	}
	saved := readDotEnv(filepath.Join(dir, envFile))
	if saved["IMAP_HOST"] != "imap.gmail.com" || saved["IMAP_APP_PASSWORD"] != "goodpass" {
		t.Errorf("unexpected saved values: %+v", saved)
	}
}

// TestBootstrapNonInteractive ensures missing credentials without a TTY
// produce guidance instead of a crash.
//
// TestBootstrapNonInteractive garante que credenciais ausentes sem TTY gerem
// orientação em vez de crash.
func TestBootstrapNonInteractive(t *testing.T) {
	t.Setenv("IMAP_HOST", "")
	t.Setenv("IMAP_USER", "")
	t.Setenv("IMAP_APP_PASSWORD", "")
	cfg, err := bootstrap()
	if cfg != nil {
		t.Fatal("expected nil config")
	}
	var ue *UserError
	if !errors.As(err, &ue) {
		t.Fatalf("expected UserError, got %v", err)
	}
	if !strings.Contains(ue.HintEN, ".env") {
		t.Errorf("hint should explain manual .env setup: %q", ue.HintEN)
	}
}

// TestLoadConfigMissingEnv checks the structured missing-vars error.
// TestLoadConfigMissingEnv verifica o erro estruturado de variáveis ausentes.
func TestLoadConfigMissingEnv(t *testing.T) {
	_, err := loadConfig(map[string]string{"IMAP_HOST": "h"})
	var miss *missingEnvError
	if !errors.As(err, &miss) {
		t.Fatalf("expected missingEnvError, got %v", err)
	}
	want := []string{"IMAP_USER", "IMAP_APP_PASSWORD"}
	if len(miss.Keys) != len(want) {
		t.Fatalf("missing keys = %v, want %v", miss.Keys, want)
	}
	for i, k := range want {
		if miss.Keys[i] != k {
			t.Errorf("keys[%d] = %q, want %q", i, miss.Keys[i], k)
		}
	}
}

// TestWriteHTMLReport ensures the standalone page is generated with escaped
// subjects (XSS-safe) and correct counters.
//
// TestWriteHTMLReport garante que a página standalone é gerada com assuntos
// escapados (seguro contra XSS) e contadores corretos.
func TestWriteHTMLReport(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(oldWd) })
	os.Chdir(dir) // report is written to cwd; keep test sandboxed

	stats := []domainStat{
		{Domain: "svc.io", FirstSeen: "2020-05-09", Categories: []string{"verification", "welcome"},
			Occurrences: 2, SampleSubject: `Verify <script>alert(1)</script>`,
			DistinctSenders: 3, MultiSender: true},
	}
	path, err := writeHTMLReport(stats)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)

	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Error("subject must be HTML-escaped") // template auto-escaping
	}
	for _, want := range []string{
		"&lt;script&gt;", // escaped payload
		"svc.io", "2020-05-09", "múltiplos",
		"1</b>", // total domains counter
	} {
		if !strings.Contains(html, want) {
			t.Errorf("html missing %q", want)
		}
	}
}
