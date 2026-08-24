// Layer 5 — BDD STEP DEFINITIONS (godog).
// Business scenarios written in Portuguese (.feature files in /test/features)
// drive the SAME hermetic fake server and the SAME full pipeline used by the
// E2E layer. This layer exists so the specification itself — not just the
// code — is executable and readable by non-developers.
//
// Camada 5 — DEFINIÇÕES DE PASSOS BDD (godog).
// Cenários de negócio escritos em português (arquivos .feature em
// /test/features) dirigem o MESMO servidor fake hermético e o MESMO pipeline
// completo usados pela camada E2E. Esta camada existe para que a própria
// especificação — não só o código — seja executável e legível por quem não
// desenvolve.
package steps_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"

	"e-crawpar/internal/core"
	"e-crawpar/test/harness"
)

// scenarioState carries one Gherkin scenario's inbox + resulting report.
// scenarioState carrega a caixa e o relatório resultante de um cenário.
type scenarioState struct {
	msgs  []harness.Msg
	stats []core.DomainStat
}

func (s *scenarioState) reset() {
	s.msgs = nil
	s.stats = nil
}

// thereIsAnEmail registers one synthetic message from the step text.
// thereIsAnEmail registra uma mensagem sintética vinda do texto do passo.
func (s *scenarioState) thereIsAnEmail(subject, from, date string) error {
	var d time.Time
	if date != "" {
		parsed, err := time.Parse("2006-01-02", date)
		if err != nil {
			return fmt.Errorf("data inválida %q: %w", date, err)
		}
		d = parsed
	}
	s.msgs = append(s.msgs, harness.Msg{From: from, Subject: subject, Date: d})
	return nil
}

// processInbox boots the fake IMAP server with the registered messages and
// runs the production pipeline over it, storing the final report rows.
//
// processInbox sobe o servidor IMAP fake com as mensagens registradas e roda
// o pipeline de produção sobre ele, guardando as linhas finais do relatório.
func (s *scenarioState) processInbox(ctx context.Context) (context.Context, error) {
	handle, err := harness.StartServerManual(s.msgs, nil)
	if err != nil {
		return ctx, err
	}
	defer handle.Close()

	client := harness.Dial(ambientT, handle.Addr)
	opt := harness.Options(handle.Addr, "INBOX", 3) // small batches / lotes pequenos
	s.stats, err = harness.RunPipelineErr(client, opt, 8, ignoreFromDefaults())
	return ctx, err
}

func ignoreFromDefaults() map[string]bool {
	m := make(map[string]bool, len(core.DefaultIgnoreDomains))
	for _, d := range core.DefaultIgnoreDomains {
		m[d] = true
	}
	return m
}

func (s *scenarioState) find(domain string) *core.DomainStat {
	for i := range s.stats {
		if s.stats[i].Domain == domain {
			return &s.stats[i]
		}
	}
	return nil
}

func (s *scenarioState) reportListsDomain(domain string) error {
	assert.NotNilf(ambientT, s.find(domain), "domínio %q deveria estar no relatório: %+v", domain, s.stats)
	return nil
}

func (s *scenarioState) reportDoesNotListDomain(domain string) error {
	assert.Nilf(ambientT, s.find(domain), "domínio %q não deveria estar no relatório: %+v", domain, s.stats)
	return nil
}

func (s *scenarioState) domainHasCategory(domain, category string) error {
	row := s.find(domain)
	if !assert.NotNilf(ambientT, row, "domínio %q ausente do relatório", domain) {
		return nil // assertion already failed; stop here / asserção já falhou
	}
	assert.Equalf(ambientT, category, row.Categories[0],
		"categoria de %q = %v", domain, row.Categories)
	return nil
}

func (s *scenarioState) domainHasMultiSenderAlert(domain string) error {
	row := s.find(domain)
	if !assert.NotNilf(ambientT, row, "domínio %q ausente do relatório", domain) {
		return nil
	}
	assert.Truef(ambientT, row.MultiSender, "domínio %q sem alerta de múltiplos remetentes: %+v", domain, row)
	return nil
}

func (s *scenarioState) firstListedDomain(domain string) error {
	if !assert.NotEmpty(ambientT, s.stats, "relatório vazio") {
		return nil
	}
	assert.Equalf(ambientT, domain, s.stats[0].Domain, "primeiro domínio = %q", s.stats[0].Domain)
	return nil
}

func (s *scenarioState) lastListedDomain(domain string) error {
	if !assert.NotEmpty(ambientT, s.stats, "relatório vazio") {
		return nil
	}
	last := s.stats[len(s.stats)-1]
	assert.Equalf(ambientT, domain, last.Domain, "último domínio = %q", last.Domain)
	return nil
}

// ambientT is the *testing.T of the running TestFeatures. godog has no
// testing context of its own; testify assertions need one to report through.
// ambientT é o *testing.T do TestFeatures em execução; o godog não tem um
// contexto próprio e as asserções testify precisam dele para reportar.
var ambientT *testing.T

func InitializeScenario(ctx *godog.ScenarioContext) {
	st := &scenarioState{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		st.reset()
		return ctx, nil
	})

	ctx.Step(`^que existe um e-mail com assunto "([^"]*)" de "([^"]*)" com data de "([^"]*)"$`,
		st.thereIsAnEmail)
	ctx.Step(`^o pipeline processa a caixa de entrada$`, st.processInbox)
	ctx.Step(`^o relatório final deve listar o domínio "([^"]+)"$`, st.reportListsDomain)
	ctx.Step(`^o relatório não deve listar o domínio "([^"]+)"$`, st.reportDoesNotListDomain)
	ctx.Step(`^a categoria do domínio "([^"]+)" deve ser "([^"]+)"$`, st.domainHasCategory)
	ctx.Step(`^o domínio "([^"]+)" deve ter alerta de múltiplos remetentes$`, st.domainHasMultiSenderAlert)
	ctx.Step(`^o primeiro domínio listado deve ser "([^"]+)"$`, st.firstListedDomain)
	ctx.Step(`^o último domínio listado deve ser "([^"]+)"$`, st.lastListedDomain)
}

// TestFeatures runs every .feature file in /test/features against the same
// hermetic fake server used by the E2E layer.
//
// TestFeatures roda todos os .feature de /test/features contra o mesmo
// servidor fake hermético usado pela camada E2E.
func TestFeatures(t *testing.T) {
	ambientT = t
	suite := godog.TestSuite{
		Name:                "bdd",
		ScenarioInitializer: InitializeScenario,
		Options: &godog.Options{
			Paths:    []string{".."}, // feature files live in /test/features / features em /test/features
			Format:   "pretty",
			Output:   os.Stdout,
			TestingT: t,
		},
	}
	if status := suite.Run(); status != 0 {
		t.Fatalf("godog suite exited with status %d", status)
	}
}
