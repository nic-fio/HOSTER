package main

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Resolver fa login agli host e trasforma le pagine-file in link diretti.
// La logica è quella generica di XFileSharing Pro (su cui si basano worldbytez,
// terabytez & co.); le euristiche di parsing vanno rifinite su host reali
// guardando i dump prodotti da -debug.
type Resolver struct {
	client    *http.Client
	jar       *cookiejar.Jar
	accounts  *Accounts
	debugDir  string
	userAgent string
	mu        sync.Mutex      // protegge loggedIn (le ri-risoluzioni sono concorrenti)
	loggedIn  map[string]bool // host già loggati in questa esecuzione
}

func NewResolver(accts *Accounts, debugDir string, forceInsecure bool, ua string) (*Resolver, error) {
	if ua == "" {
		ua = userAgent
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	base := &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		ForceAttemptHTTP2: true,
	}
	return &Resolver{
		client:    &http.Client{Jar: jar, Timeout: 90 * time.Second, Transport: newTLSFallbackTransport(base, forceInsecure)},
		jar:       jar,
		accounts:  accts,
		debugDir:  debugDir,
		userAgent: ua,
		loggedIn:  map[string]bool{},
	}, nil
}

func (r *Resolver) do(req *http.Request) (string, *http.Response, error) {
	req.Header.Set("User-Agent", r.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	resp, err := r.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return "", resp, err
	}
	return string(body), resp, nil
}

func (r *Resolver) get(rawurl string) (string, *http.Response, error) {
	req, err := http.NewRequest("GET", rawurl, nil)
	if err != nil {
		return "", nil, err
	}
	return r.do(req)
}

func (r *Resolver) postForm(rawurl string, vals url.Values, referer string) (string, *http.Response, error) {
	req, err := http.NewRequest("POST", rawurl, strings.NewReader(vals.Encode()))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	return r.do(req)
}

// filePage raccoglie ciò che serve dalla risposta alla pagina-file.
type filePage struct {
	isHTML   bool
	body     string // HTML, solo quando isHTML
	finalURL string // URL dopo gli eventuali redirect
	cdName   string // nome dichiarato dal server (Content-Disposition)
}

// openFilePage apre l'URL della pagina-file e decide, dal solo Content-Type
// (senza scaricare il corpo), se l'host sta servendo direttamente il file
// (caso premium tipico) oppure una pagina HTML da analizzare. Cattura anche
// l'URL finale (dopo i redirect) e il nome dichiarato in Content-Disposition,
// utili per gli host che servono il file da un id opaco (es. filestore.me).
func (r *Resolver) openFilePage(link string) (filePage, error) {
	req, err := http.NewRequest("GET", link, nil)
	if err != nil {
		return filePage{}, err
	}
	req.Header.Set("User-Agent", r.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	resp, err := r.client.Do(req)
	if err != nil {
		return filePage{}, err
	}
	defer resp.Body.Close()

	fp := filePage{cdName: cdFileName(resp.Header.Get("Content-Disposition"))}
	if resp.Request != nil && resp.Request.URL != nil {
		fp.finalURL = resp.Request.URL.String()
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	cd := strings.ToLower(resp.Header.Get("Content-Disposition"))
	if !strings.Contains(ct, "text/html") || strings.Contains(cd, "attachment") {
		// L'host risponde con il file (o un download forzato): non leggo il
		// corpo, è il binario.
		return fp, nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	fp.isHTML = true
	fp.body = string(b)
	return fp, err
}

func (r *Resolver) dump(tag, body string) {
	if r.debugDir == "" {
		return
	}
	_ = os.MkdirAll(r.debugDir, 0o755)
	_ = os.WriteFile(filepath.Join(r.debugDir, tag+".html"), []byte(body), 0o644)
}

// ensureLogin esegue (una sola volta per host) il login XFileSharing.
func (r *Resolver) ensureLogin(host string) error {
	r.mu.Lock()
	done := r.loggedIn[host]
	r.mu.Unlock()
	if done {
		return nil
	}
	acc, ok := r.accounts.Lookup(host)
	if !ok {
		return fmt.Errorf("nessun account nel file 'account' per %s", host)
	}
	base := "https://" + host

	// 1) GET pagina di login: imposta i cookie iniziali ed eventuale token CSRF.
	page, _, err := r.get(base + "/?op=login")
	if err != nil {
		return fmt.Errorf("apertura login: %w", err)
	}
	r.dump("login_"+host+"_1page", page)

	// 2) POST delle credenziali.
	form := url.Values{}
	form.Set("op", "login")
	form.Set("login", acc.User)
	form.Set("password", acc.Pass)
	form.Set("redirect", base+"/")
	if tok := extractHidden(page, "token"); tok != "" {
		form.Set("token", tok)
	}
	res, _, err := r.postForm(base+"/", form, base+"/?op=login")
	if err != nil {
		return fmt.Errorf("invio login: %w", err)
	}
	r.dump("login_"+host+"_2result", res)

	// 3) verifica euristica.
	if !r.looksLoggedIn(host, res) {
		return fmt.Errorf("login non riuscito su %s (credenziali errate, captcha o pagina diversa: usa -debug)", host)
	}
	r.mu.Lock()
	r.loggedIn[host] = true
	r.mu.Unlock()
	return nil
}

func (r *Resolver) looksLoggedIn(host, body string) bool {
	lb := strings.ToLower(body)
	if strings.Contains(lb, "incorrect login") ||
		strings.Contains(lb, "wrong password") ||
		strings.Contains(lb, "username and password do not match") {
		return false
	}
	if strings.Contains(lb, "op=logout") || strings.Contains(lb, "logout") || strings.Contains(lb, "my account") || strings.Contains(lb, "myaccount") {
		return true
	}
	// fallback: cookie di sessione tipici di XFileSharing.
	u, _ := url.Parse("https://" + host + "/")
	for _, c := range r.jar.Cookies(u) {
		n := strings.ToLower(c.Name)
		if n == "xfss" || n == "xfsts" || n == "login" || strings.Contains(n, "sess") {
			return true
		}
	}
	return false
}

// Resolved è l'esito della risoluzione: il link diretto al file e, quando
// l'host lo espone, il suo vero nome (Name è "" se ignoto: in quel caso lo
// ricava il downloader dall'header Content-Disposition).
type Resolved struct {
	URL  string
	Name string
	Link string // URL della pagina-file originale, per rigenerare un link scaduto
}

// Resolve trasforma il link di una pagina-file nel link diretto al file.
func (r *Resolver) Resolve(link string) (Resolved, error) {
	u, err := url.Parse(link)
	if err != nil {
		return Resolved{}, fmt.Errorf("URL non valido: %w", err)
	}
	host := normHost(u.Host)
	if err := r.ensureLogin(host); err != nil {
		return Resolved{}, err
	}

	fp, err := r.openFilePage(link)
	if err != nil {
		return Resolved{}, fmt.Errorf("apertura pagina file: %w", err)
	}
	if !fp.isHTML {
		// L'host serve il file direttamente dall'URL (premium): lo passo al
		// downloader così com'è (rifarà il GET con i cookie di sessione,
		// seguendo gli stessi redirect). Il nome lo prendo dal
		// Content-Disposition, poi dall'URL finale, infine dall'URL originale.
		name := fp.cdName
		if name == "" {
			name = nameFromURLPath(fp.finalURL)
		}
		if name == "" {
			name = nameFromURLPath(link)
		}
		return Resolved{URL: link, Name: name, Link: link}, nil
	}
	page := fp.body
	r.dump("file_"+safeName(link)+"_1", page)

	if dl := extractDirectLink(page, host); dl != "" {
		return Resolved{URL: dl, Name: resolvedName(page, dl, link), Link: link}, nil
	}

	// Percorso a più step: invio il form di download (op=download1 → download2).
	// Da premium il countdown non c'è; se compare, l'account non è premium.
	cur, curURL := page, link
	for step := 2; step <= 4; step++ {
		if detectCountdown(cur) {
			return Resolved{}, fmt.Errorf("countdown rilevato: l'account non risulta premium su %s", host)
		}
		vals, action := extractDownloadForm(cur, curURL)
		if vals == nil {
			break
		}
		next, _, err := r.postForm(action, vals, curURL)
		if err != nil {
			return Resolved{}, fmt.Errorf("invio form download: %w", err)
		}
		r.dump(fmt.Sprintf("file_%s_%d", safeName(link), step), next)
		if dl := extractDirectLink(next, host); dl != "" {
			// Il nome può stare nella pagina corrente (next) o in quelle prima.
			name := resolvedName(next, dl, link)
			if name == "" {
				name = resolvedName(page, dl, link)
			}
			return Resolved{URL: dl, Name: name, Link: link}, nil
		}
		cur, curURL = next, action
	}

	if strings.Contains(strings.ToLower(cur), "premium") {
		return Resolved{}, fmt.Errorf("link diretto non trovato (forse file solo-premium, o resolver da rifinire: usa -debug)")
	}
	return Resolved{}, fmt.Errorf("link diretto non trovato (rifinisci il resolver con -debug)")
}

// resolvedName cerca il vero nome del file: prima nella pagina XFileSharing
// (campo nascosto fname o titolo), poi nel path del link diretto, infine in
// quello del link originale. Ritorna "" se nessuna fonte dà un nome con
// estensione plausibile (allora ci penserà il Content-Disposition).
func resolvedName(page, directURL, origURL string) string {
	if n := fileNameFromHTML(page); n != "" {
		return n
	}
	if n := nameFromURLPath(directURL); n != "" {
		return n
	}
	return nameFromURLPath(origURL)
}

// fileNameFromHTML estrae il nome del file dalla pagina-file XFileSharing.
// La fonte più affidabile è il campo nascosto fname del form di download;
// in mancanza prova il <title>/intestazione se contiene un nome con estensione.
func fileNameFromHTML(page string) string {
	if n := cleanFileName(extractHidden(page, "fname")); n != "" && path.Ext(n) != "" {
		return n
	}
	for _, re := range pageNameREs {
		if m := re.FindStringSubmatch(page); m != nil {
			if n := cleanFileName(m[1]); path.Ext(n) != "" {
				return n
			}
		}
	}
	return ""
}

var pageNameREs = []*regexp.Regexp{
	regexp.MustCompile(`(?is)<title>\s*(?:download[\s:]+)?([^\s<|]+\.[A-Za-z0-9]{1,8})`),
	regexp.MustCompile(`(?is)<h[1-4][^>]*>\s*([^\s<]+\.[A-Za-z0-9]{1,8})`),
	regexp.MustCompile(`(?is)<font[^>]*>\s*([^\s<]+\.[A-Za-z0-9]{1,8})`),
}

// nameFromURLPath ricava il nome dall'ultimo segmento del path, ma solo se ha
// un'estensione plausibile: un id opaco (es. "2xh7c3nwww38") viene scartato.
func nameFromURLPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	n, err := url.PathUnescape(path.Base(u.Path))
	if err != nil {
		n = path.Base(u.Path)
	}
	n = cleanFileName(n)
	if path.Ext(n) == "" {
		return ""
	}
	return n
}

// cleanFileName rende una stringa usabile come nome file: niente componenti di
// percorso, niente entità HTML, niente spazi ai bordi.
func cleanFileName(s string) string {
	s = strings.TrimSpace(html.UnescapeString(s))
	s = strings.ReplaceAll(s, "\\", "/")
	s = path.Base(s)
	s = strings.Trim(s, " \t\r\n.")
	if s == "" || s == "/" || s == ".." {
		return ""
	}
	return s
}

// ---- parsing HTML: euristiche XFileSharing, da rifinire su host reali ----

var (
	reInput    = regexp.MustCompile(`(?is)<input[^>]*>`)
	reFormFull = regexp.MustCompile(`(?is)(<form[^>]*>)(.*?)</form>`)

	// In ordine di preferenza. Il primo che matcha vince.
	directLinkREs = []*regexp.Regexp{
		regexp.MustCompile(`(?i)href="(https?://[^"]+/d/[A-Za-z0-9]+/[^"]+)"`),
		regexp.MustCompile(`(?i)id="?direct[_-]?link"?[^>]*>\s*<a[^>]+href="([^"]+)"`),
		regexp.MustCompile(`(?i)<a[^>]+href="(https?://[^"]+)"[^>]*class="[^"]*\bdownload\b[^"]*"`),
		regexp.MustCompile(`(?i)<a[^>]+class="[^"]*\bdownload\b[^"]*"[^>]+href="(https?://[^"]+)"`),
		regexp.MustCompile(`(?i)href="(https?://[^"]+\.(?:iso|zip|rar|7z|mkv|mp4|avi|tar|gz|tgz|bin|img|exe|pdf|epub|deb|rpm)[^"]*)"`),
	}
)

func extractHidden(body, name string) string {
	for _, in := range reInput.FindAllString(body, -1) {
		if strings.EqualFold(attr(in, "name"), name) {
			return attr(in, "value")
		}
	}
	return ""
}

// extractDownloadForm trova il primo <form> che contiene un campo name="op" e
// ne restituisce tutti gli input (name=value) più la action assoluta.
func extractDownloadForm(body, pageURL string) (url.Values, string) {
	for _, f := range reFormFull.FindAllStringSubmatch(body, -1) {
		open, inner := f[1], f[2]
		li := strings.ToLower(inner)
		if !strings.Contains(li, `name="op"`) && !strings.Contains(li, `name='op'`) {
			continue
		}
		vals := url.Values{}
		for _, in := range reInput.FindAllString(inner, -1) {
			name := attr(in, "name")
			if name == "" {
				continue
			}
			vals.Set(name, attr(in, "value"))
		}
		action := attr(open, "action")
		if action == "" {
			action = pageURL
		}
		return vals, resolveURL(pageURL, action)
	}
	return nil, ""
}

func extractDirectLink(body, host string) string {
	for _, re := range directLinkREs {
		if m := re.FindStringSubmatch(body); m != nil {
			link := html.UnescapeString(m[1])
			low := strings.ToLower(link)
			// scarta falsi positivi ovvi (asset di tema/CSS/JS).
			if strings.HasSuffix(low, ".css") || strings.HasSuffix(low, ".js") ||
				strings.Contains(low, "/themes/") || strings.Contains(low, "/images/") {
				continue
			}
			return link
		}
	}
	return ""
}

func detectCountdown(body string) bool {
	lb := strings.ToLower(body)
	return strings.Contains(lb, `id="countdown"`) ||
		strings.Contains(lb, "countdown_str") ||
		strings.Contains(lb, "please wait") ||
		strings.Contains(lb, "seconds to download") ||
		strings.Contains(lb, "wait_time")
}
