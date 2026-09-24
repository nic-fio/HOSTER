package main

import (
	"strings"
	"testing"
)

func TestWantsHelp(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"-h"}, true},
		{[]string{"--help"}, true},
		{[]string{"help"}, true},
		{[]string{"-i", "links"}, false},
		{[]string{"--", "-h"}, false}, // dopo "--" non sono più opzioni
		{nil, false},
	}
	for _, c := range cases {
		if got := wantsHelp(c.args); got != c.want {
			t.Errorf("wantsHelp(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}

func TestStripANSI(t *testing.T) {
	in := "\033[1m\033[32mciao\033[0m"
	if got := stripANSI(in); got != "ciao" {
		t.Errorf("stripANSI = %q", got)
	}
}

// La guida deve elencare ogni opzione: nessun flag fantasma, nessuno mancante.
func TestHelpCoversAllFlags(t *testing.T) {
	out := stripANSI(buildHelp(false, 80))
	var rows int
	for _, sec := range helpSections {
		for _, r := range sec.rows {
			rows++
			tag := strings.Fields(r.flag)[0] // es. "-i" da "-i FILE"
			tag = strings.TrimRight(tag, ",")
			if !strings.Contains(out, tag) {
				t.Errorf("flag %q assente dalla guida", tag)
			}
		}
	}
	if rows != 23 {
		t.Errorf("numero di opzioni documentate = %d, want 23 (aggiorna se cambi i flag)", rows)
	}
	// gli host noti e gli esempi devono comparire.
	for _, h := range supportedHosts {
		if !strings.Contains(out, h) {
			t.Errorf("host %q assente dalla guida", h)
		}
	}
}
