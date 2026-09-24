package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFirstSep(t *testing.T) {
	cases := map[string]int{
		"a:b":   1,
		"a;b":   1,
		"ab:c;": 2,
		"a;b:c": 1,
		"abc":   -1,
	}
	for in, want := range cases {
		if got := firstSep(in); got != want {
			t.Errorf("firstSep(%q) = %d, want %d", in, got, want)
		}
	}
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadAccountsAndLookup(t *testing.T) {
	p := writeTemp(t, "account", `# commento
worldbytez.com:user1;pass1
terabytez.org:user2;p:a;ss
host.io:u:pp
`)
	a, err := LoadAccounts(p)
	if err != nil {
		t.Fatalf("LoadAccounts: %v", err)
	}

	// password che contiene separatori: tutto dopo il secondo separatore.
	if acc, ok := a.Lookup("terabytez.org"); !ok || acc.Pass != "p:a;ss" || acc.User != "user2" {
		t.Errorf("terabytez Lookup = %+v ok=%v", acc, ok)
	}
	// separatori misti: il secondo separatore (qui ':') delimita utente/password.
	if acc, ok := a.Lookup("host.io"); !ok || acc.User != "u" || acc.Pass != "pp" {
		t.Errorf("host.io Lookup = %+v ok=%v", acc, ok)
	}
	// lookup per URL completo (normHost).
	if _, ok := a.Lookup("https://worldbytez.com/abcd1234"); !ok {
		t.Errorf("lookup per URL fallita")
	}
	// lookup per etichetta senza TLD: account su .org deve matchare un link .com.
	if _, ok := a.Lookup("terabytez.com"); !ok {
		t.Errorf("lookup per etichetta (terabytez.com -> terabytez.org) fallita")
	}
	// host inesistente.
	if _, ok := a.Lookup("inesistente.xyz"); ok {
		t.Errorf("lookup host inesistente dovrebbe fallire")
	}
}

func TestLoadAccountsInvalidLine(t *testing.T) {
	p := writeTemp(t, "account", "nessunseparatore\n")
	if _, err := LoadAccounts(p); err == nil {
		t.Errorf("riga senza separatore: atteso errore")
	}
}
