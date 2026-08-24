package main

import (
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
