package core

import (
	"fmt"
	"io"
	"mime"
	"strings"
	"time"
)

// Job is one extracted header triplet handed to the worker pool.
// Job é um trio de headers extraído entregue ao pool de workers.
type Job struct {
	Subject string    // decoded subject / assunto já decodificado
	From    string    // original first From address: "local@host"
	Host    string    // lowercase domain part of From
	Date    time.Time // envelope Date header
}

// Result is a fully processed job, ready for aggregation.
// Result é um job totalmente processado, pronto para agregação.
type Result struct {
	Domain   string // normalized base domain ("amazon.com")
	Sender   string // original distinct address ("no-reply@amazon.com")
	Category string // matched category name ("verification")
	Subject  string // decoded subject, kept as report sample candidate
	Date     time.Time
}

// DomainStat is one row of the final report: the "birth certificate" of an
// account at a service plus everything observed about it.
//
// DomainStat é uma linha do relatório final: a "certidão de nascimento" de
// uma conta num serviço junto de tudo que foi observado sobre ela.
type DomainStat struct {
	Domain          string   `json:"domain"`
	FirstSeen       string   `json:"first_seen,omitempty"` // YYYY-MM-DD or absent when unknown
	Categories      []string `json:"categories"`
	Occurrences     int      `json:"occurrences"`
	SampleSubject   string   `json:"sample_subject"` // prefers verification/welcome samples
	DistinctSenders int      `json:"distinct_senders"`
	MultiSender     bool     `json:"multiple_senders_alert"`
}

// wordDecoder decodes RFC 2047 encoded-words ("=?utf-8?...?=") commonly
// found in Subject headers. Falls back to the raw string on any error so a
// weird charset can never drop a message from the analysis.
//
// wordDecoder decodifica encoded-words RFC 2047 ("=?utf-8?...?=") comuns
// no Subject. Em caso de erro retorna o texto bruto, para que um charset
// esquisito jamais derrube uma mensagem da análise.
var wordDecoder = &mime.WordDecoder{
	CharsetReader: func(charset string, input io.Reader) (io.Reader, error) {
		switch strings.ToLower(charset) {
		case "iso-8859-1", "latin1":
			// Latin-1 maps bytes 1:1 to Unicode code points U+0000..U+00FF.
			data, err := io.ReadAll(input)
			if err != nil {
				return nil, err
			}
			var buf strings.Builder
			for _, b := range data {
				buf.WriteRune(rune(b))
			}
			return strings.NewReader(buf.String()), nil
		default:
			// windows-1252 etc. are not supported without x/text; caller falls back.
			return nil, fmt.Errorf("unsupported charset %q", charset)
		}
	},
}

// DecodeSubject decodes RFC 2047 encoded-words in a Subject header value,
// falling back to the raw string when decoding fails.
// DecodeSubject decodifica encoded-words RFC 2047 num valor de Subject,
// retornando o texto bruto quando a decodificação falha.
func DecodeSubject(s string) string {
	if !strings.Contains(s, "=?") {
		return s
	}
	if d, err := wordDecoder.DecodeHeader(s); err == nil && d != "" {
		return d
	}
	return s
}
