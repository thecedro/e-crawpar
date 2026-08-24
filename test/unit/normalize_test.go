// Layer 1 — UNIT TESTS: sender/domain normalization.
// Transactional prefix stripping (no-reply, mail, mkt, billing,
// notifications, ...), stacked prefixes, the two-label floor that protects
// ccTLDs (foo.co.uk) and malformed inputs.
//
// Camada 1 — TESTES DE UNIDADE: normalização de remetente/domínio.
// Remoção de prefixos transacionais (no-reply, mail, mkt, billing,
// notifications, ...), prefixos empilhados, o piso de dois labels que protege
// ccTLDs (foo.co.uk) e entradas malformadas.
package unit_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"e-crawpar/internal/core"
)

func TestNormalizeDomainTransactionalPrefixes(t *testing.T) {
	tests := []struct{ host, want string }{
		// every documented prefix family / cada família de prefixo documentada
		{"no-reply.ebay.de", "ebay.de"},
		{"noreply.ebay.de", "ebay.de"},
		{"donotreply.example.com", "example.com"},
		{"mail.booking.com", "booking.com"},
		{"email.example.com.br", "example.com.br"},
		{"e.example.org", "example.org"},
		{"smtp.fastmail.com", "fastmail.com"},
		{"mkt.store.com", "store.com"},
		{"marketing.store.com", "store.com"},
		{"news.zine.io", "zine.io"},
		{"newsletter.zine.io", "zine.io"},
		{"billing.stripe.com", "stripe.com"},
		{"payments.stripe.com", "stripe.com"},
		{"payment.stripe.com", "stripe.com"},
		{"invoice.corp.net", "corp.net"},
		{"notifications.github.com", "github.com"},
		{"notify.me.io", "me.io"},
		{"alert.bank.br", "bank.br"},
		{"alerts.bank.br", "bank.br"},
		{"account.service.io", "service.io"},
		{"accounts.service.io", "service.io"},
		{"info.host.com", "host.com"},
		{"msg.host.com", "host.com"},
		{"message.host.com", "host.com"},
		{"messages.host.com", "host.com"},
		{"bounce.mandrill.app", "mandrill.app"},
		{"bounces.sendgrid.net", "sendgrid.net"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, core.NormalizeDomain(tt.host), "host %q", tt.host)
	}
}

func TestNormalizeDomainStackedPrefixes(t *testing.T) {
	// All leading transactional labels are stripped in one pass.
	// Todos os labels transacionais à esquerda são removidos numa passada.
	tests := []struct{ host, want string }{
		{"mail.mail.foo.com", "foo.com"},
		{"no-reply.billing.example.org", "example.org"},
		{"mkt.news.shop.io", "shop.io"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, core.NormalizeDomain(tt.host), "host %q", tt.host)
	}
}

func TestNormalizeDomainPreservesUnknownSubdomains(t *testing.T) {
	tests := []struct{ host, want string }{
		// unknown subdomains survive / subdomínios desconhecidos sobrevivem
		{"random.sub.example.com", "random.sub.example.com"},
		{"mx1.corp.example", "mx1.corp.example"},
		// never below two labels / nunca abaixo de dois labels
		{"foo.co.uk", "foo.co.uk"},
		{"foo.com.br", "foo.com.br"},
		// strip stops exactly at the floor / a remoção para exatamente no piso
		{"mail.foo.co.uk", "foo.co.uk"},
		{"no-reply.gov.br", "gov.br"}, // prefix stripped down to the floor / remove até o piso
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, core.NormalizeDomain(tt.host), "host %q", tt.host)
	}
}

func TestNormalizeDomainTwoLabelHostsNeverStrip(t *testing.T) {
	// A leftmost label that looks transactional is part of the domain when
	// only two labels exist ("mail.com" IS a provider).
	// Um label à esquerda com cara transacional faz parte do domínio quando
	// só existem dois labels ("mail.com" É um provedor).
	assert.Equal(t, "mail.com", core.NormalizeDomain("mail.com"))
	assert.Equal(t, "news.tv", core.NormalizeDomain("news.tv"))
}

func TestNormalizeDomainCaseAndTrailingDot(t *testing.T) {
	assert.Equal(t, "amazon.com", core.NormalizeDomain("AMAZON.COM."))
	assert.Equal(t, "booking.com", core.NormalizeDomain("Mail.BOOKING.com."))
}

func TestNormalizeDomainDegenerateInputs(t *testing.T) {
	// Malformed hosts must not panic and must stay stable.
	// Hosts malformados não podem dar panic nem mudar de forma instável.
	tests := []struct{ host, want string }{
		{"", ""},
		{".", ""}, // only a dot: trims to empty / só um ponto: vira vazio
		{"localhost", "localhost"},
		{"a..b", "a..b"},
		{".com", ".com"},
		{"MAIL.", "mail"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, core.NormalizeDomain(tt.host), "host %q", tt.host)
	}
}

func TestNormalizeDomainKnownPrefixMap(t *testing.T) {
	for _, p := range []string{"no-reply", "mail", "mkt", "billing", "notifications"} {
		assert.True(t, core.TransactionalPrefixes[p], "prefix %q documented as transactional", p)
	}
	assert.False(t, core.TransactionalPrefixes["www"], "www must NOT be stripped")
}
