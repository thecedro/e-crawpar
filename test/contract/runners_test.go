// Layer 3 — CONTRACT TEST RUNNERS.
// The same safety suite (AssertIMAPClientContract) runs against:
//
//   - the in-memory fake (fast, runs on every `go test`);
//   - the PRODUCTION ClientAdapter talking to an in-memory IMAP server over
//     real TCP — proving the real protocol path also honors the contract;
//
// plus a wire-level audit: the raw server transcript must contain ENVELOPE
// and never contain body-bearing FETCH items, and every message must still
// carry exactly its seeded \\Seen flag after a full scan.
//
// Camada 3 — EXECUTORES DOS TESTES DE CONTRATO.
// A mesma suíte de segurança roda contra o fake em memória (rápido, roda em
// todo `go test`) e contra o ClientAdapter de produção falando com um
// servidor IMAP em memória por TCP real. Há ainda uma auditoria no nível do
// protocolo: o transcript bruto deve conter ENVELOPE e jamais itens de corpo,
// e cada mensagem deve continuar com exatamente a flag \\Seen original após
// a varredura completa.

//go:build !external_imap

package contract_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"e-crawpar/internal/imapx"
	"e-crawpar/test/contract"
	"e-crawpar/test/harness"
)

func TestContractFakeHeaderClient(t *testing.T) {
	contract.AssertIMAPClientContract(t, func(t *testing.T) imapx.IMAPClient {
		return &contract.FakeHeaderClient{Total: 250}
	})
}

// transcriptBuffer is a concurrency-safe io.Writer fed by the server's
// DebugWriter (raw IMAP wire traffic).
type transcriptBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *transcriptBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func TestContractRealAdapterOverMemServer(t *testing.T) {
	msgs := make([]harness.Msg, 250)
	for i := range msgs {
		msgs[i] = harness.Msg{
			From:    "noreply@d001.example",
			Subject: "Verify your email",
			Date:    time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		}
	}

	var transcript transcriptBuffer
	handle, err := harness.StartServerManual(msgs, &transcript)
	require.NoError(t, err)

	contract.AssertIMAPClientContract(t, func(t *testing.T) imapx.IMAPClient {
		return harness.Dial(t, handle.Addr)
	})

	// --- mailbox untouched / caixa intocada (server still running) ---
	flags := harness.FetchAllFlags(t, handle.Addr)
	require.Len(t, flags, len(msgs))
	for i, f := range flags {
		assert.Equal(t, []imap.Flag{imap.FlagSeen}, f,
			"message %d flags changed during scan — read-only promise broken", i+1)
	}

	// Quiesce the server BEFORE auditing the transcript: no writer goroutine
	// may be flushing while we read the buffer.
	// Encerra o servidor ANTES de auditar o transcript: nenhuma goroutine
	// escritora pode estar gravando enquanto lemos o buffer.
	handle.Close()

	// --- wire-level audit / auditoria do protocolo ---
	wire := transcript.buf.String()
	assert.Contains(t, wire, "ENVELOPE", "the scan must ask for ENVELOPE items")
	for _, forbidden := range []string{"BODY[", "BODY.PEEK[", "RFC822.TEXT", "RFC822.HEADER", "BINARY["} {
		assert.NotContainsf(t, wire, forbidden,
			"CONTRACT VIOLATION on the wire: %q present in IMAP traffic", forbidden)
	}
}
