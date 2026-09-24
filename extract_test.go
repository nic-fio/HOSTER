package main

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestBuildPasswords(t *testing.T) {
	pwfile := writeTemp(t, "pw.txt", "# nota\nalpha\nbeta\n\nalpha\n")
	got := buildPasswords("cli", pwfile)
	want := []string{"", "cli", "alpha", "beta"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildPasswords = %v, want %v", got, want)
	}
}

func TestSafeJoinContained(t *testing.T) {
	base := "/srv/dl"
	// tentativi di path-traversal devono restare dentro base.
	for _, name := range []string{"a/b.txt", "../../etc/passwd", "..\\..\\win.txt", "/abs/file"} {
		got, err := safeJoin(base, name)
		if err != nil {
			continue // rifiutato: comunque sicuro
		}
		if got != base && !strings.HasPrefix(got, base+string(os.PathSeparator)) {
			t.Errorf("safeJoin(%q) = %q esce da base", name, got)
		}
	}
}

func TestArchiveBase(t *testing.T) {
	cases := map[string]string{
		"movie.part01.rar": "movie",
		"x.rar":            "x",
		"y.zip":            "y",
	}
	for in, want := range cases {
		if got := archiveBase(in); got != want {
			t.Errorf("archiveBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFindArchiveSets(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{
		"movie.part01.rar", "movie.part02.rar", "movie.part03.rar",
		"other.zip", "single.rar", "readme.txt",
	} {
		os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)
	}
	mains, err := findArchiveSets(dir)
	if err != nil {
		t.Fatal(err)
	}
	var base []string
	for _, m := range mains {
		base = append(base, filepath.Base(m))
	}
	sort.Strings(base)
	want := []string{"movie.part01.rar", "other.zip", "single.rar"}
	if !reflect.DeepEqual(base, want) {
		t.Errorf("findArchiveSets = %v, want %v", base, want)
	}
}

func TestRemoveSet(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"m.part01.rar", "m.part02.rar", "m.rev", "m.mkv"} {
		os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)
	}
	removeSet(dir, filepath.Join(dir, "m.part01.rar"))
	if _, err := os.Stat(filepath.Join(dir, "m.part01.rar")); err == nil {
		t.Error("archivio rar non rimosso")
	}
	if _, err := os.Stat(filepath.Join(dir, "m.mkv")); err != nil {
		t.Error("il file non-archivio non doveva essere rimosso")
	}
}

// Set reale di terabytez (LISTA_LINKS): kali ...part01.rar..part15.rar + .rev.
// Solo part01.rar dev'essere il "main" del set; le altre parti e i .rev no.
func TestKaliMultiVolumeSet(t *testing.T) {
	dir := t.TempDir()
	base := "kali-linux-2026.2-virtualbox-amd64"
	var names []string
	for i := 1; i <= 15; i++ {
		names = append(names, fmt.Sprintf("%s.part%02d.rar", base, i))
	}
	revs := []string{base + ".part01.rev", base + ".part02.rev", base + ".part03.rev"}
	for _, n := range append(append([]string{}, names...), revs...) {
		os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)
	}

	mains, err := findArchiveSets(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(mains) != 1 || filepath.Base(mains[0]) != base+".part01.rar" {
		var got []string
		for _, m := range mains {
			got = append(got, filepath.Base(m))
		}
		t.Fatalf("main del set = %v, want [%s.part01.rar]", got, base)
	}

	// il nome reale di terabytez si ricava già dall'URL (non serve l'header).
	if got := nameFromURLPath("https://terabytez.org/12xvxfzurhj1/" + base + ".part01.rar"); got != base+".part01.rar" {
		t.Errorf("nameFromURLPath terabytez = %q", got)
	}

	// cleanup: i volumi .rar vengono rimossi.
	removeSet(dir, mains[0])
	for i := 1; i <= 15; i++ {
		n := fmt.Sprintf("%s.part%02d.rar", base, i)
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			t.Errorf("%s non rimosso dal cleanup", n)
		}
	}
	// il cleanup rimuove anche i volumi di recupero ".partNN.rev" del set.
	for _, n := range revs {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			t.Errorf("%s non rimosso dal cleanup", n)
		}
	}
}

func TestGoExtractZIPRoundTripAndSlip(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "a.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	w1, _ := zw.Create("sub/hello.txt")
	w1.Write([]byte("hi"))
	w2, _ := zw.Create("../escape.txt") // tentativo di zip-slip
	w2.Write([]byte("nope"))
	zw.Close()
	zf.Close()

	dest := filepath.Join(dir, "out")
	if err := goExtractZIP(zipPath, dest); err != nil {
		t.Fatalf("goExtractZIP: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "sub", "hello.txt"))
	if err != nil || string(got) != "hi" {
		t.Errorf("hello.txt = %q err=%v", got, err)
	}
	// la voce malevola deve restare dentro dest, mai nel parent.
	if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "escape.txt")); err == nil {
		t.Error("zip-slip: file scritto fuori dalla cartella di destinazione")
	}
}
