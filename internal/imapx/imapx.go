// Package imapx isolates every IMAP conversation of e-crawpar behind one
// narrow interface, IMAPClient, so the collection stage can be exercised by
// fakes/mocks and audited by contract tests. The production implementation,
// ClientAdapter, wraps github.com/emersion/go-imap/v2/imapclient and is
// strictly READ-ONLY: it selects mailboxes EXAMINE-style and requests only
// ENVELOPE data — message bodies are never fetched.
//
// O pacote imapx isola toda a conversa IMAP do e-crawpar atrás de uma
// interface estreita, IMAPClient, para que o estágio de coleta possa ser
// exercitado por fakes/mocks e auditado por testes de contrato. A
// implementação de produção, ClientAdapter, envolve
// github.com/emersion/go-imap/v2/imapclient e é estritamente SOMENTE-LEITURA:
// seleciona caixas em modo EXAMINE e pede apenas ENVELOPE — o corpo das
// mensagens nunca é buscado.
package imapx

import (
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"e-crawpar/internal/apperr"
	"e-crawpar/internal/core"
)

// Envelope is the minimal header data the pipeline needs from one message.
// The Subject is kept RAW (possibly RFC 2047 encoded); decoding happens in
// CollectHeaders so fakes stay trivial and honest.
//
// Envelope é o mínimo de headers que o pipeline precisa de uma mensagem. O
// Subject fica BRUTO (possivelmente codificado em RFC 2047); a decodificação
// acontece em CollectHeaders para que os fakes permaneçam simples e honestos.
type Envelope struct {
	RawSubject  string    // Subject header as sent by the server / como veio do servidor
	FromMailbox string    // local part of first From address / parte local do primeiro From
	FromHost    string    // domain part of first From address / domínio do primeiro From
	Date        time.Time // envelope Date header
}

// IMAPClient is the only surface through which e-crawpar talks IMAP. Any
// implementation MUST satisfy the safety contract verified by
// /test/contract: read-only Select, ENVELOPE-only fetches and bounded batch
// pagination.
//
// IMAPClient é a única superfície pela qual o e-crawpar fala IMAP. Qualquer
// implementação DEVE satisfazer o contrato de segurança verificado em
// /test/contract: Select somente-leitura, fetch apenas-ENVELOPE e paginação
// em lotes limitados.
type IMAPClient interface {
	Login(username, password string) error
	Select(mailbox string, opts *imap.SelectOptions) (*imap.SelectData, error)
	Search(criteria *imap.SearchCriteria) (*imap.SearchData, error)
	// Fetch issues one FETCH command for exactly the given sequence numbers
	// and calls emit once per usable message (one with a parsable From).
	// Fetch emite um comando FETCH exatamente para os sequence numbers dados
	// e chama emit uma vez por mensagem útil (com From parseável).
	Fetch(seqNums []uint32, opts *imap.FetchOptions, emit func(Envelope)) error
	Logout() error
}

// ReadOnlySelect is the only select mode this tool ever uses: the mailbox is
// opened EXAMINE-style, so no flag (including \Seen) can change.
//
// ReadOnlySelect é o único modo de seleção que a ferramenta usa: a caixa é
// aberta em estilo EXAMINE, então nenhuma flag (inclusive \Seen) muda.
var ReadOnlySelect = &imap.SelectOptions{ReadOnly: true}

// EnvelopeOnly is the only fetch item this tool ever requests: just the
// envelope (From/Subject/Date). No BODY, no BODYSTRUCTURE, no flags.
//
// EnvelopeOnly é o único item de fetch que a ferramenta pede: apenas o
// envelope (From/Subject/Date). Nada de BODY, BODYSTRUCTURE ou flags.
func EnvelopeOnly() *imap.FetchOptions {
	return &imap.FetchOptions{Envelope: true}
}

// Options carries everything CollectHeaders needs besides the connection.
// Options carrega tudo que CollectHeaders precisa além da conexão.
type Options struct {
	User      string
	Password  string
	Mailbox   string
	Since     *time.Time // optional lower bound for server-side SEARCH / limite opcional
	BatchSize int        // messages per FETCH command / mensagens por comando FETCH
}

// ClientAdapter adapts the streaming go-imap v2 client to IMAPClient.
// ClientAdapter adapta o cliente streaming go-imap v2 à interface IMAPClient.
type ClientAdapter struct {
	C *imapclient.Client
}

var _ IMAPClient = ClientAdapter{}

// NewAdapter wraps an already-connected client (useful for tests that dial
// with DialInsecure against an in-memory server).
//
// NewAdapter envolve um cliente já conectado (útil em testes que usam
// DialInsecure contra um servidor em memória).
func NewAdapter(c *imapclient.Client) *ClientAdapter { return &ClientAdapter{C: c} }

// DialTLS opens the TLS session with sane timeouts. TLS is mandatory in
// production: credentials travel inside this session.
//
// DialTLS abre a sessão TLS com timeouts razoáveis. TLS é obrigatório em
// produção: as credenciais viajam dentro desta sessão.
func DialTLS(host, port string) (*ClientAdapter, error) {
	c, err := imapclient.DialTLS(host+":"+port, &imapclient.Options{
		TLSConfig: &tls.Config{ServerName: host},
		Dialer:    &net.Dialer{Timeout: 30 * time.Second},
	})
	if err != nil {
		return nil, err
	}
	return &ClientAdapter{C: c}, nil
}

// Login implements IMAPClient.
func (a ClientAdapter) Login(username, password string) error {
	return a.C.Login(username, password).Wait()
}

// Select implements IMAPClient. Callers are expected to pass ReadOnlySelect;
// the adapter never mutates it.
func (a ClientAdapter) Select(mailbox string, opts *imap.SelectOptions) (*imap.SelectData, error) {
	sel, err := a.C.Select(mailbox, opts).Wait()
	if err != nil {
		return nil, err
	}
	return sel, nil
}

// Search implements IMAPClient.
func (a ClientAdapter) Search(criteria *imap.SearchCriteria) (*imap.SearchData, error) {
	return a.C.Search(criteria, nil).Wait()
}

// Fetch implements IMAPClient by translating the streaming response into
// per-message emit callbacks.
func (a ClientAdapter) Fetch(seqNums []uint32, opts *imap.FetchOptions, emit func(Envelope)) error {
	cmd := a.C.Fetch(imap.SeqSetNum(seqNums...), opts)
	for {
		msg := cmd.Next() // nil when this command is exhausted / esgotado
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
			emit(Envelope{
				RawSubject:  env.Subject,
				FromMailbox: from.Mailbox,
				FromHost:    from.Host,
				Date:        env.Date,
			})
			break // one envelope per message / um envelope por mensagem
		}
	}
	return cmd.Close()
}

// Logout implements IMAPClient (best-effort at call sites; never masks real
// errors because callers ignore its result on deferred paths).
func (a ClientAdapter) Logout() error {
	_ = a.C.Logout().Wait()
	return nil
}

// CollectHeaders runs the whole collection stage over any IMAPClient: login,
// read-only select, list target sequence numbers and fetch envelopes batch
// by batch, pushing one decoded Job per usable message into jobs. It closes
// nothing and always logs out before returning.
//
// CollectHeaders executa todo o estágio de coleta sobre qualquer IMAPClient:
// login, select somente-leitura, listagem dos sequence numbers alvo e busca
// dos envelopes lote a lote, empurrando um Job decodificado por mensagem
// útil em jobs. Não fecha nada e sempre faz logout antes de retornar.
func CollectHeaders(client IMAPClient, opt Options, jobs chan<- core.Job) error {
	defer func() { _ = client.Logout() }() // best-effort, never masks real errors

	if err := client.Login(opt.User, opt.Password); err != nil {
		return apperr.FriendlyAuthError(err, opt.User)
	}

	// Read-only select (EXAMINE): guarantees no \Seen flag is ever touched.
	// Select somente-leitura (EXAMINE): garante que nenhuma flag \Seen seja tocada.
	sel, err := client.Select(opt.Mailbox, ReadOnlySelect)
	if err != nil {
		return apperr.FriendlySelectError(err, opt.Mailbox)
	}
	total := sel.NumMessages

	// Build the ordered list of message sequence numbers to scan:
	// - Since unset -> every message in the mailbox;
	// - Since set   -> server-side SEARCH SINCE (internal date).
	// Monta a lista ordenada de sequence numbers a varrer:
	// - sem Since -> todas as mensagens da caixa;
	// - com Since -> SEARCH SINCE no servidor (data interna).
	var seqNums []uint32
	if opt.Since == nil {
		seqNums = make([]uint32, 0, total)
		for n := uint32(1); n <= total; n++ {
			seqNums = append(seqNums, n)
		}
	} else {
		searchData, err := client.Search(&imap.SearchCriteria{Since: *opt.Since})
		if err != nil {
			return fmt.Errorf("search since %s: %w", opt.Since.Format(time.RFC3339), err)
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
	// BatchSize messages, keeping memory flat on huge mailboxes.
	// Busca de ENVELOPE em lotes. Cada ida ao servidor pede os headers de no
	// máximo BatchSize mensagens, mantendo a memória estável.
	fetchOpts := EnvelopeOnly()
	for start := 0; start < len(seqNums); start += opt.BatchSize {
		end := min(start+opt.BatchSize, len(seqNums))
		batch := seqNums[start:end]

		err := client.Fetch(batch, fetchOpts, func(e Envelope) {
			host := strings.ToLower(strings.TrimSuffix(e.FromHost, "."))
			if host == "" {
				return // defense in depth; adapters already skip these / defesa extra
			}
			jobs <- core.Job{
				Subject: core.DecodeSubject(e.RawSubject),
				From:    e.FromMailbox + "@" + host,
				Host:    host,
				Date:    e.Date,
			}
		})
		if err != nil {
			return fmt.Errorf("fetch batch [%d..%d): %w", start, end, err)
		}
	}
	return nil
}
