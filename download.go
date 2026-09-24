package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// maxRefresh è quante volte rigenerare un link diretto scaduto durante un singolo
// download prima di arrendersi.
const maxRefresh = 2

// httpError è uno stato HTTP inatteso, conservato per distinguere i link scaduti
// (403/410…) dai guasti di rete transitori.
type httpError struct{ code int }

func (e *httpError) Error() string { return fmt.Sprintf("HTTP %d", e.code) }

// isStaleLink riconosce gli stati tipici di un link diretto non più valido: vale
// la pena ri-risolvere il link originale invece di insistere sullo stesso URL.
func isStaleLink(err error) bool {
	var he *httpError
	if errors.As(err, &he) {
		switch he.code {
		case 401, 403, 404, 410:
			return true
		}
	}
	return false
}

// DLConfig raccoglie le opzioni del motore di download.
type DLConfig struct {
	Split     int   // connessioni per file
	Jobs      int   // file in parallelo (per dimensionare il pool di connessioni)
	MinSplit  int64 // soglia minima per segmentare
	Continue  bool  // riprende i download interrotti
	Retries   int
	RetryWait int
	Rate      int64 // limite banda globale (byte/s, 0 = illimitato)
	Timeout   int   // timeout di connessione/header (s)
	UserAgent string
}

// newDownloadClient costruisce un client che condivide il cookie jar (quindi la
// sessione) del resolver, ma SENZA timeout totale: un download multi-GB non deve
// essere interrotto da una scadenza complessiva. Si usano invece timeout su
// connessione, handshake e header.
func newDownloadClient(jar *cookiejar.Jar, timeout, maxConns int, forceInsecure bool) *http.Client {
	base := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: time.Duration(timeout) * time.Second}).DialContext,
		TLSHandshakeTimeout:   time.Duration(timeout) * time.Second,
		ResponseHeaderTimeout: time.Duration(timeout) * time.Second,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   maxConns,
	}
	return &http.Client{Transport: newTLSFallbackTransport(base, forceInsecure), Jar: jar}
}

// Downloader scarica file usando il client (con sessione) condiviso.
type Downloader struct {
	client *http.Client
	cfg    DLConfig
	rl     *RateLimiter
	prog   *Progress
}

func NewDownloader(client *http.Client, cfg DLConfig, prog *Progress) *Downloader {
	return &Downloader{client: client, cfg: cfg, rl: NewRateLimiter(cfg.Rate), prog: prog}
}

func (d *Downloader) newRequest(method, rawurl string) (*http.Request, error) {
	req, err := http.NewRequest(method, rawurl, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", d.cfg.UserAgent)
	return req, nil
}

type probeInfo struct {
	size   int64
	ranges bool
	ct     string
	name   string // nome dichiarato dal server (Content-Disposition), se c'è
}

// probe scopre dimensione e supporto ai range con una GET Range: bytes=0-0.
func (d *Downloader) probe(rawurl string) (*probeInfo, error) {
	req, err := d.newRequest("GET", rawurl)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
	}()
	info := &probeInfo{size: -1, ct: resp.Header.Get("Content-Type"), name: cdFileName(resp.Header.Get("Content-Disposition"))}
	switch resp.StatusCode {
	case http.StatusPartialContent: // 206
		info.ranges = true
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			if i := strings.LastIndex(cr, "/"); i >= 0 {
				if n, err := strconv.ParseInt(strings.TrimSpace(cr[i+1:]), 10, 64); err == nil {
					info.size = n
				}
			}
		}
	case http.StatusOK: // 200: niente range
		info.ranges = false
		if resp.ContentLength >= 0 {
			info.size = resp.ContentLength
		}
	case http.StatusRequestedRangeNotSatisfiable: // 416: file vuoto
		info.size = 0
	default:
		return nil, &httpError{resp.StatusCode}
	}
	return info, nil
}

// downloadFile salva una risorsa su dest (multi-segmento se possibile). Se il
// link diretto scade (403/410…) e refresh è fornito, rigenera il link e riprova.
func (d *Downloader) downloadFile(rawurl, dest string, refresh func() (string, error)) error {
	if st, err := os.Stat(dest); err == nil && !d.cfg.Continue {
		_ = st
		return nil // esiste già, niente da fare
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}

	// reResolve rigenera il link diretto quando è scaduto; ritorna false se non
	// è possibile (nessun refresh, tentativi esauriti, o errore di risoluzione).
	refreshes := 0
	reResolve := func(cause error) bool {
		if refresh == nil || refreshes >= maxRefresh || !isStaleLink(cause) {
			return false
		}
		nu, err := refresh()
		if err != nil || nu == "" {
			return false
		}
		refreshes++
		rawurl = nu
		notef("nota: link diretto scaduto, lo rigenero (%s)", filepath.Base(dest))
		return true
	}

	for {
		info, err := d.probe(rawurl)
		if err != nil {
			if reResolve(err) {
				continue
			}
			return err
		}
		// Se il nome ricavato finora non ha estensione (tipico dei link con id
		// opaco, es. host stile "filestore"), uso quello dichiarato dal server
		// nell'header Content-Disposition: è il vero nome del file.
		if info.name != "" && filepath.Ext(filepath.Base(dest)) == "" {
			dest = filepath.Join(filepath.Dir(dest), info.name)
		}
		part := dest + ".part"
		useSeg := info.ranges && info.size >= d.cfg.MinSplit && d.cfg.Split > 1 && info.size > 0

		fp := d.prog.startFile(dest, info.size)
		if useSeg {
			err = d.segmented(rawurl, dest, part, info.size, fp)
		} else {
			err = d.singleStream(rawurl, dest, part, info, fp)
		}
		if err != nil && reResolve(err) {
			d.prog.cancelFile(fp) // ritentato con un link fresco: non conta come fallimento
			continue
		}
		d.prog.finishFile(fp, err == nil)
		return err
	}
}

type seg struct {
	start, end int64
	done       int64 // atomico
}

type segMeta struct {
	URL  string  `json:"url"`
	Size int64   `json:"size"`
	Done []int64 `json:"done"`
}

func makeSegs(size int64, n int) []*seg {
	segs := make([]*seg, n)
	chunk := size / int64(n)
	for i := 0; i < n; i++ {
		s := int64(i) * chunk
		e := s + chunk - 1
		if i == n-1 {
			e = size - 1
		}
		segs[i] = &seg{start: s, end: e}
	}
	return segs
}

func (d *Downloader) segmented(rawurl, dest, part string, size int64, fp *fileProg) error {
	segs := makeSegs(size, d.cfg.Split)
	n := len(segs)
	f, err := os.OpenFile(part, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if err := f.Truncate(size); err != nil {
		f.Close()
		return err
	}

	metaPath := part + ".hoster"
	if d.cfg.Continue {
		if data, err := os.ReadFile(metaPath); err == nil {
			var m segMeta
			if json.Unmarshal(data, &m) == nil && m.Size == size && len(m.Done) == n {
				var tot int64
				for i := range segs {
					atomic.StoreInt64(&segs[i].done, m.Done[i])
					tot += m.Done[i]
				}
				atomic.StoreInt64(&fp.done, tot)
			}
		}
	}

	var metaMu sync.Mutex
	flush := func() {
		m := segMeta{URL: rawurl, Size: size, Done: make([]int64, n)}
		for i := range segs {
			m.Done[i] = atomic.LoadInt64(&segs[i].done)
		}
		data, _ := json.Marshal(m)
		metaMu.Lock()
		os.WriteFile(metaPath, data, 0o644)
		metaMu.Unlock()
	}

	done := make(chan struct{})
	go func() {
		t := time.NewTicker(1500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				flush()
			}
		}
	}()

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := segs[i]
			errs[i] = d.withRetry(func() error {
				from := s.start + atomic.LoadInt64(&s.done)
				if from > s.end {
					return nil
				}
				return d.fetchRange(rawurl, f, from, s.end, func(nb int) {
					atomic.AddInt64(&s.done, int64(nb))
					atomic.AddInt64(&fp.done, int64(nb))
				})
			})
		}(i)
	}
	wg.Wait()
	close(done)
	f.Close()

	for _, er := range errs {
		if er != nil {
			flush()
			return er
		}
	}
	os.Remove(metaPath)
	return os.Rename(part, dest)
}

func (d *Downloader) fetchRange(rawurl string, f *os.File, from, end int64, cb func(int)) error {
	req, err := d.newRequest("GET", rawurl)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", from, end))
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return &httpError{resp.StatusCode}
	}
	r := &meterReader{r: resp.Body, lim: d.rl}
	buf := make([]byte, 64*1024)
	off := from
	for {
		nr, er := r.Read(buf)
		if nr > 0 {
			if _, ew := f.WriteAt(buf[:nr], off); ew != nil {
				return ew
			}
			off += int64(nr)
			cb(nr)
		}
		if er == io.EOF {
			return nil
		}
		if er != nil {
			return er
		}
	}
}

func (d *Downloader) singleStream(rawurl, dest, part string, info *probeInfo, fp *fileProg) error {
	f, err := os.OpenFile(part, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}

	// Offset di partenza: riprende dal .part esistente solo se l'host supporta i
	// range e il file non è già completo; altrimenti riparte da zero.
	var start int64
	if d.cfg.Continue && info.ranges {
		if st, e := f.Stat(); e == nil && (info.size <= 0 || st.Size() < info.size) {
			start = st.Size()
		}
	}
	if start == 0 {
		if err := f.Truncate(0); err != nil {
			f.Close()
			return err
		}
	}
	atomic.StoreInt64(&fp.done, start)

	err = d.withRetry(func() error {
		// A ogni tentativo riparto dall'offset realmente scritto finora: con i
		// range riprendo da lì (niente duplicazioni); senza range devo
		// ricominciare da capo, quindi tronco il file.
		from := atomic.LoadInt64(&fp.done)
		if from > 0 && !info.ranges {
			if err := f.Truncate(0); err != nil {
				return err
			}
			from = 0
			atomic.StoreInt64(&fp.done, 0)
		}

		req, err := d.newRequest("GET", rawurl)
		if err != nil {
			return err
		}
		if from > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", from))
		}
		resp, err := d.client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return &httpError{resp.StatusCode}
		}
		// Se ho chiesto un range ma il server risponde 200 (lo ignora), il corpo
		// riparte da 0: scrivo dall'inizio per non sfasare i byte.
		off := from
		if from > 0 && resp.StatusCode == http.StatusOK {
			off = 0
		}
		atomic.StoreInt64(&fp.done, off)

		r := &meterReader{r: resp.Body, lim: d.rl}
		buf := make([]byte, 64*1024)
		for {
			nr, er := r.Read(buf)
			if nr > 0 {
				if _, ew := f.WriteAt(buf[:nr], off); ew != nil {
					return ew
				}
				off += int64(nr)
				atomic.StoreInt64(&fp.done, off)
			}
			if er == io.EOF {
				// rifiuta un troncamento silenzioso: se conosco la dimensione e
				// non l'ho raggiunta, è un download incompleto (ritenta).
				if info.size > 0 && off != info.size {
					return fmt.Errorf("download incompleto: %d/%d byte", off, info.size)
				}
				return nil
			}
			if er != nil {
				return er
			}
		}
	})
	if cerr := f.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(part, dest)
}

func (d *Downloader) withRetry(fn func() error) error {
	var err error
	for attempt := 0; attempt <= d.cfg.Retries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(d.cfg.RetryWait) * time.Second)
		}
		if err = fn(); err == nil {
			return nil
		}
		if isStaleLink(err) {
			return err // inutile ritentare lo stesso URL scaduto: lo gestisce chi ri-risolve
		}
	}
	return err
}

// cdFileName estrae il nome file da un header Content-Disposition. Gestisce sia
// la forma classica filename="..." sia quella estesa RFC 5987 filename*=UTF-8”...
func cdFileName(cd string) string {
	if cd == "" {
		return ""
	}
	if _, params, err := mime.ParseMediaType(cd); err == nil {
		if v := params["filename*"]; v != "" {
			return cleanFileName(v)
		}
		if v := params["filename"]; v != "" {
			return cleanFileName(v)
		}
	}
	// Fallback grezzo per header malformati che mime.ParseMediaType rifiuta.
	if m := reCDFilename.FindStringSubmatch(cd); m != nil {
		val := strings.Trim(m[1], `"`)
		val = strings.TrimPrefix(val, "UTF-8''")
		val = strings.TrimPrefix(val, "utf-8''")
		if dec, err := url.QueryUnescape(val); err == nil {
			val = dec
		}
		return cleanFileName(val)
	}
	return ""
}

var reCDFilename = regexp.MustCompile(`(?i)filename\*?\s*=\s*([^;]+)`)
