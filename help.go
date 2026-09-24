package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// codici ANSI
const (
	aReset = "\033[0m"
	aBold  = "\033[1m"
	aDim   = "\033[2m"
	aUnder = "\033[4m"
	aCyan  = "\033[36m"
	aGreen = "\033[32m"
	aYel   = "\033[33m"
	aRed   = "\033[31m"
)

type helpRow struct{ flag, desc string }
type helpSection struct {
	title string
	rows  []helpRow
}

var helpSections = []helpSection{
	{"Download", []helpRow{
		{"-i FILE", "File con i link da scaricare, uno per riga (default links)."},
		{"-a FILE", "File degli account: dominio:utente:password (default account)."},
		{"-d DIR", "Cartella di destinazione (default downloads)."},
		{"-s N", "Connessioni per file (default 1: i premium ne danno una)."},
		{"-j N", "File scaricati in parallelo (default 5)."},
		{"-c", "Riprende i download interrotti da dove si erano fermati."},
		{"--rate SIZE", "Limita la banda complessiva (es. 2M)."},
		{"--timeout SEC", "Timeout di connessione/header (default 60)."},
		{"--retries N", "Tentativi per ciascun download (default 5)."},
		{"--retry-wait SEC", "Attesa tra un tentativo e il successivo (default 2)."},
		{"-U UA", "User-Agent usato per login e download."},
		{"-k, --insecure", "Forza il salto della verifica TLS (di norma non serve: hoster si adatta da solo agli host con catena di certificati incompleta)."},
	}},
	{"Estrazione", []helpRow{
		{"-e, --extract", "Estrae gli archivi RAR/ZIP al termine del download."},
		{"--extract-only DIR", "Estrae solo gli archivi presenti in DIR ed esce."},
		{"-p PASS", "Password per gli archivi protetti."},
		{"--pwfile FILE", "File di password da provare in sequenza (default passwords.txt)."},
		{"--cleanup", "Elimina gli archivi dopo un'estrazione riuscita."},
	}},
	{"Diagnostica", []helpRow{
		{"--dry-run", "Risolve e stampa i link diretti senza scaricare."},
		{"--debug DIR", "Salva l'HTML di ogni passo del resolver in DIR."},
		{"-v, --verbose", "Log più dettagliato."},
		{"-qe", "Silenzioso: nessun output tranne gli errori."},
		{"-qa", "Silenzioso totale."},
		{"-h, --help", "Mostra questa guida (a schede nel terminale)."},
	}},
}

// hostsTested sono i siti su cui hoster è stato verificato sul campo.
var hostsTested = map[string]bool{
	"terabytez.org": true,
	"filestore.me":  true,
}

// supportedHosts elenca i file hoster noti della famiglia XFileSharing. Per
// usarne uno basta aggiungere la riga corrispondente nel file 'account'.
var supportedHosts = []string{
	"terabytez.org",
	"filestore.me",
	"worldbytez.com",
	"ddownload.com",
	"clicknupload.to",
	"dropapk.to",
	"send.cm",
	"userupload.net",
	"usersdrive.com",
	"uploadrar.com",
	"file-upload.org",
}

type helpExample struct{ cmd, note string }

var helpExamples = []helpExample{
	{"hoster", "Scarica i link di 'links' in ./downloads usando gli account di 'account'."},
	{"hoster -i kali.txt -d /media/iso", "Usa una lista e una cartella di destinazione diverse."},
	{"hoster -dry-run", "Risolve i link diretti senza scaricare (verifica)."},
	{"hoster -e -p LAPASSWORD", "Scarica e poi estrae gli archivi con la password indicata."},
	{"hoster -extract-only downloads -p PW", "Estrae una cartella già scaricata."},
}

const descrizione = "hoster è un gestore di download multi-host da terminale, in stile " +
	"JDownloader: legge un file di account e una lista di link, fa login una volta per host, " +
	"risolve i link diretti (logica XFileSharing), scarica in parallelo con ripresa e può " +
	"estrarre gli archivi RAR/ZIP al termine. Un singolo binario, senza dipendenze esterne."

func col(color bool, code, s string) string {
	if !color {
		return s
	}
	return code + s + aReset
}

func threeCol(left, mid, right string, width int) string {
	if width < len(left)+len(mid)+len(right)+2 {
		width = len(left) + len(mid) + len(right) + 2
	}
	gapTot := width - len(left) - len(mid) - len(right)
	l := gapTot / 2
	r := gapTot - l
	return left + strings.Repeat(" ", l) + mid + strings.Repeat(" ", r) + right
}

func wrapText(s string, width, indent int) []string {
	pad := strings.Repeat(" ", indent)
	avail := width - indent
	if avail < 24 {
		avail = 24
	}
	var lines []string
	cur := ""
	for _, w := range strings.Fields(s) {
		switch {
		case cur == "":
			cur = w
		case len(cur)+1+len(w) <= avail:
			cur += " " + w
		default:
			lines = append(lines, pad+cur)
			cur = w
		}
	}
	if cur != "" {
		lines = append(lines, pad+cur)
	}
	return lines
}

func buildHelp(color bool, width int) string {
	if width < 60 {
		width = 60
	}
	if width > 100 {
		width = 100
	}
	const ti = 7
	const di = 14

	var b strings.Builder
	w := func(s string) { b.WriteString(s) }
	hdr := func(s string) { w("\n" + col(color, aBold, strings.ToUpper(s)) + "\n") }
	sub := func(s string) { w("\n" + strings.Repeat(" ", ti) + col(color, aBold+aYel, s) + "\n") }
	para := func(s string, ind int) {
		for _, l := range wrapText(s, width, ind) {
			w(l + "\n")
		}
	}
	tp := func(tag, desc string) {
		w(strings.Repeat(" ", ti) + col(color, aBold+aGreen, tag) + "\n")
		para(desc, di)
	}

	w(col(color, aBold, threeCol("HOSTER(1)", "Manuale di hoster", "HOSTER(1)", width)) + "\n")

	hdr("Nome")
	para("hoster — gestore di download multi-host (XFileSharing)", ti)

	hdr("Sintassi")
	w(strings.Repeat(" ", ti) + col(color, aBold, "hoster") + " " + col(color, aDim, "[opzioni]") + "\n")

	hdr("Descrizione")
	para(descrizione, ti)

	hdr("Opzioni")
	for _, sec := range helpSections {
		sub(sec.title)
		for _, r := range sec.rows {
			tp(r.flag, r.desc)
		}
	}

	hdr("Host supportati")
	para("File hoster della famiglia XFileSharing. Per usarne uno, aggiungi la riga corrispondente nel file 'account'.", ti)
	w("\n")
	for _, h := range supportedHosts {
		line := h
		if hostsTested[h] {
			line += "  (testato)"
		}
		w(strings.Repeat(" ", ti) + col(color, aGreen, "•") + " " + col(color, aBold, line) + "\n")
	}

	hdr("Esempi")
	for _, ex := range helpExamples {
		tp(ex.cmd, ex.note)
	}

	hdr("File")
	tp("account", "Credenziali: una riga per host, dominio:utente:password.")
	tp("links", "Link da scaricare, uno per riga.")
	tp("passwords.txt", "Password da provare per gli archivi (una per riga).")
	tp("FILE.part / .part.hoster", "File parziale e metadati di ripresa (--continue).")

	hdr("Stato di uscita")
	tp("0", "Completato.")
	tp("1", "Errore durante l'esecuzione.")
	tp("2", "Argomenti non validi.")

	w("\n" + col(color, aBold, threeCol("hoster 1.0", "", "HOSTER(1)", width)) + "\n")
	return b.String()
}

var reANSI = regexp.MustCompile(`\033\[[0-9;]*m`)

func stripANSI(s string) string { return reANSI.ReplaceAllString(s, "") }

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func termWidth() int {
	w, _ := termSize()
	if w <= 0 {
		return 80
	}
	return w
}

// wantsHelp riconosce le richieste d'aiuto (e il caso "nessun argomento" no:
// hoster ha default sensati, quindi solo i flag espliciti).
func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "--" {
			break
		}
		switch a {
		case "-h", "-help", "--help", "help":
			return true
		}
	}
	return false
}

func usage() {
	fmt.Fprint(os.Stderr, stripANSI(buildHelp(false, 80)))
}

// displayHelp mostra la guida: TUI a schede nel terminale, altrimenti pager o
// testo semplice.
func displayHelp() {
	if !isTTY(os.Stdout) || !isTTY(os.Stdin) {
		fmt.Print(stripANSI(buildHelp(false, 80)))
		return
	}
	if err := runTUI(); err == nil {
		return
	}
	color := os.Getenv("NO_COLOR") == ""
	text := buildHelp(color, termWidth())
	cmd := pagerCommand()
	if cmd == nil {
		fmt.Print(text)
		return
	}
	cmd.Stdin = strings.NewReader(text)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Print(text)
	}
}

func pagerCommand() *exec.Cmd {
	if p := os.Getenv("PAGER"); p != "" {
		return exec.Command("sh", "-c", p)
	}
	if p, err := exec.LookPath("less"); err == nil {
		return exec.Command(p, "-R", "-F", "-X")
	}
	if p, err := exec.LookPath("more"); err == nil {
		return exec.Command(p)
	}
	return nil
}
