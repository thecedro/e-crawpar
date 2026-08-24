package main

// ============================================================================
// USER-FACING ERROR TRANSLATION / TRADUÇÃO DE ERROS PARA O USUÁRIO
// Raw Go/IMAP/network errors are confusing for non-technical users. This
// layer converts them into short bilingual sentences with an actionable
// hint. It is pure and unit-testable: classifiers receive any error and
// return a *UserError.
//
// Erros brutos de Go/IMAP/rede confundem usuários não técnicos. Esta camada
// os converte em frases curtas bilíngues com uma dica acionável. É puro e
// testável por unidade: os classificadores recebem qualquer erro e retornam
// um *UserError.
// ============================================================================

import (
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
)

// UserError is a pre-translated, non-technical error.
// UserError é um erro pré-traduzido, não técnico.
type UserError struct {
	MsgPT  string // one line, Portuguese
	MsgEN  string // one line, English
	HintPT string // what to do about it, Portuguese
	HintEN string // what to do about it, English
}

// Error implements the error interface using the English line, so logs stay
// readable for the widest audience while stdout gets the full bilingual box.
//
// Error implementa a interface error usando a linha em inglês, para que logs
// fiquem legíveis para o maior público enquanto o stdout recebe o bloco
// bilíngue completo.
func (e *UserError) Error() string { return e.MsgEN }

// newErrf is a small constructor keeping call sites tidy.
// newErrf é um construtor enxuto mantendo os pontos de chamada limpos.
func newErrf(msgPT, msgEN, hintPT, hintEN string) *UserError {
	return &UserError{MsgPT: msgPT, MsgEN: msgEN, HintPT: hintPT, HintEN: hintEN}
}

// printFriendly renders a bilingual error box without any stack trace.
// printFriendly exibe um quadro de erro bilíngue sem nenhum stack trace.
func printFriendly(err error) {
	fmt.Fprintln(os.Stderr, "\n✗ Algo deu errado / Something went wrong")
	var ue *UserError
	if errors.As(err, &ue) {
		fmt.Fprintf(os.Stderr, "  %s\n  %s\n", ue.MsgPT, ue.MsgEN)
		fmt.Fprintf(os.Stderr, "→ Dica / Hint: %s\n", ue.HintPT)
		if ue.HintEN != "" {
			fmt.Fprintf(os.Stderr, "→ Hint / Dica: %s\n", ue.HintEN)
		}
		return
	}
	// Fallback for unexpected errors: still no stack trace, just the message.
	// Fallback para erros inesperados: ainda sem stack trace, só a mensagem.
	fmt.Fprintf(os.Stderr, "  %v\n", err)
}

// friendlyDialError translates TCP/TLS/DNS failures from DialTLS.
// friendlyDialError traduz falhas de TCP/TLS/DNS do DialTLS.
func friendlyDialError(err error, addr string) error {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && (dnsErr.IsNotFound || strings.Contains(strings.ToLower(err.Error()), "no such host")) {
		host, _, _ := net.SplitHostPort(addr)
		return newErrf(
			fmt.Sprintf("Servidor %q não encontrado.", host),
			fmt.Sprintf("Server %q not found.", host),
			"Confira o IMAP_HOST. Gmail=imap.gmail.com, Outlook=outlook.office365.com, Yahoo=imap.mail.yahoo.com.",
			"Check IMAP_HOST. Gmail=imap.gmail.com, Outlook=outlook.office365.com, Yahoo=imap.mail.yahoo.com.")
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "connection refused"):
		return newErrf(
			fmt.Sprintf("O servidor %s recusou a conexão.", addr),
			fmt.Sprintf("The server at %s refused the connection.", addr),
			"Provavelmente a porta está errada. A porta padrão segura é 993 (IMAP_PORT).",
			"The port is probably wrong. The safe default is 993 (IMAP_PORT).")
	case strings.Contains(msg, "i/o timeout"), strings.Contains(msg, "timed out"):
		return newErrf(
			fmt.Sprintf("Tempo esgotado ao conectar em %s.", addr),
			fmt.Sprintf("Timed out connecting to %s.", addr),
			"Pode ser internet instável, firewall ou host errado. Tente novamente.",
			"Could be unstable internet, a firewall or a wrong host. Try again.")
	case strings.Contains(msg, "tls:"), errors.As(err, new(*x509.HostnameError)),
		errors.As(err, new(*x509.CertificateInvalidError)), errors.As(err, new(x509.UnknownAuthorityError)):
		return newErrf(
			fmt.Sprintf("Falha na criptografia (TLS) com %s.", addr),
			fmt.Sprintf("TLS encryption handshake failed with %s.", addr),
			"Confira se IMAP_HOST está escrito corretamente e se sua rede não intercepta HTTPS (proxies corporativos).",
			"Make sure IMAP_HOST is spelled correctly and your network does not intercept TLS (corporate proxies).")
	default:
		return newErrf(
			fmt.Sprintf("Não foi possível conectar ao servidor %s.", addr),
			fmt.Sprintf("Could not connect to server %s.", addr),
			"Verifique internet, IMAP_HOST e IMAP_PORT, depois tente novamente.",
			"Check your internet, IMAP_HOST and IMAP_PORT, then try again.")
	}
}

// friendlyAuthError translates LOGIN rejections into app-password guidance.
// friendlyAuthError traduz rejeições de LOGIN em orientação sobre senha de app.
func friendlyAuthError(err error, user string) error {
	msg := strings.ToUpper(err.Error())
	switch {
	case strings.Contains(msg, "AUTHENTICATIONFAILED"),
		strings.Contains(msg, "INVALID-CREDENTIALS"),
		strings.Contains(msg, "LOGIN FAILED"),
		strings.Contains(msg, "[AUTH"):
		return newErrf(
			fmt.Sprintf("Login recusado para %s.", user),
			fmt.Sprintf("Login rejected for %s.", user),
			"Use uma SENHA DE APP (não a senha normal do e-mail) e confira o endereço. Veja o README para gerar a sua.",
			"Use an APP PASSWORD (not your regular email password) and double-check the address. See the README to generate yours.")
	case strings.Contains(msg, "LOGINDISABLED"), strings.Contains(msg, "PRIVACYREQUIRED"),
		strings.Contains(msg, "WEAKPASSWORD"), strings.Contains(msg, "AUTHORIZATIONFAILED"):
		return newErrf(
			"O provedor bloqueou este tipo de login.",
			"The provider blocked this kind of login.",
			"Ative a verificação em duas etapas na conta e gere uma senha de aplicativo. Alguns provedores também exigem permitir 'apps menos seguros' ou OAuth.",
			"Enable two-step verification on the account and create an app password. Some providers also require allowing 'less secure apps' or OAuth.")
	default:
		return newErrf(
			fmt.Sprintf("Não foi possível entrar na conta %s.", user),
			fmt.Sprintf("Could not sign in to account %s.", user),
			"Tente novamente; se persistir, gere uma nova senha de aplicativo.",
			"Try again; if it persists, generate a fresh app password.")
	}
}

// friendlySelectError translates EXAMINE failures (bad mailbox name).
// friendlySelectError traduz falhas de EXAMINE (nome de caixa inválido).
func friendlySelectError(err error, mailbox string) error {
	return newErrf(
		fmt.Sprintf("A caixa %q não existe ou não pode ser aberta.", mailbox),
		fmt.Sprintf("The mailbox %q does not exist or cannot be opened.", mailbox),
		"O padrão é INBOX. Só mude IMAP_MAILBOX se você souber o nome exato da pasta no seu servidor.",
		"The default is INBOX. Only change IMAP_MAILBOX if you know the exact folder name on your server.")
}
