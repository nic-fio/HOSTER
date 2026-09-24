package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Account è una credenziale per un host.
type Account struct {
	Domain, User, Pass string
}

// Accounts indicizza le credenziali per host normalizzato e per etichetta
// (il nome senza TLD), così "terabytez" matcha anche "terabytez.org".
type Accounts struct {
	byHost  map[string]Account
	byLabel map[string]Account
}

// firstSep restituisce l'indice del primo separatore di campo (':' o ';').
func firstSep(s string) int {
	i1 := strings.IndexByte(s, ':')
	i2 := strings.IndexByte(s, ';')
	switch {
	case i1 < 0:
		return i2
	case i2 < 0:
		return i1
	case i1 < i2:
		return i1
	default:
		return i2
	}
}

// LoadAccounts legge il file credenziali: una riga per host, nella forma
//
//	dominio<sep>utente<sep>password
//
// dove <sep> è ':' oppure ';'. I primi due separatori delimitano dominio e
// utente; la password è tutto il resto (può contenere ':', ';', ecc.).
// Righe vuote e righe che iniziano con '#' sono ignorate.
func LoadAccounts(path string) (*Accounts, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	a := &Accounts{byHost: map[string]Account{}, byLabel: map[string]Account{}}
	sc := bufio.NewScanner(f)
	ln := 0
	for sc.Scan() {
		ln++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		s1 := firstSep(line)
		if s1 <= 0 {
			return nil, fmt.Errorf("riga %d non valida (manca ':' o ';'): %q", ln, line)
		}
		dom := normHost(line[:s1])
		rest := line[s1+1:]
		s2 := firstSep(rest)
		if s2 < 0 {
			return nil, fmt.Errorf("riga %d: manca il separatore tra utente e password: %q", ln, line)
		}
		user := strings.TrimSpace(rest[:s2])
		pass := rest[s2+1:]
		if dom == "" || user == "" {
			return nil, fmt.Errorf("riga %d: dominio o utente vuoto: %q", ln, line)
		}
		acc := Account{Domain: dom, User: user, Pass: pass}
		a.byHost[dom] = acc
		a.byLabel[labelOf(dom)] = acc
	}
	return a, sc.Err()
}

// Lookup trova l'account per un host (o per un URL: normHost lo ripulisce),
// prima per host esatto, poi per etichetta senza TLD.
func (a *Accounts) Lookup(host string) (Account, bool) {
	if acc, ok := a.byHost[normHost(host)]; ok {
		return acc, true
	}
	if acc, ok := a.byLabel[labelOf(host)]; ok {
		return acc, true
	}
	return Account{}, false
}
