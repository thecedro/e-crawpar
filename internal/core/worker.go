package core

import (
	"sync"
)

// Worker consumes jobs until the jobs channel closes. Jobs whose subject
// matches nothing or whose domain is ignored produce no result — silence is
// the noise filter at work.
//
// Worker consome jobs até o canal fechar. Jobs cujo assunto não casa com
// nada ou cujo domínio está na ignore-list não geram resultado — o silêncio
// é o filtro de ruído em ação.
func Worker(jobs <-chan Job, results chan<- Result, cats []Category, ignore map[string]bool) {
	for j := range jobs {
		category := Classify(j.Subject, cats)
		if category == "" {
			continue // not an account-evidence email / não evidencia cadastro
		}
		domain := NormalizeDomain(j.Host)
		if ignore[domain] {
			continue // configured noise / ruído configurado
		}
		results <- Result{
			Domain:   domain,
			Sender:   j.From,
			Category: category,
			Subject:  j.Subject,
			Date:     j.Date,
		}
	}
}

// RunWorkerPool spawns the given number of workers and wires the pipeline so
// that closing jobs automatically drains and closes results. It returns
// immediately; the caller drains the returned channel. The collector is the
// only writer of aggregate state, so no mutex is needed.
//
// RunWorkerPool cria a quantidade informada de workers e conecta o pipeline
// de modo que fechar jobs drene e feche results automaticamente. Retorna
// já; quem chama drena o canal retornado. A coletora é a única escritora do
// estado agregado, portanto nenhum mutex é necessário.
func RunWorkerPool(workers int, cats []Category, ignore map[string]bool, jobs <-chan Job) <-chan Result {
	results := make(chan Result)

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			Worker(jobs, results, cats, ignore)
		}()
	}

	// Single closer goroutine: results stays open until every worker exits,
	// giving the collector a clean termination signal without mutexes.
	// Goroutine única que fecha: results fica aberto até todos os workers
	// terminarem, dando à coletora sinal limpo de término sem mutexes.
	go func() {
		wg.Wait()
		close(results)
	}()
	return results
}
