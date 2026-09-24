package main

import "testing"

func TestParseKeys(t *testing.T) {
	cases := []struct {
		in   string
		want [2]int
	}{
		{"q", [2]int{kQuit, -1}},
		{"\x1b[A", [2]int{kUp, -1}},
		{"\x1b[B", [2]int{kDown, -1}},
		{"\x1b[C", [2]int{kRight, -1}},
		{"\x1b[D", [2]int{kLeft, -1}},
		{"\t", [2]int{kTab, -1}},
		{"j", [2]int{kDown, -1}},
		{"k", [2]int{kUp, -1}},
		{"1", [2]int{kNone, 0}},
		{"3", [2]int{kNone, 2}},
	}
	for _, c := range cases {
		evs := parseKeys([]byte(c.in))
		if len(evs) != 1 || evs[0] != c.want {
			t.Errorf("parseKeys(%q) = %v, want [%v]", c.in, evs, c.want)
		}
	}
}

func TestBuildTabs(t *testing.T) {
	tabs := buildTabs()
	if len(tabs) == 0 {
		t.Fatal("nessuna scheda")
	}
	// ogni scheda deve produrre righe senza panicare.
	for _, tb := range tabs {
		if tb.name == "" {
			t.Error("scheda senza nome")
		}
		_ = tb.build(60, false)
	}
}
