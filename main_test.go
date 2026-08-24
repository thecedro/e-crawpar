package main

// Root-package tests cover the pieces that stay unexported on purpose:
// environment/config loading, .env round-trip and the interactive first-run
// wizard. Business logic tests live in /test/unit against internal packages.
//
// Os testes do pacote raiz cobrem o que fica não-exportado de propósito:
// carregamento de ambiente/config, round-trip do .env e o assistente
// interativo da primeira execução. Testes de lógica de negócio ficam em
// /test/unit contra os pacotes internos.

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"e-crawpar/internal/apperr"
)

// TestDotEnvRoundTrip covers parsing quirks and the quoted write-back.
// TestDotEnvRoundTrip cobre peculiaridades de parse e a escrita entre aspas.
func TestDotEnvRoundTrip(t *testing.T) {
	parsed := parseDotEnv("# comment\n\nIMAP_HOST=imap.gmail.com\n  IMAP_PORT = 993 \n" +
		"IMAP_USER=\"user@gmail.com\"\nIMAP_APP_PASSWORD='abcd efgh ijkl mnop'\nbroken line")
	want := map[string]string{
		"IMAP_HOST":         "imap.gmail.com",
		"IMAP_PORT":         "993",
		"IMAP_USER":         "user@gmail.com",
		"IMAP_APP_PASSWORD": "abcd efgh ijkl mnop",
	}
	for k, v := range want {
		if parsed[k] != v {
			t.Errorf("parse %q = %q, want %q", k, parsed[k], v)
		}
	}
	if _, ok := parsed["broken"]; ok {
		t.Error("line without '=' must be ignored")
	}

	path := filepath.Join(t.TempDir(), ".env")
	values := map[string]string{
		"IMAP_HOST": "imap.test", "IMAP_PORT": "993", "IMAP_USER": "a@b.c",
		"IMAP_APP_PASSWORD": "pass with spaces",
	}
	if err := writeDotEnv(path, values); err != nil {
		t.Fatal(err)
	}
	back := readDotEnv(path)
	for k, v := range values {
		if back[k] != v { // spaces survive quoting / espaços sobrevivem às aspas
			t.Errorf("roundtrip %q = %q, want %q", k, back[k], v)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf(".env permission = %v, want -rw-------", info.Mode().Perm())
	}
}

// TestRunSetupWizard drives the interactive flow through piped stdin,
// including the retry loop, ending when valid credentials are accepted.
//
// TestRunSetupWizard conduz o fluxo interativo via stdin canalizado,
// incluindo o loop de repetição, terminando quando credenciais válidas
// são aceitas.
func TestRunSetupWizard(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(oldWd) })
	os.Chdir(dir) // wizard writes .env to cwd; keep test sandboxed

	// Stub the network probe: fail twice, then succeed. No real IMAP traffic.
	// Simula a validação: falha duas vezes, depois funciona. Sem tráfego real.
	attempts := 0
	origProbe := probeAccountFn
	probeAccountFn = func(host, port, user, pass string) error {
		attempts++
		if attempts < 3 {
			return apperr.FriendlyAuthError(errors.New("AUTHENTICATIONFAILED"), user)
		}
		if host != "imap.gmail.com" || port != "993" || user != "me@gmail.com" || pass != "goodpass" {
			t.Errorf("probe got unexpected args: %s %s %s %s", host, port, user, pass)
		}
		return nil
	}
	t.Cleanup(func() { probeAccountFn = origProbe })

	// provider=1 (Gmail), email, bad password x2, then good one.
	// provedor=1 (Gmail), e-mail, senha ruim x2, depois a boa.
	in := strings.NewReader("1\nme@gmail.com\nwrongpass\nme@gmail.com\nwrongpass2\nme@gmail.com\ngoodpass\n")
	var out bytes.Buffer
	_, err := runSetup(bufio.NewReader(in), &out)
	logs := out.String()
	if err != nil {
		t.Fatalf("wizard failed: %v\noutput:\n%s", err, logs)
	}
	if !strings.Contains(logs, "Testing the connection") {
		t.Errorf("wizard should validate before saving:\n%s", logs)
	}
	saved := readDotEnv(filepath.Join(dir, envFile))
	if saved["IMAP_HOST"] != "imap.gmail.com" || saved["IMAP_APP_PASSWORD"] != "goodpass" {
		t.Errorf("unexpected saved values: %+v", saved)
	}
}

// TestBootstrapNonInteractive ensures missing credentials without a TTY
// produce guidance instead of a crash.
//
// TestBootstrapNonInteractive garante que credenciais ausentes sem TTY gerem
// orientação em vez de crash.
func TestBootstrapNonInteractive(t *testing.T) {
	t.Setenv("IMAP_HOST", "")
	t.Setenv("IMAP_USER", "")
	t.Setenv("IMAP_APP_PASSWORD", "")
	cfg, err := bootstrap()
	if cfg != nil {
		t.Fatal("expected nil config")
	}
	var ue *apperr.UserError
	if !errors.As(err, &ue) {
		t.Fatalf("expected UserError, got %v", err)
	}
	if !strings.Contains(ue.HintEN, ".env") {
		t.Errorf("hint should explain manual .env setup: %q", ue.HintEN)
	}
}

// TestLoadConfigMissingEnv checks the structured missing-vars error.
// TestLoadConfigMissingEnv verifica o erro estruturado de variáveis ausentes.
func TestLoadConfigMissingEnv(t *testing.T) {
	_, err := loadConfig(map[string]string{"IMAP_HOST": "h"})
	var miss *missingEnvError
	if !errors.As(err, &miss) {
		t.Fatalf("expected missingEnvError, got %v", err)
	}
	want := []string{"IMAP_USER", "IMAP_APP_PASSWORD"}
	if len(miss.Keys) != len(want) {
		t.Fatalf("missing keys = %v, want %v", miss.Keys, want)
	}
	for i, k := range want {
		if miss.Keys[i] != k {
			t.Errorf("keys[%d] = %q, want %q", i, miss.Keys[i], k)
		}
	}
}

// TestLoadConfigValidation covers numeric parsing and noise-domain merging.
// TestLoadConfigValidation cobre parsing numérico e mesclagem de domínios de ruído.
func TestLoadConfigValidation(t *testing.T) {
	t.Setenv("IMAP_HOST", "h")
	t.Setenv("IMAP_USER", "u")
	t.Setenv("IMAP_APP_PASSWORD", "p")

	t.Setenv("WORKERS", "0")
	if _, err := loadConfig(nil); err == nil {
		t.Error("WORKERS=0 must be rejected")
	}
	t.Setenv("WORKERS", "4")
	t.Setenv("BATCH_SIZE", "nope")
	if _, err := loadConfig(nil); err == nil || !strings.Contains(err.Error(), "BATCH_SIZE") {
		t.Errorf("invalid BATCH_SIZE must name the var, got %v", err)
	}
	t.Setenv("BATCH_SIZE", "50")
	t.Setenv("IMAP_SINCE", "not-a-date")
	if _, err := loadConfig(nil); err == nil || !strings.Contains(err.Error(), "RFC3339") {
		t.Errorf("invalid IMAP_SINCE must mention RFC3339, got %v", err)
	}
	t.Setenv("IMAP_SINCE", "2024-01-01T00:00:00Z")
	t.Setenv("IGNORE_DOMAINS", " Spam.IO , , foo.bar ")
	cfg, err := loadConfig(nil)
	if err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if !cfg.Ignore["spam.io"] || !cfg.Ignore["foo.bar"] || !cfg.Ignore["netflix.com"] {
		t.Errorf("ignore list must merge defaults + IGNORE_DOMAINS lowercased: %+v", cfg.Ignore)
	}
	if cfg.Workers != 4 || cfg.BatchSize != 50 || cfg.Since == nil || cfg.Port != "993" {
		t.Errorf("config fields wrong: %+v", cfg)
	}
}
