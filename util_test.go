package main

import "testing"

func TestNormHost(t *testing.T) {
	cases := map[string]string{
		"terabytez.org":                      "terabytez.org",
		"https://WWW.Example.com:8080/path":  "example.com",
		"http://user:pass@Host.com/x?y#z":    "host.com",
		"www.terabytez.org":                  "terabytez.org",
		"  HTTPS://Terabytez.ORG/file/123  ": "terabytez.org",
		"host.com:443":                       "host.com",
	}
	for in, want := range cases {
		if got := normHost(in); got != want {
			t.Errorf("normHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLabelOf(t *testing.T) {
	cases := map[string]string{
		"terabytez.org":           "terabytez",
		"https://terabytez.com/x": "terabytez",
		"worldbytez.com":          "worldbytez",
		"singlelabel":             "singlelabel",
	}
	for in, want := range cases {
		if got := labelOf(in); got != want {
			t.Errorf("labelOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAttr(t *testing.T) {
	if got := attr(`<input name="op" value="download2">`, "name"); got != "op" {
		t.Errorf(`attr name = %q, want "op"`, got)
	}
	if got := attr(`<input name="op" value="download2">`, "value"); got != "download2" {
		t.Errorf(`attr value = %q, want "download2"`, got)
	}
	if got := attr(`<input name='op' value='single'>`, "value"); got != "single" {
		t.Errorf(`attr (single quotes) = %q, want "single"`, got)
	}
	if got := attr(`<input name="x" value="a&amp;b">`, "value"); got != "a&b" {
		t.Errorf(`attr (entity) = %q, want "a&b"`, got)
	}
	if got := attr(`<input value="x">`, "name"); got != "" {
		t.Errorf(`attr missing = %q, want ""`, got)
	}
}

func TestResolveURL(t *testing.T) {
	cases := []struct{ base, ref, want string }{
		{"https://h.test/path/page", "/dl", "https://h.test/dl"},
		{"https://h.test/a/b", "c", "https://h.test/a/c"},
		{"https://h.test/x", "https://other.test/y", "https://other.test/y"},
	}
	for _, c := range cases {
		if got := resolveURL(c.base, c.ref); got != c.want {
			t.Errorf("resolveURL(%q,%q) = %q, want %q", c.base, c.ref, got, c.want)
		}
	}
}

func TestSafeName(t *testing.T) {
	if got := safeName("https://h.test/a b"); got != "https___h_test_a_b" {
		t.Errorf("safeName = %q", got)
	}
	long := safeName(string(make([]byte, 200)))
	if len(long) > 60 {
		t.Errorf("safeName too long: %d", len(long))
	}
}

func TestParseSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		err  bool
	}{
		{"", 0, false},
		{"2048", 2048, false},
		{"500k", 500 << 10, false},
		{"10M", 10 << 20, false},
		{"1.5G", 1<<30 + 1<<29, false},
		{"2T", 2 << 40, false},
		{"bad", 0, true},
	}
	for _, c := range cases {
		got, err := parseSize(c.in)
		if c.err {
			if err == nil {
				t.Errorf("parseSize(%q) expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSize(%q) unexpected error: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("parseSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestRateLimiterUnlimited(t *testing.T) {
	rl := NewRateLimiter(0)
	// Senza limite, Take non deve bloccare né panicare.
	rl.Take(1 << 20)
}
