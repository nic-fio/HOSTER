package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCleanFileName(t *testing.T) {
	cases := map[string]string{
		"  Foo&amp;Bar.rar ": "Foo&Bar.rar",
		"../../etc/passwd":   "passwd",
		"a/b/c.txt":          "c.txt",
		"name.rar.":          "name.rar",
		"":                   "",
	}
	for in, want := range cases {
		if got := cleanFileName(in); got != want {
			t.Errorf("cleanFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNameFromURLPath(t *testing.T) {
	// con estensione plausibile -> nome; id opaco senza estensione -> "".
	if got := nameFromURLPath("https://h.test/d/abc/Kali%20Linux.iso"); got != "Kali Linux.iso" {
		t.Errorf("nameFromURLPath = %q, want %q", got, "Kali Linux.iso")
	}
	if got := nameFromURLPath("https://h.test/2xh7c3nwww38"); got != "" {
		t.Errorf("id opaco dovrebbe dare \"\", invece %q", got)
	}
}

func TestExtractHidden(t *testing.T) {
	body := `<form><input type="hidden" name="token" value="tok123"><input name="op" value="download1"></form>`
	if got := extractHidden(body, "token"); got != "tok123" {
		t.Errorf("extractHidden token = %q", got)
	}
	if got := extractHidden(body, "missing"); got != "" {
		t.Errorf("extractHidden missing = %q", got)
	}
}

func TestExtractDownloadForm(t *testing.T) {
	body := `<html><form action="/dl2" method="post">` +
		`<input name="op" value="download2">` +
		`<input name="id" value="abc">` +
		`<input name="rand" value="">` +
		`</form></html>`
	vals, action := extractDownloadForm(body, "https://example.test/file-html")
	if vals == nil {
		t.Fatal("extractDownloadForm: nessun form trovato")
	}
	if vals.Get("op") != "download2" || vals.Get("id") != "abc" {
		t.Errorf("form values = %v", vals)
	}
	if action != "https://example.test/dl2" {
		t.Errorf("action = %q, want https://example.test/dl2", action)
	}
}

func TestExtractDirectLink(t *testing.T) {
	// pattern /d/<id>/<name>
	if got := extractDirectLink(`x <a href="https://h.test/d/abc123/Kali.iso">y`, "h.test"); got != "https://h.test/d/abc123/Kali.iso" {
		t.Errorf("direct /d/ = %q", got)
	}
	// estensione media
	if got := extractDirectLink(`<a href="https://cdn.test/files/movie.mkv?x=1">`, "h.test"); got != "https://cdn.test/files/movie.mkv?x=1" {
		t.Errorf("direct media = %q", got)
	}
	// asset di tema scartati
	if got := extractDirectLink(`<a href="https://cdn.test/themes/app.js">`, "h.test"); got != "" {
		t.Errorf("asset js non doveva matchare: %q", got)
	}
}

func TestDetectCountdown(t *testing.T) {
	if !detectCountdown(`<div id="countdown">10</div>`) {
		t.Error("countdown id non rilevato")
	}
	if !detectCountdown(`Please Wait 30 seconds`) {
		t.Error("please wait non rilevato")
	}
	if detectCountdown(`<a href="x">scarica</a>`) {
		t.Error("falso positivo countdown")
	}
}

func TestFileNameFromHTML(t *testing.T) {
	if got := fileNameFromHTML(`<input name="fname" value="My File.rar">`); got != "My File.rar" {
		t.Errorf("fname = %q", got)
	}
	if got := fileNameFromHTML(`<title>Download something.iso</title>`); got != "something.iso" {
		t.Errorf("title = %q", got)
	}
}

func TestLooksLoggedIn(t *testing.T) {
	r, err := NewResolver(&Accounts{}, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if !r.looksLoggedIn("h.test", `<a href="?op=logout">Logout</a>`) {
		t.Error("logout: atteso loggato")
	}
	if r.looksLoggedIn("h.test", `<p>Incorrect Login or Password</p>`) {
		t.Error("incorrect login: atteso NON loggato")
	}
	// fallback su cookie di sessione XFileSharing.
	u, _ := url.Parse("https://cookie.test/")
	r.jar.SetCookies(u, []*http.Cookie{{Name: "xfss", Value: "1"}})
	if !r.looksLoggedIn("cookie.test", `<html>pagina neutra</html>`) {
		t.Error("cookie xfss: atteso loggato")
	}
}

// --- integrazione: server XFileSharing fittizio ---

// rewriteRT dirotta ogni richiesta (https://host/...) verso il server di test,
// preservando path/query/header così la sessione e i cookie restano coerenti.
type rewriteRT struct{ base *url.URL }

func (rt *rewriteRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r2 := req.Clone(req.Context())
	r2.URL.Scheme = rt.base.Scheme
	r2.URL.Host = rt.base.Host
	r2.Host = rt.base.Host
	return http.DefaultTransport.RoundTrip(r2)
}

func newFakeXFS(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			r.ParseForm()
			if r.FormValue("op") == "login" && r.FormValue("login") == "u" &&
				r.FormValue("password") == "p" && r.FormValue("token") == "tok123" {
				io.WriteString(w, "<html>welcome — <a href='?op=logout'>logout</a></html>")
			} else {
				io.WriteString(w, "<html>Incorrect Login</html>")
			}
			return
		}
		io.WriteString(w, `<html><form><input type="hidden" name="token" value="tok123"></form></html>`)
	})
	// host che serve il file direttamente, col nome solo in Content-Disposition.
	mux.HandleFunc("/file-direct", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="real-name.iso"`)
		w.Write([]byte("binarydata"))
	})
	// host stile XFileSharing con pagina HTML e form a due passi.
	mux.HandleFunc("/file-html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, `<html><head><title>Download</title></head><body>
<form method="post" action="/dl2">
<input type="hidden" name="op" value="download2">
<input type="hidden" name="id" value="abc">
</form></body></html>`)
	})
	mux.HandleFunc("/dl2", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, `<html><head><title>Download</title></head><body>
<a href="https://example.test/direct/Movie.2024.mkv">scarica</a>
</body></html>`)
	})
	return httptest.NewServer(mux)
}

func newTestResolver(t *testing.T, srvURL string) *Resolver {
	t.Helper()
	accts := &Accounts{
		byHost:  map[string]Account{"example.test": {Domain: "example.test", User: "u", Pass: "p"}},
		byLabel: map[string]Account{"example": {Domain: "example.test", User: "u", Pass: "p"}},
	}
	r, err := NewResolver(accts, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	base, _ := url.Parse(srvURL)
	r.client.Transport = &rewriteRT{base: base}
	return r
}

func TestResolveDirectServeNaming(t *testing.T) {
	srv := newFakeXFS(t)
	defer srv.Close()
	r := newTestResolver(t, srv.URL)

	res, err := r.Resolve("https://example.test/file-direct")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// regressione filestore: il nome viene dall'header, non dall'id opaco.
	if res.Name != "real-name.iso" {
		t.Errorf("Name = %q, want real-name.iso", res.Name)
	}
	if res.URL != "https://example.test/file-direct" {
		t.Errorf("URL = %q", res.URL)
	}
}

func TestResolveHTMLTwoStep(t *testing.T) {
	srv := newFakeXFS(t)
	defer srv.Close()
	r := newTestResolver(t, srv.URL)

	res, err := r.Resolve("https://example.test/file-html")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !strings.HasSuffix(res.URL, "/direct/Movie.2024.mkv") {
		t.Errorf("URL = %q, want .../direct/Movie.2024.mkv", res.URL)
	}
	if res.Name != "Movie.2024.mkv" {
		t.Errorf("Name = %q, want Movie.2024.mkv", res.Name)
	}
}

// Il resolver deve usare lo User-Agent configurato (-U), non una costante: alcuni
// host legano la sessione all'UA, quindi login e download devono combaciare.
func TestResolverUsesConfiguredUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="f.bin"`)
		w.Write([]byte("x"))
	}))
	defer srv.Close()

	accts := &Accounts{
		byHost:  map[string]Account{"example.test": {Domain: "example.test", User: "u", Pass: "p"}},
		byLabel: map[string]Account{"example": {Domain: "example.test", User: "u", Pass: "p"}},
	}
	r, err := NewResolver(accts, "", false, "TestUA/9.9")
	if err != nil {
		t.Fatal(err)
	}
	base, _ := url.Parse(srv.URL)
	r.client.Transport = &rewriteRT{base: base}
	r.loggedIn["example.test"] = true // salto il login: mi interessa l'UA sul GET del file

	if _, err := r.Resolve("https://example.test/file"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if gotUA != "TestUA/9.9" {
		t.Errorf("User-Agent = %q, want TestUA/9.9", gotUA)
	}
}

func TestResolveLoginFailure(t *testing.T) {
	srv := newFakeXFS(t)
	defer srv.Close()
	r := newTestResolver(t, srv.URL)
	// account con password errata -> il login fallisce in modo controllato.
	r.accounts.byHost["example.test"] = Account{Domain: "example.test", User: "u", Pass: "WRONG"}
	if _, err := r.Resolve("https://example.test/file-direct"); err == nil {
		t.Error("login con password errata: atteso errore")
	}
}
