package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCDFileName(t *testing.T) {
	cases := map[string]string{
		`attachment; filename="kali-linux.rar"`:        "kali-linux.rar",
		`attachment; filename*=UTF-8''Fedora%2040.iso`: "Fedora 40.iso",
		`inline; filename=plain.bin`:                   "plain.bin",
		``:                                             "",
	}
	for in, want := range cases {
		if got := cdFileName(in); got != want {
			t.Errorf("cdFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMakeSegs(t *testing.T) {
	segs := makeSegs(1000, 4)
	if len(segs) != 4 {
		t.Fatalf("segmenti = %d, want 4", len(segs))
	}
	if segs[0].start != 0 || segs[0].end != 249 {
		t.Errorf("seg0 = %d-%d", segs[0].start, segs[0].end)
	}
	// l'ultimo segmento copre fino a size-1, senza buchi.
	if segs[3].end != 999 {
		t.Errorf("ultimo seg end = %d, want 999", segs[3].end)
	}
	for i := 1; i < len(segs); i++ {
		if segs[i].start != segs[i-1].end+1 {
			t.Errorf("buco tra seg %d e %d", i-1, i)
		}
	}
}

// serveData restituisce un server che serve data con supporto ai Range e un
// eventuale Content-Disposition.
func serveData(data []byte, cd string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cd != "" {
			w.Header().Set("Content-Disposition", cd)
		}
		http.ServeContent(w, r, "", time.Unix(0, 0), bytes.NewReader(data))
	}))
}

func newTestDownloader(t *testing.T, cfg DLConfig) *Downloader {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := newDownloadClient(jar, 30, 8, false)
	return NewDownloader(client, cfg, NewProgress(true))
}

// Regressione filestore: un link con id opaco (senza estensione) deve produrre
// un file col nome dichiarato dal server nel Content-Disposition.
func TestDownloadFileNamingFromContentDisposition(t *testing.T) {
	data := bytes.Repeat([]byte("A"), 2048)
	srv := serveData(data, `attachment; filename="kali-linux.rar"`)
	defer srv.Close()

	dl := newTestDownloader(t, DLConfig{Split: 1, Jobs: 1, MinSplit: 1 << 20, Retries: 1, RetryWait: 0})
	out := t.TempDir()
	opaque := filepath.Join(out, "2xh7c3nwww38")
	if err := dl.downloadFile(srv.URL+"/2xh7c3nwww38", opaque, nil); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}

	renamed := filepath.Join(out, "kali-linux.rar")
	got, err := os.ReadFile(renamed)
	if err != nil {
		t.Fatalf("atteso file rinominato in kali-linux.rar: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("contenuto non corrisponde (%d byte)", len(got))
	}
	if _, err := os.Stat(opaque); err == nil {
		t.Errorf("il file con id opaco non dovrebbe esistere")
	}
}

func TestDownloadFileSingleStream(t *testing.T) {
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i % 251)
	}
	srv := serveData(data, "")
	defer srv.Close()

	dl := newTestDownloader(t, DLConfig{Split: 1, Jobs: 1, MinSplit: 1 << 20, Retries: 1, RetryWait: 0})
	dest := filepath.Join(t.TempDir(), "file.bin")
	if err := dl.downloadFile(srv.URL+"/file.bin", dest, nil); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Errorf("contenuto single-stream non corrisponde")
	}
}

func TestDownloadFileSegmented(t *testing.T) {
	const size = 3 << 20 // 3 MiB, sopra MinSplit
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 251)
	}
	srv := serveData(data, "")
	defer srv.Close()

	dl := newTestDownloader(t, DLConfig{Split: 4, Jobs: 1, MinSplit: 1 << 20, Retries: 2, RetryWait: 0})
	dest := filepath.Join(t.TempDir(), "big.bin")
	if err := dl.downloadFile(srv.URL+"/big.bin", dest, nil); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if len(got) != size {
		t.Fatalf("dimensione = %d, want %d", len(got), size)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("contenuto segmentato non corrisponde")
	}
	// niente file residui .part / .part.hoster.
	if _, err := os.Stat(dest + ".part"); err == nil {
		t.Errorf(".part non rimosso")
	}
}

// Regressione del bug di corruzione: se la connessione cade a metà, il retry del
// single-stream deve RIPRENDERE dall'offset scritto, non riscaricare e accodare.
// Con il vecchio codice il file usciva sovradimensionato e corrotto.
func TestDownloadFileResumesAfterMidStreamDrop(t *testing.T) {
	data := make([]byte, 8000)
	for i := range data {
		data[i] = byte(i % 251)
	}
	var realGets int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rng := r.Header.Get("Range")
		if rng == "bytes=0-0" { // probe: dimensione + supporto ai range
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-0/%d", len(data)))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(data[:1])
			return
		}
		var start int
		if rng != "" {
			fmt.Sscanf(rng, "bytes=%d-", &start)
		}
		w.Header().Set("Accept-Ranges", "bytes")
		if start > 0 {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
			w.Header().Set("Content-Length", fmt.Sprint(len(data)-start))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			w.WriteHeader(http.StatusOK)
		}
		if atomic.AddInt32(&realGets, 1) == 1 {
			// primo tentativo: manda solo una parte, poi tronca bruscamente.
			w.Write(data[start : start+500])
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			panic(http.ErrAbortHandler)
		}
		w.Write(data[start:]) // tentativi successivi: corpo completo dal punto richiesto
	}))
	defer srv.Close()

	dl := newTestDownloader(t, DLConfig{Split: 1, Jobs: 1, MinSplit: 1 << 20, Retries: 3, RetryWait: 0})
	dest := filepath.Join(t.TempDir(), "drop.bin")
	if err := dl.downloadFile(srv.URL+"/drop.bin", dest, nil); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if len(got) != len(data) {
		t.Fatalf("dimensione = %d, want %d (ripresa errata: accodamento?)", len(got), len(data))
	}
	if !bytes.Equal(got, data) {
		t.Errorf("contenuto corrotto dopo la ripresa")
	}
}

// Se il link diretto è scaduto (403), downloadFile deve rigenerarlo via refresh
// e completare con il link fresco, invece di insistere sull'URL morto.
func TestStaleLinkReResolves(t *testing.T) {
	data := bytes.Repeat([]byte("Z"), 3000)
	var refreshCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/old") {
			w.WriteHeader(http.StatusForbidden) // link scaduto
			return
		}
		http.ServeContent(w, r, "", time.Unix(0, 0), bytes.NewReader(data))
	}))
	defer srv.Close()

	dl := newTestDownloader(t, DLConfig{Split: 1, Jobs: 1, MinSplit: 1 << 20, Retries: 2, RetryWait: 0})
	dest := filepath.Join(t.TempDir(), "f.bin")
	refresh := func() (string, error) {
		atomic.AddInt32(&refreshCalls, 1)
		return srv.URL + "/new", nil
	}
	if err := dl.downloadFile(srv.URL+"/old", dest, refresh); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}
	if atomic.LoadInt32(&refreshCalls) == 0 {
		t.Error("refresh non chiamato: il link scaduto non è stato rigenerato")
	}
	if got, _ := os.ReadFile(dest); !bytes.Equal(got, data) {
		t.Errorf("contenuto errato dopo la ri-risoluzione")
	}
}

func TestProbe(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 5000)
	srv := serveData(data, `attachment; filename="z.bin"`)
	defer srv.Close()

	dl := newTestDownloader(t, DLConfig{Split: 1, Retries: 1})
	info, err := dl.probe(srv.URL + "/z")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if info.size != int64(len(data)) {
		t.Errorf("size = %d, want %d", info.size, len(data))
	}
	if !info.ranges {
		t.Errorf("ranges = false, atteso true")
	}
	if info.name != "z.bin" {
		t.Errorf("name = %q, want z.bin", info.name)
	}
}
