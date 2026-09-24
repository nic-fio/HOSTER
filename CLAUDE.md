# Lavorare a hoster

Come si sviluppa questo progetto: gli accordi, le decisioni già prese e cosa
controllare prima di registrare una modifica. Un clone più questo file sono
tutto il contesto di lavoro: niente di ciò che serve per continuare sta fuori
dal repository, tranne i file personali (vedi sotto).

## Come si lavora

- **Si parla con l'utente in italiano, sempre.** L'utente non capisce
  l'inglese: niente frasi di passaggio in inglese, nemmeno brevi. Anche
  codice, commenti, messaggi del programma e documentazione sono in italiano.
- **Testi per l'utente senza gergo da programmatori** (preferenza esplicita):
  help, messaggi e manuale utente sono "copy di prodotto", non note tecniche.
- **Verificare, non dichiarare.** Ogni affermazione sul comportamento viene da
  un'esecuzione: i test, un `--dry-run`, un download reale. "Dovrebbe
  funzionare" non è un risultato.
- **La documentazione conta quanto il codice.** Una funzione non è finita
  finché i manuali non la descrivono.

## Decisioni prese: non riaprirle

Il perché di ciascuna è in [docs/decisioni-e-storia.md](docs/decisioni-e-storia.md).

| Decisione | |
|---|---|
| **Un solo binario statico, zero dipendenze esterne** | Motore di download ed estrazione integrati; unica libreria Go: `rardecode`. |
| **Solo percorso premium autenticato** | Niente captcha né countdown da aggirare: scelta etica e di robustezza. |
| **Niente sottoprocesso `scrap`** | Il motore di `scrap` è stato re-implementato qui; `scrap` resta intoccato. Flag armonizzati con `scrap`. |
| **Una barra per file** | I premium danno una connessione per file: la vista multi-segmento non serve. |
| **TLS: ripiego automatico e mirato** | Solo per "autorità sconosciuta" (catena incompleta) e solo per quell'host; `-k` resta come forzatura. |
| **File personali mai nel repository** | `account`, `links`, `LISTA_LINKS`, `passwords.txt` sono in `.gitignore`. Decisione dell'utente: le credenziali restano fuori del tutto, nemmeno cifrate. |
| **Nessuna licenza** | Copyright nic-fio, tutti i diritti riservati; il codice è pubblico per poterlo leggere e recuperare. |
| **Manuali in italiano, tema chiaro** | Stesso impianto di NG-EFI_SHELL (HTML in `docs/`, GitHub Pages), niente tema scuro. |

## Il repository

| Dove | Cosa |
|---|---|
| `*.go` | Il programma (`package main`): vedi la mappa dei file nel manuale tecnico. |
| `*_test.go` | 47 test senza rete + `TestLiveResolve` (solo con `HOSTER_LIVE=1`). |
| `docs/` | `manuale-utente.html`, `manuale-tecnico.html`, `decisioni-e-storia.md`, `assets/`. Pubblicati con GitHub Pages. |
| `tools/` | `setup-dev.sh` (pacchetti e identità git), `backup.sh` (bundle git), `check-docs.py` (controlli dei manuali). |
| `account.example`, `links.example` | Modelli dei file personali. |
| `build/`, `dist/` | Solo prodotti, mai registrati. |

## Prima di registrare una modifica

1. `make test`: `go vet`, `gofmt`, test con `-race` e i controlli dei manuali
   (ogni opzione della guida integrata deve comparire nel riferimento del
   manuale utente, ogni file `.go` nella mappa del manuale tecnico, la
   versione deve coincidere ovunque). **Fallisce se i manuali non sono
   aggiornati.**
2. Se hai toccato resolver, login o TLS: `make live` con `account` e
   `LISTA_LINKS` reali (lo lancia l'utente, le credenziali sono sue).
3. Controlla che `git status` non mostri file personali. Mai `git add -A`
   senza guardare.
4. I commit usano l'identità locale impostata da `tools/setup-dev.sh`
   (indirizzo noreply di GitHub), che tiene fuori quello personale.

Rilascio: aggiorna la versione in `help.go` (`"hoster 1.0"`), in `README.md`,
`docs/index.html` e nei due manuali, poi crea un tag annotato `vX.Y.Z` e fai
push del tag: la CI costruisce i binari Linux amd64/arm64 e pubblica la
release, con il messaggio del tag come note.

## A che punto è

Versione 1.0, provata sul campo su terabytez.org e filestore.me (set Kali da
15 parti, ~18 GB, scaricato ed estratto). Punti aperti noti, descritti nel
capitolo *Limiti noti* del manuale tecnico: `-c` riscarica anche i file già
completi; i file con un codice al posto del nome non sono riconosciuti come
già scaricati; il programma esce con stato 0 anche se qualche download
fallisce; due ri-risoluzioni simultanee possono rifare il login due volte.
Idee per il futuro: verificare gli altri siti, riparazione con i `.rev`,
limiti di parallelismo per sito, stato in JSON, coda persistente.
