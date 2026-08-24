package main

// ============================================================================
// FIRST-RUN INTERACTIVE SETUP / ASSISTENTE INTERATIVO DE PRIMEIRA EXECUÇÃO
// When required credentials are missing and stdin is a terminal, the tool
// walks the user through provider choice, e-mail and app password, validates
// the IMAP connection BEFORE saving anything, then writes a private .env so
// the next run needs no questions.
//
// Quando faltam credenciais obrigatórias e o stdin é um terminal, a
// ferramenta guia o usuário pela escolha do provedor, e-mail e senha de app,
// valida a conexão IMAP ANTES de salvar qualquer coisa e grava um .env
// privado para que a próxima execução não pergunte nada.
// ============================================================================

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"e-crawpar/internal/apperr"
	"e-crawpar/internal/imapx"
)

const envFile = ".env"

// missingEnvError reports which mandatory variables are absent.
// missingEnvError informa quais variáveis obrigatórias estão ausentes.
type missingEnvError struct{ Keys []string }

func (e *missingEnvError) Error() string {
	return "missing required env vars: " + strings.Join(e.Keys, ", ")
}

// provider describes a well-known mailbox provider for the menu.
// provider descreve um provedor conhecido para o menu.
type provider struct {
	Label string // shown to the user
	Host  string // IMAP host; empty means "ask"
	Port  string
}

var providers = []provider{
	{Label: "Gmail / Google Workspace", Host: "imap.gmail.com", Port: "993"},
	{Label: "Outlook / Hotmail / Microsoft 365", Host: "outlook.office365.com", Port: "993"},
	{Label: "Yahoo Mail", Host: "imap.mail.yahoo.com", Port: "993"},
	{Label: "Outro / Other (digite o servidor)", Host: "", Port: "993"},
}

// --- .env handling / manuseio do .env ---

// parseDotEnv parses KEY=VALUE lines, ignoring blanks, comments and optional
// surrounding quotes. No external dotenv dependency needed.
//
// parseDotEnv interpreta linhas CHAVE=VALOR, ignorando vazias, comentários e
// aspas opcionais. Sem dependência externa de dotenv.
func parseDotEnv(data string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		if key != "" {
			out[key] = value
		}
	}
	return out
}

// readDotEnv returns parsed contents of path, or an empty map when missing.
// readDotEnv retorna o conteúdo parseado do caminho, ou mapa vazio se ausente.
func readDotEnv(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	return parseDotEnv(string(data))
}

// renderDotEnv produces the file body with values safely quoted.
// renderDotEnv produz o corpo do arquivo com valores devidamente entre aspas.
func renderDotEnv(values map[string]string) string {
	keys := []string{"IMAP_HOST", "IMAP_PORT", "IMAP_USER", "IMAP_APP_PASSWORD"}
	var b strings.Builder
	b.WriteString("# e-crawpar credentials - keep this file PRIVATE!\n")
	b.WriteString("# Credenciais do e-crawpar - mantenha este arquivo PRIVADO!\n")
	for _, k := range keys {
		if v := values[k]; v != "" {
			_, _ = fmt.Fprintf(&b, "%s=%q\n", k, v)
		}
	}
	return b.String()
}

// writeDotEnv saves credentials with owner-only permissions (0600).
// writeDotEnv salva credenciais com permissão apenas para o dono (0600).
func writeDotEnv(path string, values map[string]string) error {
	return os.WriteFile(path, []byte(renderDotEnv(values)), 0o600)
}

// --- prompts / entradas do usuário ---

// promptLine reads one trimmed non-empty answer from r.
// promptLine lê uma resposta aparada e não vazia de r.
func promptLine(r *bufio.Reader, w interface{ Write([]byte) (int, error) }, label string) string {
	for {
		_, _ = fmt.Fprint(w, label+" ")
		line, _ := r.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
		_, _ = fmt.Fprintln(w, "  Valor vazio, tente de novo. / Empty value, try again.")
	}
}

// promptPassword reads a secret with echo disabled when possible.
// promptPassword lê um segredo com eco desativado quando possível.
func promptPassword(r *bufio.Reader, w interface{ Write([]byte) (int, error) }, label string) string {
	_, _ = fmt.Fprint(w, label+" ")
	if term.IsTerminal(int(os.Stdin.Fd())) {
		secret, err := term.ReadPassword(int(os.Stdin.Fd()))
		_, _ = fmt.Fprintln(w) // restore newline killed by raw mode / restaura a quebra de linha
		if err == nil && len(secret) > 0 {
			return strings.TrimSpace(string(secret))
		}
	}
	// Piped input (tests/scripts): plain read is the only option.
	// Entrada por pipe (testes/scripts): leitura simples é a única opção.
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}

// probeAccount validates that host+credentials actually work over TLS,
// touching nothing on the server (read-only select).
//
// probeAccount valida que host+credenciais funcionam via TLS, sem tocar em
// nada no servidor (select somente-leitura).
func probeAccount(host, port, user, pass string) error {
	client, err := imapx.DialTLS(host, port)
	if err != nil {
		return err
	}
	defer func() { _ = client.Logout() }()
	if err := client.Login(user, pass); err != nil {
		return apperr.FriendlyAuthError(err, user)
	}
	if _, err := client.Select("INBOX", imapx.ReadOnlySelect); err != nil {
		return apperr.FriendlySelectError(err, "INBOX")
	}
	return nil
}

// probeAccountFn is an indirection so tests can stub the network probe.
// probeAccountFn é uma indireção para os testes simularem a validação.
var probeAccountFn = probeAccount

// runSetup drives the interactive wizard until a working credential set is
// produced or the user exhausts three validation attempts.
//
// runSetup conduz o assistente interativo até produzir credenciais válidas ou
// até esgotar três tentativas de validação.
func runSetup(stdin *bufio.Reader, stdout interface{ Write([]byte) (int, error) }) (map[string]string, error) {
	_, _ = fmt.Fprintln(stdout, "\n== Bem-vindo ao e-crawpar! Vamos configurar sua conta uma única vez. ==")
	_, _ = fmt.Fprintln(stdout, "== Welcome to e-crawpar! Let's set up your account just once. ==")
	_, _ = fmt.Fprintln(stdout, "\nDe onde é seu e-mail? / Which provider is your e-mail from?")
	for i, p := range providers {
		_, _ = fmt.Fprintf(stdout, "  %d) %s\n", i+1, p.Label)
	}
	choice := promptLine(stdin, stdout, fmt.Sprintf("Escolha 1-%d / Pick 1-%d:", len(providers), len(providers)))
	idx := -1
	for _, c := range choice {
		if c >= '1' && c <= rune('0'+len(providers)) {
			idx = int(c - '1')
			break
		}
	}
	if idx < 0 {
		return nil, apperr.NewErrf("Opção inválida.", "Invalid option.",
			"Execute novamente e digite o número da opção.", "Run again and type the option number.")
	}
	p := providers[idx]
	host := p.Host
	if host == "" {
		host = promptLine(stdin, stdout, "Servidor IMAP (ex.: imap.provedor.com):")
	}

	email := promptLine(stdin, stdout, "Seu e-mail completo:")
	pass := promptPassword(stdin, stdout, "Senha de APP (cole aqui; não aparece enquanto digita):")

	values := map[string]string{"IMAP_HOST": host, "IMAP_PORT": p.Port, "IMAP_USER": email}

	_, _ = fmt.Fprintln(stdout, "\nTestando a conexão... / Testing the connection...")
	for attempt := 1; attempt <= 3; attempt++ {
		err := probeAccountFn(host, p.Port, email, pass)
		if err == nil {
			values["IMAP_APP_PASSWORD"] = pass
			if werr := writeDotEnv(envFile, values); werr != nil {
				return nil, apperr.NewErrf(
					fmt.Sprintf("Conectei, mas não consegui salvar o %s.", envFile),
					fmt.Sprintf("Connected, but could not save %s.", envFile),
					fmt.Sprintf("Verifique permissões de escrita na pasta atual (%v).", werr),
					fmt.Sprintf("Check write permissions in the current folder (%v).", werr))
			}
			_, _ = fmt.Fprintf(stdout, "\n✓ Conectado! Credenciais salvas em %q.\n", envFile)
			_, _ = fmt.Fprintf(stdout, "✓ Connected! Credentials saved to %q.\n", envFile)
			_, _ = fmt.Fprintln(stdout, "  O arquivo é privado (permissão 600) e está no .gitignore.")
			_, _ = fmt.Fprintln(stdout, "  The file is private (permission 600) and git-ignored.")
			return values, nil
		}
		apperr.PrintFriendly(err)
		if attempt == 3 {
			break
		}
		_, _ = fmt.Fprintln(stdout, "\nVamos tentar de novo. / Let's try again.")
		email = promptLine(stdin, stdout, "E-mail completo:")
		pass = promptPassword(stdin, stdout, "Senha de APP:")
		values["IMAP_USER"] = email
	}
	return nil, apperr.NewErrf(
		"Não conseguimos validar a conexão após 3 tentativas.",
		"We could not validate the connection after 3 attempts.",
		"Gere uma NOVA senha de app seguindo o README e execute novamente. Nada foi salvo.",
		"Generate a NEW app password following the README and run again. Nothing was saved.")
}

// bootstrap loads .env + OS environment into a Config. When mandatory vars
// are missing it either runs the interactive wizard or explains how to
// configure manually.
//
// bootstrap carrega .env + ambiente do SO numa Config. Faltando variáveis
// obrigatórias, roda o assistente interativo ou explica como configurar à mão.
func bootstrap() (*Config, error) {
	fileVars := readDotEnv(envFile)
	cfg, err := loadConfig(fileVars)
	if err == nil {
		return cfg, nil
	}
	var miss *missingEnvError
	if !errors.As(err, &miss) {
		return nil, err
	}
	if !isInteractive() {
		return nil, apperr.NewErrf(
			"Faltam credenciais e não há terminal interativo nesta sessão.",
			"Credentials are missing and there is no interactive terminal in this session.",
			"Crie um arquivo "+envFile+" com IMAP_HOST, IMAP_PORT, IMAP_USER e IMAP_APP_PASSWORD (veja o README), ou execute o programa num terminal normal para o modo guiado.",
			"Create a "+envFile+" file with IMAP_HOST, IMAP_PORT, IMAP_USER and IMAP_APP_PASSWORD (see the README), or run the program in a normal terminal for the guided mode.")
	}
	values, err := runSetup(bufio.NewReader(os.Stdin), os.Stdout)
	if err != nil {
		return nil, err
	}
	// Re-merge: saved file plus any extra OS overrides still apply.
	// Re-mescla: arquivo salvo mais eventuais overrides do SO ainda valem.
	for k := range values {
		delete(fileVars, k) // fresh file wins over stale entries / arquivo novo vence
	}
	for k, v := range values {
		fileVars[k] = v
	}
	return loadConfig(fileVars)
}

func isInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}
