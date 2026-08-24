package core

import (
	"strings"
)

// TransactionalPrefixes are leftmost DNS labels commonly used by bulk/
// transactional senders. When stripping from a host we never go below two
// labels, so "foo.co.uk" stays intact.
// TransactionalPrefixes são labels DNS à esquerda comuns em remetentes
// transacionais/massivos. Ao remover do host nunca descemos abaixo de dois
// labels, então "foo.co.uk" permanece intacto.
var TransactionalPrefixes = map[string]bool{
	"no-reply": true, "noreply": true, "donotreply": true,
	"mail": true, "email": true, "e": true, "smtp": true,
	"mkt": true, "marketing": true, "news": true, "newsletter": true,
	"billing": true, "payments": true, "payment": true, "invoice": true,
	"notifications": true, "notify": true, "alert": true, "alerts": true,
	"account": true, "accounts": true, "info": true, "msg": true,
	"message": true, "messages": true, "bounce": true, "bounces": true,
}

// DefaultIgnoreDomains are excluded from the final report so it stays clean.
// Extend at runtime with IGNORE_DOMAINS (comma separated).
// DefaultIgnoreDomains são excluídos do relatório final para mantê-lo limpo.
// Estenda em tempo de execução com IGNORE_DOMAINS (separado por vírgulas).
var DefaultIgnoreDomains = []string{
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

// NormalizeDomain reduces a mail host to its base service domain:
// "mail.booking.com" -> "booking.com" only while the leftmost label is a
// known transactional prefix; unknown subdomains are preserved because we
// never strip blindly below two labels ("foo.co.uk" stays "foo.co.uk").
//
// NormalizeDomain reduz um host de e-mail ao domínio base do serviço:
// "mail.booking.com" -> "booking.com" apenas enquanto o label mais à esquerda
// for um prefixo transacional conhecido; subdomínios desconhecidos são
// preservados porque nunca removemos às cegas abaixo de dois labels
// ("foo.co.uk" continua "foo.co.uk").
func NormalizeDomain(host string) string {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	labels := strings.Split(host, ".")
	for len(labels) > 2 && TransactionalPrefixes[labels[0]] {
		labels = labels[1:]
	}
	return strings.Join(labels, ".")
}
