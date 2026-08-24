// Package core holds the pure business logic of e-crawpar: subject
// classification, sender normalization, the concurrent worker pool and the
// collector that aggregates results into report rows. It has no I/O and no
// IMAP dependency, which makes it fully unit-testable and mutation-testable.
//
// O pacote core concentra a lógica de negócio pura do e-crawpar:
// classificação de assuntos, normalização de remetentes, o pool concorrente
// de workers e a coletora que agrega resultados nas linhas do relatório.
// Não faz I/O nem depende de IMAP — por isso é totalmente testável por
// unidade e por mutação.
package core

import (
	"fmt"
	"regexp"
	"sort"
)

// Category is one class of "account evidence" email.
// Category é uma classe de e-mail que evidencia cadastro em um serviço.
type Category struct {
	Name     string           // stable identifier used in reports
	Priority int              // lower = evaluated first
	Patterns []*regexp.Regexp // compiled case-insensitive patterns
}

// categorySpecs maps category names to their PT-BR/EN patterns.
// categorySpecs mapeia nomes de categoria aos padrões PT-BR/EN.
var categorySpecs = []struct {
	Name     string
	Priority int
	Patterns []string
}{
	{
		Name: "security", Priority: 1,
		Patterns: []string{
			`new login`,
			`login from`,
			`new device`,
			`senha alterada`,
			`seguran[çc]a atualizad`,
			`password (was )?changed`,
		},
	},
	{
		Name: "verification", Priority: 2,
		Patterns: []string{
			`verify your email`,
			`confirme seu e-?mail`,
			`c[óo]digo de verifica[çc][ãa]o`,
			`ative sua conta`,
			`confirm your (email|account)`,
		},
	},
	{
		Name: "welcome", Priority: 3,
		Patterns: []string{
			`welcome to`,
			`bem-vindo ao?s?`,
			`obrigado por se registrar`,
			`sua conta foi criada`,
		},
	},
	{
		Name: "receipt", Priority: 4,
		Patterns: []string{
			`fatura (est[áa] )?dispon[íi]vel`,
			`pagamento aprovado`,
			`pedido recebido`,
			`recibo`,
		},
	},
	{
		Name: "policy", Priority: 5,
		Patterns: []string{
			`privacy policy update`,
			`termos de uso atualizados`,
			`terms of (service|use) (update|updated)`,
		},
	},
}

// BuildCategories compiles all specs once at startup; workers only read it.
// BuildCategories compila todas as especificações uma vez no startup;
// os workers apenas as leem.
func BuildCategories() []Category {
	cats := make([]Category, 0, len(categorySpecs))
	for _, spec := range categorySpecs {
		c := Category{Name: spec.Name, Priority: spec.Priority}
		for _, p := range spec.Patterns {
			re, err := regexp.Compile(`(?i)` + p)
			if err != nil {
				// A bad pattern here is a programming bug, not user input.
				panic(fmt.Sprintf("invalid category pattern %q: %v", p, err))
			}
			c.Patterns = append(c.Patterns, re)
		}
		cats = append(cats, c)
	}
	// Enforce evaluation order security > verification > welcome > receipt > policy.
	// Garante a ordem segurança > verificação > boas-vindas > recibo > política.
	sort.Slice(cats, func(i, j int) bool { return cats[i].Priority < cats[j].Priority })
	return cats
}

// Classify returns the first category matching the subject, or "" when none
// does. Categories are evaluated in priority order (security first), so a
// subject that matches two patterns keeps its strongest signal.
//
// Classify retorna a primeira categoria que casa com o assunto, ou "" se
// nenhuma casar. As categorias são avaliadas em ordem de prioridade
// (segurança primeiro), então um assunto que case com dois padrões mantém
// seu sinal mais forte.
func Classify(subject string, cats []Category) string {
	for _, c := range cats {
		for _, re := range c.Patterns {
			if re.MatchString(subject) {
				return c.Name
			}
		}
	}
	return ""
}

// SampleRank maps categories to sample desirability: verification (2) then
// welcome (3); other categories are not eligible as curated samples but the
// caller still falls back to the first seen subject.
//
// SampleRank mapeia categorias para a preferência de amostra: verificação (2)
// depois boas-vindas (3); outras categorias não são amostras curadas, mas o
// chamador ainda usa o primeiro assunto visto como fallback.
func SampleRank(category string) (int, bool) {
	switch category {
	case "verification":
		return 2, true
	case "welcome":
		return 3, true
	default:
		return 0, false
	}
}

// categoryRank drives both evaluation order and report display order.
// categoryRank dirige tanto a ordem de avaliação quanto a de exibição.
var categoryRank = func() map[string]int {
	m := make(map[string]int, len(categorySpecs))
	for _, s := range categorySpecs {
		m[s.Name] = s.Priority
	}
	return m
}()
