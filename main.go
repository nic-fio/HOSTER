// hoster — gestore di download multi-host da terminale, stile JDownloader.
//
// Legge un file di account (dominio:utente:password) e una lista di link, fa
// login una volta per host, risolve i link diretti (logica XFileSharing),
// scarica in parallelo con ripresa e può estrarre gli archivi al termine.
// Un singolo binario, senza dipendenze esterne.
package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sync"
)

// userAgent: identico tra login/resolve e download (la sessione può dipenderne).
const userAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0"

func main() {
	if wantsHelp(os.Args[1:]) {
		displayHelp()
		os.Exit(0)
	}

	var (
		linksFile = flag.String("i", "links", "file con i link da scaricare")
		acctFile  = flag.String("a", "account", "file degli account (dominio:utente:password)")
		outDir    = flag.String("d", "downloads", "cartella di destinazione")
		split     = flag.Int("s", 1, "connessioni per file")
		jobs      = flag.Int("j", 5, "file scaricati in parallelo")
		cont      = flag.Bool("c", false, "riprende i download interrotti")
		rateS     = flag.String("rate", "0", "limite banda complessivo (es. 2M)")
		timeout   = flag.Int("timeout", 60, "timeout di connessione/header (s)")
		retries   = flag.Int("retries", 5, "tentativi per download")
		retryWait = flag.Int("retry-wait", 2, "attesa tra i tentativi (s)")
		ua        = flag.String("U", userAgent, "User-Agent")
		insecure  bool

		dryRun   = flag.Bool("dry-run", false, "risolve e stampa i link diretti senza scaricare")
		debugDir = flag.String("debug", "", "salva l'HTML di ogni passo del resolver qui")

		pw          = flag.String("p", "", "password per gli archivi protetti")
		pwfile      = flag.String("pwfile", "passwords.txt", "file di password da provare")
		cleanup     = flag.Bool("cleanup", false, "elimina gli archivi dopo l'estrazione")
		extractOnly = flag.String("extract-only", "", "estrai solo questa cartella ed esci")

		verbose bool
		extract bool
		quietE  = flag.Bool("qe", false, "silenzioso: solo errori")
		quietA  = flag.Bool("qa", false, "silenzioso totale")
	)
	flag.BoolVar(&insecure, "k", false, "accetta host con certificato di sicurezza incompleto")
	flag.BoolVar(&insecure, "insecure", false, "accetta host con certificato di sicurezza incompleto")
	flag.BoolVar(&extract, "e", false, "estrai gli archivi al termine del download")
	flag.BoolVar(&extract, "extract", false, "estrai gli archivi al termine del download")
	flag.BoolVar(&verbose, "v", false, "log dettagliato")
	flag.BoolVar(&verbose, "verbose", false, "log dettagliato")
	flag.Usage = usage
	flag.Parse()

	// Modalità sola estrazione: non servono né account né link.
	if *extractOnly != "" {
		runExtraction(*extractOnly, *pw, *pwfile, *cleanup)
		return
	}

	rate, err := parseSize(*rateS)
	if err != nil {
		fatal("%v", err)
	}

	accts, err := LoadAccounts(*acctFile)
	if err != nil {
		fatal("account: %v", err)
	}
	links, err := LoadLinks(*linksFile)
	if err != nil {
		fatal("link: %v", err)
	}
	if len(links) == 0 {
		fatal("nessun link in %s", *linksFile)
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatal("cartella di destinazione: %v", err)
	}

	res, err := NewResolver(accts, *debugDir, insecure, *ua)
	if err != nil {
		fatal("resolver: %v", err)
	}

	// Modalità dry-run: risolve e stampa, niente progress né download.
	if *dryRun {
		ok, fail := 0, 0
		for _, link := range links {
			r, err := res.Resolve(link)
			if err != nil {
				fmt.Printf("✗ %s\n  %v\n", link, err)
				fail++
				continue
			}
			name := r.Name
			if name == "" {
				name = fileNameFromURL(r.URL)
			}
			fmt.Printf("✓ %s\n  %s\n", name, r.URL)
			ok++
		}
		fmt.Printf("\nrisolti: %d  falliti: %d\n", ok, fail)
		return
	}

	prog := NewProgress(*quietA || *quietE)
	gProg = prog
	go prog.run()

	dlClient := newDownloadClient(res.jar, *timeout, *split+*jobs, insecure)
	dl := NewDownloader(dlClient, DLConfig{
		Split:     *split,
		Jobs:      *jobs,
		MinSplit:  1 << 20,
		Continue:  *cont,
		Retries:   *retries,
		RetryWait: *retryWait,
		Rate:      rate,
		Timeout:   *timeout,
		UserAgent: *ua,
	}, prog)

	var okCount, failCount int
	seenDest := map[string]bool{} // evita due download verso lo stesso file (mirror/duplicati)
	for bi := 0; bi < len(links); bi += *jobs {
		end := min(bi+*jobs, len(links))
		chunk := links[bi:end]

		// Risolvo il lotto (i link diretti scadono: meglio "freschi" appena prima).
		var direct []Resolved
		for _, link := range chunk {
			r, err := res.Resolve(link)
			if err != nil {
				prog.Log(fmt.Sprintf(" %s %s  %v", col(prog.color, aRed, "✗"), filepath.Base(link), err))
				failCount++
				continue
			}
			direct = append(direct, r)
			if verbose {
				nm := r.Name
				if nm == "" {
					nm = fileNameFromURL(r.URL)
				}
				prog.Log(fmt.Sprintf(" %s %s  %s", col(prog.color, aCyan, "→"), nm, col(prog.color, aDim, r.URL)))
			}
		}
		if len(direct) == 0 {
			continue
		}

		// Scarico il lotto in parallelo.
		var wg sync.WaitGroup
		var mu sync.Mutex
		for _, r := range direct {
			name := r.Name
			if name == "" {
				name = fileNameFromURL(r.URL)
			}
			dest := filepath.Join(*outDir, name)
			if seenDest[dest] {
				prog.Log(fmt.Sprintf(" %s %s  (duplicato, ignorato)", col(prog.color, aYel, "•"), name))
				continue
			}
			seenDest[dest] = true
			wg.Add(1)
			go func(r Resolved, dest string) {
				defer wg.Done()
				// refresh rigenera il link diretto se scade durante il download.
				refresh := func() (string, error) {
					nr, err := res.Resolve(r.Link)
					if err != nil {
						return "", err
					}
					return nr.URL, nil
				}
				if err := dl.downloadFile(r.URL, dest, refresh); err != nil {
					prog.Log(fmt.Sprintf(" %s %s  %v", col(prog.color, aRed, "✗"), filepath.Base(dest), err))
					mu.Lock()
					failCount++
					mu.Unlock()
					return
				}
				mu.Lock()
				okCount++
				mu.Unlock()
			}(r, dest)
		}
		wg.Wait()
	}

	prog.finish()
	if !*quietA {
		fmt.Printf("scaricati: %d  falliti: %d\n", okCount, failCount)
	}

	if extract {
		runExtraction(*outDir, *pw, *pwfile, *cleanup)
	}
}

// fileNameFromURL ricava il nome file dall'URL (parte finale del path).
func fileNameFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "download.bin"
	}
	name := path.Base(u.Path)
	if dec, err := url.PathUnescape(name); err == nil {
		name = dec
	}
	if name == "" || name == "/" || name == "." {
		return "download.bin"
	}
	return name
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "hoster: "+format+"\n", a...)
	os.Exit(1)
}
