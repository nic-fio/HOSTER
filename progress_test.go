package main

import (
	"strings"
	"testing"
)

func TestHumanShort(t *testing.T) {
	cases := map[int64]string{
		0:       "0B",
		1023:    "1023B",
		1024:    "1.0K",
		1536:    "1.5K",
		1 << 20: "1.0M",
		153600:  "150K",
	}
	for in, want := range cases {
		if got := humanShort(in); got != want {
			t.Errorf("humanShort(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestEtaStr(t *testing.T) {
	cases := map[float64]string{
		5:    "5s",
		65:   "1m05s",
		3661: "1h01m",
		-1:   "--",
	}
	for in, want := range cases {
		if got := etaStr(in); got != want {
			t.Errorf("etaStr(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFitName(t *testing.T) {
	if got := fitName("hi", 5); got != "hi   " {
		t.Errorf("fitName pad = %q", got)
	}
	if got := fitName("hello", 5); got != "hello" {
		t.Errorf("fitName exact = %q", got)
	}
	if got := fitName("hello!", 5); got != "hell…" {
		t.Errorf("fitName trunc = %q", got)
	}
}

func TestMainBar(t *testing.T) {
	bar := mainBar(0.5, 10, false)
	if !strings.HasPrefix(bar, "▕") || !strings.HasSuffix(bar, "▏") {
		t.Errorf("mainBar bordi inattesi: %q", bar)
	}
	// estremi: non deve panicare né uscire dai bordi.
	_ = mainBar(-1, 10, false)
	_ = mainBar(2, 10, false)
}
