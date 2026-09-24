package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"sync"
)

// tlsFallbackTransport prova ogni richiesta CON verifica TLS e, solo se il
// certificato dell'host non è verificabile perché la catena è incompleta
// (manca l'intermedio: tipico di alcuni nodi hoster, es. terabytez), ritenta lo
// stesso host SENZA verifica. Così gli host configurati bene restano verificati
// e quelli mal configurati funzionano lo stesso, senza dover ricordare un flag.
//
// La deroga è mirata: scatta SOLO per "autorità sconosciuta" (catena incompleta),
// non per certificati scaduti o con hostname errato, che restano rifiutati.
type tlsFallbackTransport struct {
	secure   *http.Transport
	insecure *http.Transport
	force    bool     // -k: salta direttamente alla variante senza verifica
	bad      sync.Map // host già noti per catena incompleta (memo per evitare handshake falliti ripetuti)
}

func (t *tlsFallbackTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.force {
		return t.insecure.RoundTrip(req)
	}
	if _, known := t.bad.Load(req.URL.Host); known {
		return t.insecure.RoundTrip(req)
	}
	resp, err := t.secure.RoundTrip(req)
	if err != nil && isIncompleteChain(err) {
		if _, seen := t.bad.LoadOrStore(req.URL.Host, struct{}{}); !seen {
			notef("nota: %s ha una catena di certificati incompleta — verifica TLS saltata per questo host", req.URL.Host)
		}
		return t.insecure.RoundTrip(req.Clone(req.Context()))
	}
	return resp, err
}

// isIncompleteChain riconosce il caso "impossibile costruire la catena fino a una
// radice fidata" (intermedio mancante / CA sconosciuta), distinto da scadenza o
// hostname errato.
func isIncompleteChain(err error) bool {
	var ua x509.UnknownAuthorityError
	return errors.As(err, &ua)
}

// newTLSFallbackTransport avvolge un transport di base in due varianti (con e
// senza verifica) e restituisce il RoundTripper con fallback automatico.
func newTLSFallbackTransport(base *http.Transport, force bool) http.RoundTripper {
	secure := base.Clone()
	secure.TLSClientConfig = &tls.Config{}
	insecure := base.Clone()
	insecure.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	return &tlsFallbackTransport{secure: secure, insecure: insecure, force: force}
}
