// Package harness provides the hermetic test infrastructure shared by the
// contract, E2E and BDD layers: an in-memory IMAP server (go-imap's
// imapmemserver behind imapserver) reachable over real TCP on 127.0.0.1,
// plus a tiny RFC 822 builder and a one-call pipeline runner.
//
// No test ever touches the network beyond loopback and no credentials are
// real: everything lives and dies inside the test process.
//
// O pacote harness provê a infraestrutura hermética compartilhada pelas
// camadas de contrato, E2E e BDD: um servidor IMAP em memória (imapmemserver
// atrás de imapserver) acessível por TCP real em 127.0.0.1, além de um
// construtor mínimo de RFC 822 e um executor de pipeline de uma chamada.
//
// Nenhum teste toca a rede fora do loopback e nenhuma credencial é real:
// tudo nasce e morre dentro do processo de teste.
package harness

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"e-crawpar/internal/core"
	"e-crawpar/internal/imapx"
)

// TestAccount is the fixed credential pair every test server accepts.
// TestAccount é o par fixo de credenciais que todo servidor de teste aceita.
const (
	TestUser = "tester@local.test"
	TestPass = "app-password-not-real"
)

// Msg describes one synthetic message appended to INBOX.
// Msg descreve uma mensagem sintética acrescentada à INBOX.
type Msg struct {
	From    string    // full From header value / valor completo do header From
	Subject string    // raw subject (may be RFC 2047 encoded) / bruto, pode vir codificado
	Date    time.Time // zero => no Date header / zero => sem header Date
}

// literal adapts bytes.Reader to imap.LiteralReader.
type literal struct{ r *bytes.Reader }

func (l literal) Size() int64 { return l.r.Size() }
func (l literal) Read(p []byte) (int, error) {
	return l.r.Read(p)
}

// BuildEML renders m as a minimal RFC 822 message. The body is filler: the
// crawler must never request it, and the fake server only serves it if asked
// (which contract tests forbid).
//
// BuildEML renderiza m como uma mensagem RFC 822 mínima. O corpo é enchimento:
// o crawler nunca deve pedi-lo, e o servidor fake só o serve se pedido (o que
// os testes de contrato proíbem).
func BuildEML(m Msg) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", m.From)
	b.WriteString("To: tester@local.test\r\n")
	if !m.Date.IsZero() {
		fmt.Fprintf(&b, "Date: %s\r\n", m.Date.Format("Mon, 02 Jan 2006 15:04:05 -0700"))
	}
	fmt.Fprintf(&b, "Subject: %s\r\n", m.Subject)
	fmt.Fprintf(&b, "Message-ID: <%d@synthetic.local>\r\n", time.Now().UnixNano())
	b.WriteString("\r\n")
	b.WriteString("BODY NEVER READ BY TESTS / CORPO NUNCA LIDO PELOS TESTES.\r\n")
	return []byte(b.String())
}

// quietLogger silences the in-memory server.
type quietLogger struct{}

func (quietLogger) Printf(format string, args ...any) {}

// StartServer boots the server without wire capture.
// StartServer sobe o servidor sem captura de tráfego.
func StartServer(t *testing.T, msgs []Msg) string {
	return StartServerWithTranscript(t, msgs, nil)
}

// ServerHandle is a manually managed server instance, for frameworks that
// have no *testing.T to hang cleanup on (e.g. godog steps).
//
// ServerHandle é uma instância gerenciada manualmente, para frameworks sem
// *testing.T onde pendurar o cleanup (ex.: steps do godog).
type ServerHandle struct {
	Addr string
	srv  *imapserver.Server
}

// Close shuts the server down.
// Close encerra o servidor.
func (h *ServerHandle) Close() { _ = h.srv.Close() }

// StartServerManual starts a server without requiring a *testing.T.
// StartServerManual sobe um servidor sem exigir *testing.T.
func StartServerManual(msgs []Msg, transcript io.Writer) (*ServerHandle, error) {
	mem := imapmemserver.New()
	user := imapmemserver.NewUser(TestUser, TestPass)
	mem.AddUser(user)
	if err := user.Create("INBOX", nil); err != nil {
		return nil, fmt.Errorf("create INBOX: %w", err)
	}
	for i, m := range msgs {
		raw := BuildEML(m)
		appendTime := m.Date
		if appendTime.IsZero() {
			appendTime = time.Now() // internal date; envelope Date stays absent
		}
		if _, err := user.Append("INBOX", literal{bytes.NewReader(raw)}, &imap.AppendOptions{
			Time:  appendTime,
			Flags: []imap.Flag{imap.FlagSeen}, // seeded unseen / semeada como não-lida
		}); err != nil {
			return nil, fmt.Errorf("append msg %d: %w", i+1, err)
		}
	}

	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		InsecureAuth: true, // plain-text auth allowed: loopback-only test server
		Logger:       quietLogger{},
		DebugWriter:  transcript,
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	go func() { _ = srv.Serve(ln) }()
	return &ServerHandle{Addr: ln.Addr().String(), srv: srv}, nil
}

// StartServerWithTranscript boots an in-memory IMAP server on a random
// loopback port with exactly one account (TestUser/TestPass) whose INBOX is
// pre-populated with msgs in order. When transcript is non-nil it receives
// the RAW IMAP wire traffic (which contains credentials — never log it).
//
// StartServerWithTranscript sobe um servidor IMAP em memória numa porta de
// loopback aleatória com exatamente uma conta (TestUser/TestPass) cuja INBOX
// vem populada com msgs em ordem. Se transcript não for nulo, recebe o
// TRÁFEGO IMAP BRUTO (que contém credenciais — nunca logar).
func StartServerWithTranscript(t *testing.T, msgs []Msg, transcript io.Writer) string {
	t.Helper()

	mem := imapmemserver.New()
	user := imapmemserver.NewUser(TestUser, TestPass)
	mem.AddUser(user)
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatalf("create INBOX: %v", err)
	}
	for i, m := range msgs {
		raw := BuildEML(m)
		appendTime := m.Date
		if appendTime.IsZero() {
			appendTime = time.Now() // internal date; envelope Date stays absent
		}
		// Seed as unseen; E2E asserts \Seen never appears / semeia como não-lida
		if _, err := user.Append("INBOX", literal{bytes.NewReader(raw)}, &imap.AppendOptions{
			Time:  appendTime,
			Flags: []imap.Flag{imap.FlagSeen},
		}); err != nil {
			t.Fatalf("append msg %d: %v", i+1, err)
		}
	}

	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		InsecureAuth: true, // plain-text auth allowed: loopback-only test server
		Logger:       quietLogger{},
		DebugWriter:  transcript,
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String()
}

// Dial connects a production ClientAdapter to addr over plain TCP (the fake
// server does not do TLS). Production code always uses DialTLS instead.
//
// Dial conecta um ClientAdapter de produção ao addr via TCP puro (o servidor
// fake não faz TLS). Código de produção sempre usa DialTLS.
func Dial(t *testing.T, addr string) *imapx.ClientAdapter {
	t.Helper()
	c, err := imapclient.DialInsecure(addr, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return imapx.NewAdapter(c)
}

// Options returns the collection options matching the started server.
// Options retorna as opções de coleta correspondentes ao servidor iniciado.
func Options(addr, mailbox string, batchSize int) imapx.Options {
	return imapx.Options{
		User:      TestUser,
		Password:  TestPass,
		Mailbox:   mailbox,
		BatchSize: batchSize,
	}
}

// RunPipeline executes stages 1-3 end to end against any IMAPClient and
// returns the aggregated report rows.
//
// RunPipeline executa os estágios 1-3 ponta a ponta contra qualquer
// IMAPClient e retorna as linhas agregadas do relatório.
func RunPipeline(t *testing.T, client imapx.IMAPClient, opt imapx.Options, workers int, ignore map[string]bool) []core.DomainStat {
	t.Helper()
	stats, err := RunPipelineErr(client, opt, workers, ignore)
	if err != nil {
		t.Fatalf("collect headers: %v", err)
	}
	return stats
}

// RunPipelineErr is the *testing.T-free variant of RunPipeline.
// RunPipelineErr é a variante sem *testing.T de RunPipeline.
func RunPipelineErr(client imapx.IMAPClient, opt imapx.Options, workers int, ignore map[string]bool) ([]core.DomainStat, error) {
	jobs := make(chan core.Job, opt.BatchSize)
	errCh := make(chan error, 1)
	go func() {
		defer close(jobs)
		errCh <- imapx.CollectHeaders(client, opt, jobs)
	}()
	results := core.RunWorkerPool(workers, core.BuildCategories(), ignore, jobs)
	stats := core.Collect(results)
	if err := <-errCh; err != nil {
		return nil, fmt.Errorf("collect headers: %w", err)
	}
	return stats, nil
}

// FetchAllFlags opens its own read-only session and returns the FLAGS of
// every message in INBOX, letting tests prove the crawler left flags alone
// (especially \\Seen).
//
// FetchAllFlags abre uma sessão própria somente-leitura e retorna os FLAGS de
// todas as mensagens da INBOX, permitindo provar que o crawler não mexeu nas
// flags (em particular \\Seen).
func FetchAllFlags(t *testing.T, addr string) [][]imap.Flag {
	t.Helper()
	c, err := imapclient.DialInsecure(addr, nil)
	if err != nil {
		t.Fatalf("dial for flags: %v", err)
	}
	defer func() { _ = c.Logout().Wait() }()
	if err := c.Login(TestUser, TestPass).Wait(); err != nil {
		t.Fatalf("login for flags: %v", err)
	}
	sel, err := c.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		t.Fatalf("select for flags: %v", err)
	}
	var all [][]imap.Flag
	if sel.NumMessages == 0 {
		return all
	}
	nums := make([]uint32, 0, sel.NumMessages)
	for n := uint32(1); n <= sel.NumMessages; n++ {
		nums = append(nums, n)
	}
	cmd := c.Fetch(imap.SeqSetNum(nums...), &imap.FetchOptions{Flags: true})
	for {
		msg := cmd.Next()
		if msg == nil {
			break
		}
		for {
			item := msg.Next()
			if item == nil {
				break
			}
			if fd, ok := item.(imapclient.FetchItemDataFlags); ok {
				all = append(all, fd.Flags)
			}
		}
	}
	if err := cmd.Close(); err != nil {
		t.Fatalf("fetch flags close: %v", err)
	}
	return all
}

// LoadFixtureDir reads every .eml file (sorted by name) from dir and converts
// it into synthetic Msg values by extracting Subject/From/Date headers.
//
// LoadFixtureDir lê todos os .eml (ordenados por nome) de dir e converte em
// Msg sintéticas extraindo os headers Subject/From/Date.
func LoadFixtureDir(t *testing.T, dir string) []Msg {
	t.Helper()
	entries, err := filepath.Glob(filepath.Join(dir, "*.eml"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("no .eml fixtures in %s: %v", dir, err)
	}
	var msgs []Msg
	for _, path := range entries {
		msgs = append(msgs, ParseEML(t, path))
	}
	return msgs
}

// ParseEML extracts Subject, From and Date from a fixture file so .eml files
// stay the single source of truth for E2E/BDD scenarios.
//
// ParseEML extrai Subject, From e Date de um arquivo de fixture para que os
// .eml sejam a única fonte da verdade dos cenários E2E/BDD.
func ParseEML(t *testing.T, path string) Msg {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	var m Msg
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" { // end of headers / fim dos headers
			break
		}
		name, value, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		switch strings.ToLower(name) {
		case "from":
			m.From = value
		case "subject":
			m.Subject = value
		case "date":
			parsed, err := time.Parse("Mon, 02 Jan 2006 15:04:05 -0700", value)
			if err != nil {
				t.Fatalf("fixture %s has bad Date %q: %v", path, value, err)
			}
			m.Date = parsed
		}
	}
	if m.From == "" || m.Subject == "" {
		t.Fatalf("fixture %s missing From or Subject", path)
	}
	return m
}

// FixtureDir resolves repo-root/testdata relative to a test package dir such
// as /test/e2e (two levels below the repository root).
//
// FixtureDir resolve repo-root/testdata a partir de um diretório de teste
// como /test/e2e (dois níveis abaixo da raiz).
func FixtureDir() string {
	return filepath.Join("..", "..", "testdata", "eml")
}
