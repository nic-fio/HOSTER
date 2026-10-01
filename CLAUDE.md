# Lavorare a hoster

Come si sviluppa questo progetto: gli accordi, le decisioni già prese e cosa
controllare prima di registrare una modifica. Un clone più questo file sono
tutto il contesto di lavoro: niente di ciò che serve per continuare sta fuori
dal repository, tranne i file personali (vedi sotto).

## Come si lavora

- **Si parla con l'utente in italiano, sempre.** L'utente non capisce
  l'inglese: niente frasi di passaggio in inglese, nemmeno brevi. Anche
  codice, commenti, messaggi del programma, guida integrata (`help.go`) e i
  documenti Markdown (`README.md`, `docs/README.md`, `decisions-and-history.md`,
  questo file) sono in italiano.
- **I manuali e la pagina `docs/index.html` sono in inglese** (decisione del
  proprietario, 30 settembre e 1° ottobre 2026): `docs/User Manual.html`,
  `docs/Technical Manual.html` e `docs/index.html`. Quando citano
  messaggi o schede del programma li riportano in italiano, come compaiono a
  schermo, con la traduzione accanto dove serve. Il programma non si traduce.
- **Testi per l'utente senza gergo da programmatori** (preferenza esplicita):
  help, messaggi e manuale utente sono "copy di prodotto", non note tecniche.
- **Verificare, non dichiarare.** Ogni affermazione sul comportamento viene da
  un'esecuzione: i test, un `--dry-run`, un download reale. "Dovrebbe
  funzionare" non è un risultato.
- **La documentazione conta quanto il codice.** Una funzione non è finita
  finché i manuali non la descrivono.

## Decisioni prese: non riaprirle

Il perché di ciascuna è in [docs/decisions-and-history.md](docs/decisions-and-history.md).

| Decisione | |
|---|---|
| **Un solo binario statico, zero dipendenze esterne** | Motore di download ed estrazione integrati; unica libreria Go: `rardecode`. |
| **Solo percorso premium autenticato** | Niente captcha né countdown da aggirare: scelta etica e di robustezza. |
| **Niente sottoprocesso `scrap`** | Il motore di `scrap` è stato re-implementato qui; `scrap` resta intoccato. Flag armonizzati con `scrap`. |
| **Una barra per file** | I premium danno una connessione per file: la vista multi-segmento non serve. |
| **TLS: ripiego automatico e mirato** | Solo per "autorità sconosciuta" (catena incompleta) e solo per quell'host; `-k` resta come forzatura. |
| **File personali mai nel repository** | `account`, `links`, `LISTA_LINKS`, `passwords.txt` sono in `.gitignore`. Decisione dell'utente: le credenziali restano fuori del tutto, nemmeno cifrate. |
| **L'eseguibile sta nel repository** | `./hoster` (Linux x86-64, statico) è registrato, così un `git clone` basta anche senza Go. Richiesta esplicita dell'utente. `make` lo rigenera. |
| **Nessuna licenza** | Copyright nic-fio, tutti i diritti riservati; il codice è pubblico per poterlo leggere e recuperare. |
| **Manuali in inglese, tema chiaro, stile comune** | `docs/User Manual.html` e `docs/Technical Manual.html` (negli href lo spazio si scrive `%20`): file HTML unici in `docs/`, pubblicati con GitHub Pages; niente tema scuro. Stile comune dei manuali dei 7 progetti, approvato dal proprietario il 1° ottobre 2026: il canone è incorporato in ogni manuale ed è identico in tutti, byte per byte. Il `<style>` comincia con il CSS comune (`manual.css`, commento iniziale «Stile comune dei manuali dei 7 progetti»); l'ultimo `<script>` è lo script comune (`manual.js`: ricerca, indice analitico, pulsante Copy), preceduto da `window.MANUAL_CODE_TERMS` (le voci dell'indice da mostrare come codice). Non si modificano in un solo progetto: un cambio vale per tutti e sette. Copertina: logo (copia ridotta di `logos/hoster-logo.png`, incorporata), «User Manual»/«Technical Manual», Version e Date; piè di pagina `hoster · User Manual · Version X · Mese AAAA · © 2026 Nicola Fiorillo`. Programma e resto della documentazione restano in italiano. |

## Il repository

| Dove | Cosa |
|---|---|
| `*.go` | Il programma (`package main`): vedi la mappa dei file nel manuale tecnico. |
| `*_test.go` | 47 test senza rete + `TestLiveResolve` (solo con `HOSTER_LIVE=1`). |
| `docs/` | `User Manual.html`, `Technical Manual.html` (i manuali, in inglese), `index.html`, `decisions-and-history.md`. Pubblicati con GitHub Pages. |
| `tools/` | `setup-dev.sh` (pacchetti e identità git), `backup.sh` (bundle git), `check-docs.py` (controlli dei manuali). |
| `logos/` | `hoster-logo.png`, il logo (PNG 2172×724, sfondo trasparente). Nella copertina dei manuali ce n'è una copia ridotta (700 px, 256 colori), incorporata. |
| `account.example`, `links.example` | Modelli dei file personali. |
| `hoster` | L'eseguibile Linux x86-64, **registrato**: va rigenerato con `make` e registrato insieme a ogni modifica del codice. |
| `dist/` | Binari per le release, mai registrati. |

Il repository segue la struttura standard comune ai sette progetti (AMS,
EFI_PARTITION_MANAGER, HOSTER, MTERM, NESH, PHONESTRA, SCRAPER): il codice Go
alla radice, `tools/`, `docs/` (con `index.html`, i due manuali, `README.md`,
`decisions-and-history.md`, `.nojekyll`), `NOTICE.md`, la CI e gli obiettivi
comuni di `make`: `all`, `test`, `docs-check`, `clean`.

## Prima di registrare una modifica

1. `make test`: `go vet`, `gofmt`, test con `-race` e i controlli dei manuali
   (ogni opzione della guida integrata deve comparire nel riferimento del
   manuale utente, ogni file `.go` nella mappa del manuale tecnico, la
   versione deve coincidere ovunque). **Fallisce se i manuali non sono
   aggiornati.**
2. Se hai cambiato il codice: `make` e registra anche `./hoster`, che deve
   corrispondere al sorgente (`check-docs.py` verifica che esista e che la sua
   versione coincida con `help.go`).
3. Se hai toccato resolver, login o TLS: `make live` con `account` e
   `LISTA_LINKS` reali (lo lancia l'utente, le credenziali sono sue).
4. Controlla che `git status` non mostri file personali. Mai `git add -A`
   senza guardare.
5. I commit usano l'identità locale impostata da `tools/setup-dev.sh`
   (indirizzo noreply di GitHub), che tiene fuori quello personale.

Rilascio: aggiorna la versione in `help.go` (`"hoster 1.0"`), in `README.md`,
`docs/index.html` e nei due manuali (copertina e piè di pagina di tutti e tre), poi crea un tag annotato `vX.Y.Z` e fai
push del tag: la CI costruisce i binari Linux amd64/arm64 e pubblica la
release, con il messaggio del tag come note.

## A che punto è

Versione 1.0, provata sul campo su terabytez.org e filestore.me (set Kali da
15 parti, ~18 GB, scaricato ed estratto). Punti aperti noti, descritti nel
capitolo *Known limitations* del manuale tecnico: `-c` riscarica anche i file già
completi; i file con un codice al posto del nome non sono riconosciuti come
già scaricati; il programma esce con stato 0 anche se qualche download
fallisce; due ri-risoluzioni simultanee possono rifare il login due volte.
Idee per il futuro: verificare gli altri siti, riparazione con i `.rev`,
limiti di parallelismo per sito, stato in JSON, coda persistente.
