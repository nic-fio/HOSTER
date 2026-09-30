# hoster — decisioni e storia

> Perché hoster è fatto così: il contesto, le decisioni e la storia del progetto.
> Il funzionamento in dettaglio è nel [manuale tecnico](https://nic-fio.github.io/HOSTER/Technical%20Manual.html) (in inglese).
> Stato: **completo e collaudato** (giugno 2026). Uso strettamente **personale**:
> non viene distribuito, serve a scaricare con i propri account contenuti che si
> è autorizzati a scaricare (es. distribuzioni Linux). Non implementa né intende
> implementare aggiramenti (captcha, countdown): usa solo il percorso premium
> autenticato.

---

## 1. Cos'è

`hoster` è un gestore di download multi-host da terminale, in stile **JDownloader
ma senza Java**: un **singolo binario statico Go, zero dipendenze esterne**.

Flusso: legge due file (`account` e `links`), fa **login una volta per host**,
**risolve il link diretto** di ogni file (logica XFileSharing), **scarica in
parallelo con ripresa** e, su richiesta, **estrae** gli archivi RAR/ZIP.

```
account + links → login per host → risoluzione link diretto
               → download parallelo (ripresa) → [estrazione opzionale]
```

## 2. Contesto e genesi

- Nasce dal problema: i file hoster (worldbytez, terabytez, …) richiedono login
  e poi click manuale su ogni link → lento e noioso. `wget`/`aria2` non
  bastano perché serve la sessione autenticata.
- Sono tutti cloni di **XFileSharing Pro** (stesso script lato server) → un
  resolver generico li copre quasi tutti. JDownloader fa lo stesso (ne è la
  prova: ha un plugin `WorldBytezCom` che estende `XFileSharingProBasic`).
- Progetto gemello: **`scrap`** — download manager + crawler generico, dello
  stesso autore. Decisione presa:
  **`scrap` resta intoccato**; il suo motore di download e lo stile dell'help a
  schede sono stati **re-implementati dentro `hoster`** (copia rivista), e i
  flag della CLI sono stati **armonizzati** con quelli di `scrap`.

## 3. Architettura (file Go, `package main`)

| File | Ruolo |
|---|---|
| `main.go` | CLI/flag, intercetta `--help`, `-dry-run`, loop a lotti (risolve `-j` link, li scarica in parallelo), avvio estrazione, `fileNameFromURL` |
| `accounts.go` | Parser file account + indice per host e per etichetta (`terabytez` ↔ `terabytez.org`) |
| `links.go` | Parser link (dedup, salta vuote/`#`) |
| `resolver.go` | **Cuore**: login XFileSharing + risoluzione link diretto + dump di debug |
| `download.go` | Motore di download in-process: client condiviso, probe, segmentazione, ripresa, retry, rate-limit |
| `progress.go` | Barra di avanzamento **singola per file** + riepilogo |
| `extract.go` | Estrazione integrata (RAR via `rardecode`, ZIP via stdlib) + fallback esterno `rar`/`7z` |
| `util.go` | Helper: `normHost`, `labelOf`, parsing HTML, `parseSize`, `RateLimiter`, `meterReader` |
| `help.go` | Pagina man (ANSI), sezioni dell'help, lista host, scelta TUI/pager/testo |
| `tui.go` | Guida `--help` come TUI a schede (raw mode termios) |
| `tls.go` | Verifica TLS con ripiego automatico per gli host con catena di certificati incompleta (§12) |
| `term_linux.go` / `term_other.go` | Primitive termios/ioctl/segnali per piattaforma (copiate da `scrap`) |
| `go.mod` | `module hoster`; unica dipendenza: `github.com/nwaples/rardecode/v2` |

File a runtime: `account`, `links`, `passwords.txt` (facoltativo), `downloads/`,
`*.part` + `*.part.hoster` (metadati di ripresa).

## 4. Come funziona, nel dettaglio

### Account (`accounts.go`)
Formato: `dominio<sep>utente<sep>password`, con `<sep>` = `:` oppure `;`. I primi
due separatori delimitano dominio e utente; la password è tutto il resto (può
contenere `:`/`;`). Il match dominio è per **etichetta** (parte prima del primo
punto), così `terabytez` combacia con `terabytez.org`.

### Resolver (`resolver.go`) — logica XFileSharing
1. **Login** (una volta per host, in cache): `GET /?op=login` per cookie iniziali
   ed eventuale token CSRF, poi `POST /` con `op=login,login,password,redirect[,token]`.
   Verifica euristica (`looksLoggedIn`: cerca "logout"/"my account" o cookie di
   sessione tipo `xfss`/`xfsts`).
2. **Risoluzione** (`Resolve`): apre l'URL della pagina-file e guarda il
   `Content-Type` **senza scaricare il corpo** (`openFilePage`):
   - **Non HTML / attachment** → l'host serve il file direttamente (caso premium
     tipico, confermato su terabytez): ritorna il link così com'è.
   - **HTML** → percorso a due passi: invia il form di download
     (`op=download1` → `op=download2`), estrae il link diretto
     (`extractDirectLink`), rileva eventuale `countdown` (se compare l'account
     non è premium).
3. **Nome del file** (`Resolve` ritorna `Resolved{URL, Name}`): non tutti gli
   host mettono il nome nell'URL. terabytez sì (`/id/nome.rar`); altri (stile
   **filestore**) danno un link diretto con solo un id opaco (`/2xh7c3nwww38`),
   che finiva come nome del file scaricato. Il nome vero si prende, in ordine:
   campo nascosto `fname` del form / `<title>` della pagina (`fileNameFromHTML`),
   poi il path del link se ha un'estensione (`nameFromURLPath`), infine — a
   download avviato — l'header `Content-Disposition` del server (`cdFileName` in
   `download.go`, override solo se il nome corrente è senza estensione). È
   host-agnostico e sistema anche l'estrazione, che cerca i volumi per nome.
4. **Debug**: con `--debug DIR` salva l'HTML di ogni passo per rifinire le regex.

### Download (`download.go`)
- Client HTTP che **condivide il cookie jar** del resolver (stessa sessione), con
  timeout su connessione/handshake/header **ma senza timeout totale** (un download
  multi-GB non deve scadere).
- `probe` con `Range: bytes=0-0` per dimensione e supporto ai range.
- `singleStream` (default, 1 connessione = premium) o `segmented` (se `-s > 1`).
- **Ripresa**: file `.part` + sidecar `.part.hoster` con gli offset; con `-c`
  riprende da dove era. Retry per tentativo: il single-stream riprende
  dall'offset realmente scritto (scrittura posizionale `WriteAt`, niente
  accodamento/duplicazioni) e, a fine stream, verifica la dimensione attesa
  (un troncamento del server non passa più come successo).
- **Robustezza**: link diretto scaduto (403/410…) → ri-risoluzione automatica dal
  link originale (campo `Resolved.Link`, fino a 2 volte, `httpError`/`isStaleLink`);
  più download verso lo **stesso file** deduplicati per destinazione (mirror
  saltati); `-U` ora vale anche per login/resolver (UA coerente con il download);
  login protetto da mutex perché le ri-risoluzioni avvengono in parallelo.

### Progress (`progress.go`)
Una sola barra per file: `⬇ nome  ▕████░░░▏ 68% 748M/1.1G 92M/s 4s`. Footer con
"N in corso · M completati". Riepilogo finale. Fuori dal terminale → righe semplici.

### Estrazione (`extract.go`)
- **Integrata** (default, niente tool esterno): RAR via `rardecode` (multi-volume,
  cifrato), ZIP via `archive/zip`.
- **Fallback** automatico a `rar`/`unrar`/`7z` esterni **se presenti**, per i casi
  che il decoder Go non copre (riparazione `.rev`, RAR esotici).
- Password provate in ordine: **nessuna → `-p` → quelle di `passwords.txt`**.
- Trova il volume principale del set (`part01`/`.rar`/`.zip`), `safeJoin` previene
  il path-traversal, `--cleanup` elimina gli archivi dopo il successo.

## 5. Riga di comando (flag armonizzati con `scrap`)

**Download:** `-i` link · `-a` account · `-d` cartella (default `downloads`) ·
`-s` conn/file (1) · `-j` paralleli (5) · `-c` riprendi · `--rate` · `--timeout`
(60) · `--retries` (5) · `--retry-wait` (2) · `-U` user-agent · `-k`/`--insecure`
(forza il salto della verifica TLS; di norma non serve, vedi §13).

**Estrazione:** `-e`/`--extract` · `--extract-only DIR` · `-p` password ·
`--pwfile` (default `passwords.txt`) · `--cleanup`.

**Diagnostica:** `--dry-run` · `--debug DIR` · `-v` · `-qe` · `-qa` · `-h`/`--help`.

Esempi:
```sh
./hoster                       # scarica i link di 'links' in ./downloads
./hoster -e -p LAPASSWORD      # scarica e poi estrae
./hoster -dry-run              # risolve e stampa i link diretti
./hoster -extract-only downloads -p PW
./hoster --help                # guida a schede (TUI)
```

## 6. Cosa è stato verificato

- Build statico + `go vet` + `gofmt` puliti.
- **Suite di test**: 47 test offline (ermetici, con `-race`), inclusi i casi
  critici — naming filestore (id opaco → `Content-Disposition`), ripresa dopo
  caduta di connessione, link scaduto → ri-risoluzione, fallback TLS, zip-slip.
- **Verifica dal vivo** (account reale, `LISTA_LINKS`): risoluzione 36/36 link su
  **terabytez.org** e **filestore.me**; download dell'intero set Kali (15 volumi,
  ~18 GB) **senza `-k`** (auto-fallback TLS) ed estrazione integrata → immagine
  VirtualBox valida, **senza `rar` installato** né password.
- **Estrazione integrata** (rardecode, multi-volume): contenuto corretto su set
  reale. Confermata anche dall'utente.
- Help: copertura 100% dei flag (controllo automatico), nessun flag fantasma.

## 7. Decisioni di design (e perché)

- **Binario unico, zero dipendenze**: distribuzione/uso banali; coerente con la
  filosofia di `scrap`. → motore integrato + estrazione integrata.
- **Niente sottoprocesso `scrap`**: progresso via callback (pulito) invece di
  parsing di testo.
- **Output a barra singola**: i premium danno 1 connessione per file, quindi la
  vista multi-segmento di `scrap` non serviva.
- **Estrazione integrata + fallback**: autonoma di default, completa quando può.
- **Solo percorso premium autenticato**: niente captcha/countdown da aggirare
  (scelta etica e di robustezza).
- **`scrap` intoccato**: resta un prodotto a sé.

## 8. Host supportati

Famiglia **XFileSharing**. Testati: **terabytez.org** e **filestore.me**. Attesi
compatibili (stesso script): worldbytez.com, ddownload.com, clicknupload.to,
dropapk.to, send.cm, userupload.net, usersdrive.com, uploadrar.com,
file-upload.org. Per aggiungerne uno basta la riga in `account`.

## 9. Come rifinire/aggiungere un host

1. Aggiungi l'account in `account` e un link in `links`.
2. `./hoster -dry-run -debug dbg` → guarda gli HTML in `dbg/`:
   - `login_<host>_1page.html` / `_2result.html` → il login passa?
   - `file_*_1.html`, `_2.html`… → dov'è il link diretto?
3. Adegua, se serve, le euristiche in `resolver.go`:
   `extractDirectLink` (regex `directLinkREs`), `extractDownloadForm`,
   `looksLoggedIn`, `detectCountdown`.

## 10. Build

```sh
make            # = CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o hoster .
make test       # vet, gofmt, test con -race, controlli dei manuali
```
Richiede Go 1.24 (testato con 1.24.4). La prima build scarica `rardecode`.

## 11. Idee per il futuro (TODO se si riprende)

- [x] `Makefile` + binari Linux amd64/arm64 (`make dist`, e la CI a ogni tag).
- [ ] Binari anche per macOS/Windows (la guida a schede lì ripiega sul testo).
- [ ] Verificare gli altri host della lista (promuoverli da "atteso" a "testato").
- [ ] Riparazione automatica con i `.rev` quando una parte è corrotta (serve `rar`).
- [ ] Limiti di concorrenza per-host configurabili (alcuni host cappano i paralleli).
- [ ] Output JSON dello stato (per integrazioni/script).
- [ ] Coda persistente su disco (riprendere l'intera sessione dopo una chiusura).

## 12. TLS: fallback automatico per catene incomplete

Alcuni nodi di download hoster servono una **catena di certificati incompleta**
(solo la foglia, senza l'intermedio): i browser perdonano, la libreria TLS di Go
no (`x509: certificate signed by unknown authority`). Confermato su terabytez
(`fsNN.terabytez.org:183`, GlobalSign, 1 solo certificato in catena) — login e
risoluzione passano, casca solo l'handshake col nodo file.

`tls.go` (`tlsFallbackTransport`) gestisce il caso **da solo**: prova con verifica
e, **solo** se l'errore è "autorità sconosciuta" (catena incompleta), ripiega
senza verifica **per quel singolo host**, stampando una nota; gli host a posto
(es. filestore) restano verificati. La deroga è mirata: scadenza o hostname
errato restano rifiutati. `-k`/`--insecure` resta come forzatura manuale, ma
**non è necessario ricordarsene**. Verificato sul campo: set Kali completo (15
volumi, ~18 GB) scaricato da terabytez senza `-k` ed estratto (VM VirtualBox
valida).

## 13. Note

- Mantenere i testi rivolti all'utente **senza gergo da programmatori**
  (preferenza esplicita): help e messaggi come "copy di prodotto", non note
  tecniche.

## 14. Storia

| Quando | Cosa |
|---|---|
| giugno 2026 | Nasce hoster: resolver XFileSharing, motore di download ripreso da `scrap`, barra per file, estrazione integrata. |
| giugno 2026 | Verifica dal vivo su terabytez.org e filestore.me: 36/36 link risolti, set Kali (15 parti, ~18 GB) scaricato ed estratto. Correzioni nate dal campo: nome da `Content-Disposition` per i link con id opaco, ripresa senza duplicazioni dopo una caduta di connessione, rigenerazione dei link scaduti, ripiego TLS automatico. |
| 24 settembre 2026 | Migrazione nel repository pubblico **nic-fio/HOSTER**, perché un `git clone` basti a recuperare tutto se il tablet si guasta. Prima il progetto esisteva in una sola copia locale. Aggiunti: manuale utente e manuale tecnico (HTML, GitHub Pages, sul modello di NG-EFI_SHELL), `CLAUDE.md`, `Makefile`, `tools/setup-dev.sh`, `tools/backup.sh`, `tools/check-docs.py`, CI con release dei binari. |

### Scelte fatte alla migrazione

- **Credenziali fuori dal repository, del tutto.** Il file `account` contiene
  password vere; in un repository pubblico non può stare in chiaro. Tra le
  alternative (file cifrato nel repository, gist privato, esclusione)
  l'utente ha scelto l'esclusione: dopo un clone `account` si riscrive a mano
  partendo da `account.example`. `tools/backup.sh --personal` ne fa una copia
  locale per chi vuole tenerla su una chiavetta.
- **`LISTA_LINKS` esclusa**: link personali pubblicati possono essere
  segnalati e rimossi dal sito che li ospita.
- **Manuali in italiano**, come l'interfaccia del programma.
- **Nessuna licenza**: il codice è visibile, tutti i diritti restano riservati.
- **L'eseguibile nel repository** (richiesta dell'utente, subito dopo la
  pubblicazione): `./hoster` per Linux x86-64 è registrato, così dopo un
  `git clone` il programma è pronto anche su una macchina senza Go. `make` lo
  rigenera; la versione per ARM resta nelle release.

### Manuali nello stile IR (30 settembre 2026)

Su richiesta dell'utente i due manuali sono stati riscritti con stile,
struttura e palette dei manuali di un altro suo progetto (IR), perché la
documentazione dei suoi progetti abbia un aspetto unico.

- **Nomi come i modelli**: `HOSTER_Manuale_Utente.html` e
  `HOSTER_Manuale_Tecnico.html`. I vecchi indirizzi `manuale-*.html` non
  esistono più.
- **File unici**: ogni manuale contiene il proprio stile e il proprio
  script. Spariscono `docs/assets` e la copia di Mermaid; i diagrammi sono
  disegnati direttamente nella pagina.
- **Ricerca e indice analitico conservati**: i modelli IR non li hanno, ma
  l'utente ha scelto di tenerli, integrati nell'aspetto IR.
- **Niente logo**: hoster non ne ha uno. In copertina c'è solo il nome.

### Manuali in inglese (30 settembre 2026)

Decisione del proprietario: i due manuali sono tradotti in inglese e
rinominati `docs/User Manual.html` e `docs/Technical Manual.html` (negli
indirizzi lo spazio diventa `%20`).

- **Solo i manuali**: il programma (messaggi, guida integrata), il codice, i
  commenti e gli altri documenti restano in italiano.
- **Messaggi citati come appaiono**: i manuali riportano in italiano ciò che
  hoster scrive a schermo, con la traduzione accanto dove serve.
- **Aspetto invariato**: struttura, palette e stile dei documenti sono
  rimasti identici; sono cambiati solo i testi (compresi i termini
  dell'indice analitico) e i collegamenti ai nuovi nomi.
