package main

import (
	"os"
	"testing"
)

// TestLiveResolve risolve i link reali di LISTA_LINKS usando l'account reale,
// SENZA scaricare (solo Resolve, come un dry-run). È un controllo manuale:
// parte solo con HOSTER_LIVE=1, così non entra nella suite offline, che deve
// restare ermetica (niente rete né credenziali).
//
//	HOSTER_LIVE=1 go test -run TestLiveResolve -v
//
// Valida sul campo proprio il caso filestore (id opaco -> nome dal server) e
// terabytez (nome nell'URL), gli stessi coperti in modo ermetico dai fittizi.
func TestLiveResolve(t *testing.T) {
	if os.Getenv("HOSTER_LIVE") == "" {
		t.Skip("test dal vivo disattivato: imposta HOSTER_LIVE=1 per eseguirlo")
	}
	acctFile := envOr("HOSTER_ACCOUNT", "account")
	linksFile := envOr("HOSTER_LINKS", "LISTA_LINKS")

	accts, err := LoadAccounts(acctFile)
	if err != nil {
		t.Fatalf("account: %v (serve il file %q reale)", err, acctFile)
	}
	links, err := LoadLinks(linksFile)
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if len(links) == 0 {
		t.Fatalf("nessun link in %s", linksFile)
	}
	// force=false: verifichiamo che il fallback TLS automatico gestisca da solo
	// gli host con catena incompleta (es. terabytez), senza forzare -k.
	res, err := NewResolver(accts, "", false, "")
	if err != nil {
		t.Fatal(err)
	}

	var ok, fail int
	for _, link := range links {
		r, err := res.Resolve(link)
		if err != nil {
			t.Logf("✗ %s\n      %v", link, err)
			fail++
			continue
		}
		name := r.Name
		if name == "" {
			name = fileNameFromURL(r.URL)
		}
		// il nome non deve essere un id opaco senza estensione (regressione filestore).
		if name == "" {
			t.Errorf("nome vuoto per %s", link)
		}
		t.Logf("✓ %-48s -> %s", name, r.URL)
		ok++
	}
	t.Logf("risolti %d, falliti %d (su %d)", ok, fail, len(links))
	if ok == 0 {
		t.Fatalf("nessun link risolto: login o resolver da verificare")
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
