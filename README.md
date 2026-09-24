# hoster

[![CI](https://github.com/nic-fio/HOSTER/actions/workflows/ci.yml/badge.svg)](https://github.com/nic-fio/HOSTER/actions/workflows/ci.yml)
[Documentazione](https://nic-fio.github.io/HOSTER/) · [Download](https://github.com/nic-fio/HOSTER/releases/latest)

Un programma da terminale per scaricare file dai siti di file hosting, in stile
**JDownloader ma senza Java**: un **unico eseguibile**, niente da installare.
Legge i tuoi account e una lista di link, fa il login una volta per sito,
**ricava il link diretto** di ogni file (siti della famiglia XFileSharing:
terabytez, filestore, worldbytez e simili), **scarica più file insieme** con
ripresa se si interrompe e alla fine può **estrarre gli archivi** RAR/ZIP.

```
$ ./hoster -e -p LAPASSWORD
 ✓ kali-linux.part01.rar  1.2G
 ⬇ kali-linux.part02.rar        ▕████████████░░░░░░░░░░░░░░░░▏ 43%  512M/1.2G  92.0M/s  7s
 ⬇ kali-linux.part03.rar        ▕█████▉░░░░░░░░░░░░░░░░░░░░░░▏ 21%  250M/1.2G  88.0M/s  11s
 2 in corso · 1 completati
```

> Uso previsto: scaricare con i **propri account** contenuti che si è
> autorizzati a scaricare, attraverso il percorso premium autenticato, come fa
> JDownloader. hoster non aggira captcha né attese.

## In breve

- **Due file e un comando**: `account` (una riga per sito) e `links` (un link
  per riga), poi `./hoster`.
- **Link diretti ricavati da solo**, anche quando il sito mostra un codice al
  posto del nome del file: il nome vero viene preso dalla pagina o dal server.
- **Download in parallelo con ripresa** (`-c`), nuovi tentativi, limite di
  banda, rigenerazione automatica dei link scaduti.
- **Estrazione integrata** di RAR (anche in più parti e con password) e ZIP,
  senza `rar` installato; se serve usa `rar`/`unrar`/`7z` quando ci sono.
- **Certificati di sicurezza incompleti** gestiti da solo, solo per il sito
  che ne ha bisogno.
- **Guida a schede** nel terminale: `hoster --help`.

## Documentazione

| Documento | Per |
|---|---|
| [Manuale utente](https://nic-fio.github.io/HOSTER/manuale-utente.html) | Installare e usare hoster: account, link, download, estrazione, problemi e soluzioni, tutte le opzioni. |
| [Manuale tecnico](https://nic-fio.github.io/HOSTER/manuale-tecnico.html) | Come è fatto dentro: architettura, resolver, motore di download, estrazione, test, come aggiungere un sito. |
| [Decisioni e storia](docs/decisioni-e-storia.md) | Perché hoster è fatto così. |

I manuali sono pagine HTML, che GitHub mostra come codice sorgente: leggili
online su **https://nic-fio.github.io/HOSTER/**, oppure apri
`docs/manuale-utente.html` nel browser da un clone (funzionano anche senza rete).

## Installazione

**Il programma pronto**: dall'[ultima release](https://github.com/nic-fio/HOSTER/releases/latest)
scarica `hoster-linux-amd64` (PC e tablet x86-64) o `hoster-linux-arm64`, poi:

```
mv hoster-linux-amd64 hoster && chmod +x hoster
./hoster --help
```

**Dal sorgente** (serve Go 1.24):

```
git clone https://github.com/nic-fio/HOSTER.git
cd HOSTER
tools/setup-dev.sh --install    # pacchetti (chiede sudo) e identità git
make && make test               # build/hoster, poi tutti i controlli
```

## Recupero dopo un guasto

Il repository contiene **tutto** tranne i file personali: sorgenti, test,
manuali, strumenti, la storia completa. Per ripartire su un altro computer o
tablet:

```
git clone https://github.com/nic-fio/HOSTER.git
cd HOSTER && tools/setup-dev.sh --install && make test
```

poi ricrea `account` (partendo da `account.example`) e, se servono, `links` e
`passwords.txt`: per scelta **non sono mai pubblicati**. `tools/backup.sh`
crea anche un backup su file, che si ripristina senza rete; con `--personal`
salva a parte i file personali. I dettagli sono nel capitolo *Recupero su un
nuovo dispositivo* del manuale utente.

## Uso

```
./hoster                                  # scarica i link di 'links' in ./downloads
./hoster -e -p LAPASSWORD                 # scarica e poi estrae gli archivi
./hoster --dry-run                        # ricava i link diretti senza scaricare
./hoster --extract-only downloads -p PW   # estrae una cartella già scaricata
./hoster --help                           # guida a schede
```

Formato di `account`: `dominio:utente:password` (al posto dei `:` va bene anche
`;`; la password può contenere qualunque carattere).

## Siti

Provati sul campo: **terabytez.org** e **filestore.me**. Dovrebbero funzionare
allo stesso modo gli altri siti XFileSharing (worldbytez.com, ddownload.com,
clicknupload.to, dropapk.to, send.cm, userupload.net, usersdrive.com,
uploadrar.com, file-upload.org): basta aggiungere la riga in `account`.

## Lavorare al progetto

[CLAUDE.md](CLAUDE.md) raccoglie come si lavora al progetto: gli accordi, le
decisioni già prese e cosa controllare prima di registrare una modifica.

## Copyright

Copyright (c) 2026 nic-fio. **Tutti i diritti riservati**: il codice è
pubblico perché si possa leggere e recuperare, ma non è concessa alcuna
licenza d'uso, copia o modifica. I componenti di terzi mantengono le proprie
licenze, elencate in [NOTICE.md](NOTICE.md).

## Stato

Versione 1.0. Provato sul campo su terabytez.org e filestore.me: il set Kali
completo (15 parti, circa 18 GB) scaricato ed estratto con l'estrazione
integrata, ottenendo un'immagine VirtualBox valida. 47 test automatici che non
usano la rete, più un test dal vivo facoltativo (`make live`).
