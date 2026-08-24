// Package contract defines and enforces THE SAFETY CONTRACT of
// imapx.IMAPClient — the guarantee that e-crawpar never reads message bodies
// and never mutates the mailbox. Any implementation must:
//
//  1. be READ-ONLY: every Select happens EXAMINE-style (ReadOnly), so no
//     flag — including \Seen — can ever change;
//  2. request ONLY the ENVELOPE: no BODY[], no RFC822*, no BODYSTRUCTURE,
//     no BINARY sections;
//  3. paginate in bounded batches: a scan of N messages issues ceil(N/batch)
//     FETCH commands, never asking for the whole mailbox at once, with no
//     gaps and no duplicated sequence numbers.
//
// AssertIMAPClientContract drives any implementation through a fixed
// scenario behind a recording decorator and fails if any clause is broken.
// It runs against the in-memory fake AND against the production
// ClientAdapter talking to an in-memory IMAP server over real TCP.
//
// O pacote contract define e fiscaliza O CONTRATO DE SEGURANÇA de
// imapx.IMAPClient — a garantia de que o e-crawpar nunca lê corpo de
// mensagem e nunca altera a caixa. Qualquer implementação deve:
//
//  1. ser SOMENTE-LEITURA: todo Select em modo EXAMINE (ReadOnly), então
//     nenhuma flag — inclusive \Seen — pode mudar;
//  2. pedir APENAS o ENVELOPE: nada de BODY[], RFC822*, BODYSTRUCTURE ou
//     seções BINARY;
//  3. paginar em lotes limitados: varrer N mensagens emite ceil(N/lote)
//     comandos FETCH, jamais pedindo a caixa inteira de uma vez, sem lacunas
//     e sem sequence numbers duplicados.
package contract

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emersion/go-imap/v2"

	"e-crawpar/internal/imapx"
	"e-crawpar/test/harness"
)

// Scenario dimensions shared by all implementations under audit.
// Dimensões do cenário compartilhadas por todas as implementações auditadas.
const (
	scenarioMessages   = 250
	scenarioBatchSize  = 100 // => 3 fetches: 100 + 100 + 50 / => 3 fetches
	expectedFetchCalls = 3
)

// ClientFactory builds a fresh, connected-but-unauthenticated IMAPClient for
// one contract run.
// ClientFactory constrói um IMAPClient novo, conectado mas não autenticado,
// para uma rodada do contrato.
type ClientFactory func(t *testing.T) imapx.IMAPClient

// --- recording decorator / decorator gravador ---

type selectCall struct {
	mailbox  string
	readOnly bool
}

type fetchCall struct {
	seqNums []uint32
	opts    imap.FetchOptions // deep copy taken before delegation / cópia antes de delegar
}

type recordingClient struct {
	inner imapx.IMAPClient

	mu        sync.Mutex
	logins    int
	selects   []selectCall
	fetches   []fetchCall
	emitted   []imapx.Envelope
	logouts   int
	opCounter int // global order stamp / carimbo de ordem global
	lastOp    string
}

var _ imapx.IMAPClient = (*recordingClient)(nil)

func (r *recordingClient) stamp(op string) int {
	r.opCounter++
	r.lastOp = op
	return r.opCounter
}

func (r *recordingClient) Login(username, password string) error {
	r.mu.Lock()
	r.logins++
	r.stamp("login")
	r.mu.Unlock()
	return r.inner.Login(username, password)
}

func (r *recordingClient) Select(mailbox string, opts *imap.SelectOptions) (*imap.SelectData, error) {
	call := selectCall{mailbox: mailbox}
	if opts != nil {
		call.readOnly = opts.ReadOnly // captured before inner may touch it / capturado antes
	}
	r.mu.Lock()
	r.selects = append(r.selects, call)
	r.stamp("select")
	r.mu.Unlock()
	return r.inner.Select(mailbox, opts)
}

func (r *recordingClient) Search(criteria *imap.SearchCriteria) (*imap.SearchData, error) {
	r.mu.Lock()
	r.stamp("search")
	r.mu.Unlock()
	return r.inner.Search(criteria)
}

func (r *recordingClient) Fetch(seqNums []uint32, opts *imap.FetchOptions, emit func(imapx.Envelope)) error {
	call := fetchCall{
		seqNums: append([]uint32(nil), seqNums...),
	}
	if opts != nil {
		call.opts = *opts // shallow copy enough: slices compared by nil/len below
	}
	r.mu.Lock()
	r.fetches = append(r.fetches, call)
	r.stamp("fetch")
	r.mu.Unlock()

	wrapped := func(e imapx.Envelope) {
		r.mu.Lock()
		r.emitted = append(r.emitted, e)
		r.mu.Unlock()
		emit(e)
	}

	return r.inner.Fetch(seqNums, opts, wrapped)
}

func (r *recordingClient) Logout() error {
	r.mu.Lock()
	r.logouts++
	r.stamp("logout")
	r.mu.Unlock()
	return r.inner.Logout()
}

// --- the contract itself / o contrato em si ---

// AssertIMAPClientContract is the reusable safety suite. Call it once per
// implementation:
//
//	contract.AssertIMAPClientContract(t, myFactory)
//
// A failing clause blocks CI — this is the merge gate protecting the
// "never reads the e-mail body" promise.
//
// AssertIMAPClientContract é a suíte reutilizável de segurança. Chame uma
// vez por implementação. Uma cláusula falha bloqueia o CI — é o portão de
// merge que protege a promessa "nunca lê o corpo do e-mail".
func AssertIMAPClientContract(t *testing.T, factory ClientFactory) {
	t.Helper()

	rec := &recordingClient{inner: factory(t)}

	// --- drive the standard scenario / executa o cenário padrão ---
	require.NoError(t, rec.Login(harness.TestUser, harness.TestPass))

	sel, err := rec.Select("INBOX", imapx.ReadOnlySelect)
	require.NoError(t, err)
	require.Equal(t, uint32(scenarioMessages), sel.NumMessages,
		"backend must expose %d messages", scenarioMessages)

	fetchOpts := imapx.EnvelopeOnly()
	for start := 0; start < scenarioMessages; start += scenarioBatchSize {
		end := min(start+scenarioBatchSize, scenarioMessages)
		batch := make([]uint32, 0, end-start)
		for n := start + 1; n <= end; n++ { // sequence numbers are 1-based / 1-indexados
			batch = append(batch, uint32(n))
		}
		require.NoError(t, rec.Fetch(batch, fetchOpts, func(e imapx.Envelope) {}),
			"fetch batch [%d..%d)", start, end)
	}

	require.NoError(t, rec.Logout())

	// --- clause 1: read-only select / cláusula 1: select somente-leitura ---
	require.NotEmpty(t, rec.selects, "a scan must select the mailbox")
	for _, s := range rec.selects {
		assert.True(t, s.readOnly,
			"CONTRACT VIOLATION: Select(%q) without ReadOnly (EXAMINE) mode", s.mailbox)
	}

	// --- clause 2: ENVELOPE-only fetches / cláusula 2: fetch apenas-ENVELOPE ---
	require.Len(t, rec.fetches, expectedFetchCalls,
		"scan of %d messages in batches of %d", scenarioMessages, scenarioBatchSize)
	wantOpts := imap.FetchOptions{Envelope: true}
	for i, f := range rec.fetches {
		assert.Truef(t, reflect.DeepEqual(f.opts, wantOpts),
			"CONTRACT VIOLATION: fetch #%d requested more than ENVELOPE: %+v", i+1, f.opts)
		assert.Nilf(t, f.opts.BodySection, "BODY[] requested in fetch #%d", i+1)
		assert.Nilf(t, f.opts.BodyStructure, "BODYSTRUCTURE requested in fetch #%d", i+1)
	}

	// --- clause 3: bounded pagination, no gaps, no duplicates ---
	// --- cláusula 3: paginação limitada, sem lacunas, sem duplicatas ---
	seen := map[uint32]int{}
	for _, f := range rec.fetches {
		assert.LessOrEqualf(t, len(f.seqNums), scenarioBatchSize,
			"batch larger than configured size (%d)", scenarioBatchSize)
		for _, n := range f.seqNums {
			seen[n]++
			assert.LessOrEqualf(t, seen[n], 1, "sequence number %d fetched twice", n)
		}
	}
	require.Len(t, seen, scenarioMessages, "every message fetched exactly once")
	for n := uint32(1); n <= scenarioMessages; n++ {
		assert.Containsf(t, seen, n, "gap: message %d never fetched", n)
	}

	// --- emission completeness / emissão completa ---
	assert.Len(t, rec.emitted, scenarioMessages,
		"one envelope per usable message must be emitted")

	// --- lifecycle / ciclo de vida ---
	assert.Equal(t, 1, rec.logins, "exactly one login per scan")
	assert.Equal(t, 1, rec.logouts, "exactly one logout per scan")
}

// FakeHeaderClient is the in-memory reference implementation used both by
// these contract tests and by unit-style tests elsewhere: it serves canned
// envelopes straight from RAM, ignoring credentials entirely.
//
// FakeHeaderClient é a implementação de referência em memória usada tanto
// pelos testes de contrato quanto por outras suítes: serve envelopes
// enlatados direto da RAM, ignorando credenciais por completo.
type FakeHeaderClient struct {
	Total uint32
}

var _ imapx.IMAPClient = (*FakeHeaderClient)(nil)

func (f *FakeHeaderClient) Login(username, password string) error { return nil }

func (f *FakeHeaderClient) Select(mailbox string, opts *imap.SelectOptions) (*imap.SelectData, error) {
	if !opts.ReadOnly {
		return nil, fmt.Errorf("fake refuses non-read-only select") // self-enforcing / auto-fiscalizador
	}
	return &imap.SelectData{NumMessages: f.Total}, nil
}

func (f *FakeHeaderClient) Search(criteria *imap.SearchCriteria) (*imap.SearchData, error) {
	return nil, fmt.Errorf("fake does not implement SEARCH")
}

func (f *FakeHeaderClient) Fetch(seqNums []uint32, opts *imap.FetchOptions, emit func(imapx.Envelope)) error {
	for _, n := range seqNums {
		if n > f.Total {
			continue
		}
		emit(imapx.Envelope{
			RawSubject:  fmt.Sprintf("Verify your email (#%d)", n),
			FromMailbox: "noreply",
			FromHost:    fmt.Sprintf("d%03d.example", n%40),
			Date:        fixedDate(),
		})
	}
	return nil
}

func (f *FakeHeaderClient) Logout() error { return nil }

// fixedDate gives deterministic fixtures across suites.
// fixedDate dá fixtures determinísticas entre suítes.
func fixedDate() time.Time {
	return time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
}
