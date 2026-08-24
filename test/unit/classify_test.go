// Layer 1 — UNIT TESTS.
// Pure business rules, one function at a time, no I/O and no IMAP: the
// subject classifier (PT+EN patterns, priority between categories), the
// RFC 2047 subject decoder and the curated-sample ranking. Ambiguity cases
// here lock the documented precedence security > verification > welcome >
// receipt > policy.
//
// Camada 1 — TESTES DE UNIDADE.
// Regras de negócio puras, uma função por vez, sem I/O e sem IMAP: o
// classificador de assuntos (padrões PT+EN, prioridade entre categorias),
// o decodificador RFC 2047 e o ranque de amostra curada. Os casos de
// ambiguidade travam a precedência documentada segurança > verificação >
// boas-vindas > recibo > política.
package unit_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"e-crawpar/internal/core"
)

// buildCats is the single compiled category set shared by unit suites.
// buildCats é o conjunto compilado de categorias compartilhado pelas suítes.
func buildCats() []core.Category { return core.BuildCategories() }

func TestClassifyEnglishSubjects(t *testing.T) {
	cats := buildCats()
	tests := []struct {
		subject string
		want    string
	}{
		{"New login from Chrome on Windows", "security"},
		{"New device signed in", "security"},
		{"Your password was changed successfully", "security"},
		{"Password changed", "security"},
		{"Verify your email to finish signing up", "verification"},
		{"Confirm your email address", "verification"},
		{"Confirm your account now", "verification"},
		{"Welcome to Acme, thanks for joining!", "welcome"},
		{"Privacy Policy Update — please review", "policy"},
		{"Terms of Service updated", "policy"},
		{"Terms of use update effective today", "policy"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, core.Classify(tt.subject, cats), "subject %q", tt.subject)
	}
}

func TestClassifyPortugueseSubjects(t *testing.T) {
	cats := buildCats()
	tests := []struct {
		subject string
		want    string
	}{
		{"Senha alterada com sucesso", "security"},
		{"Segurança atualizada no seu perfil", "security"}, // accented form / forma acentuada
		{"Seguranca atualizada agora", "security"},         // unaccented variant / variante sem acento
		{"Código de verificação: 123456", "verification"},
		{"Codigo de verificacao da conta", "verification"},
		{"Confirme seu e-mail em 24h", "verification"},
		{"Confirme seu email", "verification"}, // hyphen optional / hífen opcional
		{"Ative sua conta clicando no botão", "verification"},
		{"Bem-vindo ao Spotify", "welcome"},
		{"Bem-vindo ao clube", "welcome"},
		{"Obrigado por se registrar", "welcome"},
		{"Sua conta foi criada com sucesso", "welcome"},
		{"Sua fatura está disponível", "receipt"},
		{"Sua fatura disponível para pagamento", "receipt"}, // without "está" / sem "está"
		{"Pagamento aprovado para o pedido #99", "receipt"},
		{"Pedido recebido #1234", "receipt"},
		{"Recibo de compra", "receipt"},
		{"Termos de uso atualizados", "policy"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, core.Classify(tt.subject, cats), "subject %q", tt.subject)
	}
}

func TestClassifyNoMatch(t *testing.T) {
	cats := buildCats()
	for _, s := range []string{
		"Lunch tomorrow?",
		"Random newsletter content",
		"Promoção semanal da loja",
		"", // empty subject matches nothing / assunto vazio não casa
	} {
		assert.Equal(t, "", core.Classify(s, cats), "subject %q", s)
	}
}

func TestClassifyAmbiguityPriority(t *testing.T) {
	// Subjects that match more than one category must resolve by priority:
	// security > verification > welcome > receipt > policy.
	// Assuntos que casam com mais de uma categoria resolvem por prioridade:
	// segurança > verificação > boas-vindas > recibo > política.
	cats := buildCats()
	tests := []struct {
		subject string
		want    string
	}{
		{"New device detected — confirm your account", "security"},   // security beats verification
		{"New login from Chrome — welcome to Acme", "security"},      // security beats welcome
		{"Verify your email — welcome to Acme", "verification"},      // verification beats welcome
		{"Welcome to Acme — your receipt is ready", "welcome"},       // welcome beats receipt
		{"Recibo enviado após termos de uso atualizados", "receipt"}, // receipt beats policy
		// chained triple-match keeps the strongest signal / cadeia tripla mantém o sinal mais forte
		{"New login from Chrome — welcome to Acme — privacy policy update", "security"},
		{"Password changed; verify your email; terms of service updated", "security"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, core.Classify(tt.subject, cats), "subject %q", tt.subject)
	}
}

func TestClassifyCaseInsensitive(t *testing.T) {
	cats := buildCats()
	assert.Equal(t, "security", core.Classify("NEW LOGIN FROM FIREFOX", cats))
	assert.Equal(t, "welcome", core.Classify("WELCOME TO OUR SERVICE", cats))
	assert.Equal(t, "policy", core.Classify("TERMOS DE USO ATUALIZADOS", cats))
}

func TestBuildCategoriesPriorityOrder(t *testing.T) {
	cats := buildCats()
	names := make([]string, 0, len(cats))
	for _, c := range cats {
		names = append(names, c.Name)
		for _, re := range c.Patterns {
			assert.NotNil(t, re, "category %s has compiled patterns", c.Name)
		}
	}
	assert.Equal(t, []string{"security", "verification", "welcome", "receipt", "policy"}, names,
		"evaluation order is the documented priority")
}

func TestSampleRankPreference(t *testing.T) {
	rankV, okV := core.SampleRank("verification")
	rankW, okW := core.SampleRank("welcome")
	assert.True(t, okV && okW, "verification and welcome are curated samples")
	assert.Less(t, rankV, rankW, "verification outranks welcome")

	for _, c := range []string{"security", "receipt", "policy", "", "unknown"} {
		_, ok := core.SampleRank(c)
		assert.False(t, ok, "%q must not be a curated sample", c)
	}
}

func TestDefaultIgnoreDomainsSanity(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range core.DefaultIgnoreDomains {
		assert.False(t, seen[d], "duplicate ignore domain %q", d)
		seen[d] = true
		assert.NotEmpty(t, d)
		assert.False(t, core.TransactionalPrefixes[d], "ignore list holds full domains, not prefixes")
	}
	// A few entries each class depends on / entradas das quais cada classe depende:
	for _, want := range []string{"netflix.com", "google.com", "nubank.com.br", "sentry.io"} {
		assert.Contains(t, seen, want)
	}
}
