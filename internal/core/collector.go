package core

import (
	"sort"
	"time"
)

// agg is the mutable per-domain accumulator used while streaming results.
// agg é o acumulador mutável por domínio usado durante o streaming.
type agg struct {
	first      time.Time
	hasDate    bool
	cats       map[string]bool
	count      int
	sample     string
	sampleRank int // lower = better sample candidate
	senders    map[string]bool
}

// Collect consumes every result from the (already concurrent) pipeline and
// returns report rows sorted by first-seen date ascending — unknown dates go
// last, keeping the timeline meaningful.
//
// Collect consome todos os resultados do pipeline (já concorrente) e retorna
// as linhas do relatório ordenadas por data da primeira ocorrência crescente
// — datas desconhecidas vão para o fim, preservando a linha do tempo.
func Collect(results <-chan Result) []DomainStat {
	aggs := make(map[string]*agg)

	for r := range results {
		a := aggs[r.Domain]
		if a == nil {
			a = &agg{
				cats:       make(map[string]bool),
				senders:    make(map[string]bool),
				sampleRank: 1 << 30,
			}
			aggs[r.Domain] = a
		}

		a.count++
		a.cats[r.Category] = true
		a.senders[r.Sender] = true

		// Track the earliest envelope Date: the account's "birthday".
		// Guarda o menor Date do envelope: o "aniversário" da conta.
		if !r.Date.IsZero() && (!a.hasDate || r.Date.Before(a.first)) {
			a.first, a.hasDate = r.Date, true
		}

		// Prefer a subject that screams "account creation": verification
		// beats welcome beats anything else already stored.
		// Prefere um assunto que grite "criação de conta": verificação vence
		// boas-vindas que vence qualquer outro já armazenado.
		if rank, ok := SampleRank(r.Category); ok && rank < a.sampleRank {
			a.sample, a.sampleRank = r.Subject, rank
		} else if a.sample == "" {
			a.sample = r.Subject
		}
	}

	// Materialize rows.
	// Materializa as linhas.
	stats := make([]DomainStat, 0, len(aggs))
	for domain, a := range aggs {
		cats := make([]string, 0, len(a.cats))
		for c := range a.cats {
			cats = append(cats, c)
		}
		// Categories shown in fixed priority order for stable reports.
		// Categorias em ordem fixa de prioridade para relatórios estáveis.
		sort.Slice(cats, func(i, j int) bool {
			return categoryRank[cats[i]] < categoryRank[cats[j]]
		})

		row := DomainStat{
			Domain:          domain,
			Categories:      cats,
			Occurrences:     a.count,
			SampleSubject:   a.sample,
			DistinctSenders: len(a.senders),
			MultiSender:     len(a.senders) > 1,
		}
		if a.hasDate {
			row.FirstSeen = a.first.Format("2006-01-02")
		}
		stats = append(stats, row)
	}

	sort.Slice(stats, func(i, j int) bool {
		a, b := stats[i], stats[j]
		if (a.FirstSeen != "") != (b.FirstSeen != "") {
			return a.FirstSeen != "" // dated rows first / linhas datadas primeiro
		}
		return a.FirstSeen < b.FirstSeen
	})
	return stats
}
