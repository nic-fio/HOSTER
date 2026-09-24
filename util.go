package main

import (
	"fmt"
	"html"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// normHost normalizza un host o un intero URL nel suo host minuscolo, senza
// schema, porta, credenziali, percorso o prefisso "www.".
func normHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if i := strings.Index(h, "://"); i >= 0 {
		h = h[i+3:]
	}
	if i := strings.IndexAny(h, "/?#"); i >= 0 {
		h = h[:i]
	}
	if i := strings.LastIndex(h, "@"); i >= 0 {
		h = h[i+1:]
	}
	if i := strings.Index(h, ":"); i >= 0 {
		h = h[:i]
	}
	return strings.TrimPrefix(h, "www.")
}

// labelOf restituisce l'etichetta principale di un host (la parte prima del
// primo punto): "terabytez.org" -> "terabytez". Serve a far combaciare un
// account scritto come "terabytez" con i link su "terabytez.org"/".com".
func labelOf(host string) string {
	h := normHost(host)
	if i := strings.Index(h, "."); i >= 0 {
		return h[:i]
	}
	return h
}

// attr estrae il valore dell'attributo name da un frammento di tag HTML
// (gestisce sia virgolette doppie sia singole) e ne decodifica le entità.
func attr(tag, name string) string {
	q := regexp.QuoteMeta(name)
	if m := regexp.MustCompile(`(?i)\b` + q + `\s*=\s*"([^"]*)"`).FindStringSubmatch(tag); m != nil {
		return html.UnescapeString(m[1])
	}
	if m := regexp.MustCompile(`(?i)\b` + q + `\s*=\s*'([^']*)'`).FindStringSubmatch(tag); m != nil {
		return html.UnescapeString(m[1])
	}
	return ""
}

// resolveURL risolve ref rispetto a base (gestisce link relativi nelle action).
func resolveURL(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

// safeName rende una stringa usabile come nome file (per i dump di debug).
func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if len(out) > 60 {
		out = out[len(out)-60:]
	}
	return out
}

func itoa(n int) string { return strconv.Itoa(n) }

func errf2(format string, a ...any) error { return fmt.Errorf(format, a...) }

// parseSize converte stringhe tipo "10M", "1.5G", "500k", "2048" in byte.
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	mult := float64(1)
	switch s[len(s)-1] {
	case 'k', 'K':
		mult = 1 << 10
		s = s[:len(s)-1]
	case 'm', 'M':
		mult = 1 << 20
		s = s[:len(s)-1]
	case 'g', 'G':
		mult = 1 << 30
		s = s[:len(s)-1]
	case 't', 'T':
		mult = 1 << 40
		s = s[:len(s)-1]
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("dimensione non valida: %q", s)
	}
	return int64(v * mult), nil
}

// RateLimiter è un token-bucket globale condiviso da tutti i lettori.
type RateLimiter struct {
	mu     sync.Mutex
	rate   float64 // byte/sec, 0 = illimitato
	tokens float64
	max    float64
	last   time.Time
}

func NewRateLimiter(rate int64) *RateLimiter {
	if rate <= 0 {
		return &RateLimiter{}
	}
	return &RateLimiter{rate: float64(rate), tokens: float64(rate), max: float64(rate), last: time.Now()}
}

// Take blocca finché n byte non sono "permessi" dalla banda configurata.
func (r *RateLimiter) Take(n int) {
	if r.rate <= 0 {
		return
	}
	r.mu.Lock()
	now := time.Now()
	r.tokens += now.Sub(r.last).Seconds() * r.rate
	r.last = now
	if r.tokens > r.max {
		r.tokens = r.max
	}
	r.tokens -= float64(n)
	var wait time.Duration
	if r.tokens < 0 {
		wait = time.Duration(-r.tokens / r.rate * float64(time.Second))
	}
	r.mu.Unlock()
	if wait > 0 {
		time.Sleep(wait)
	}
}

// meterReader avvolge un Reader applicando il rate-limit.
type meterReader struct {
	r   io.Reader
	lim *RateLimiter
}

func (m *meterReader) Read(p []byte) (int, error) {
	n, err := m.r.Read(p)
	if n > 0 && m.lim != nil {
		m.lim.Take(n)
	}
	return n, err
}
