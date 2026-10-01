# Documentazione di hoster

GitHub mostra i file `.html` come codice sorgente: non visualizza le pagine web
salvate in un repository. I manuali si leggono in uno di questi due modi.

**Online** (GitHub Pages, sempre allineati a `main`):

| Documento | Link |
|---|---|
| Manuale utente (in inglese) | https://nic-fio.github.io/HOSTER/User%20Manual.html |
| Manuale tecnico (in inglese) | https://nic-fio.github.io/HOSTER/Technical%20Manual.html |
| Pagina iniziale | https://nic-fio.github.io/HOSTER/ |

**Senza rete**: clona il repository e apri `docs/User Manual.html` nel
browser. Ogni manuale è un file unico che contiene stile (lo stile comune dei manuali
dei sette progetti, identico in tutti), script (ricerca, indice analitico,
pulsante Copy), logo e diagrammi, quindi funziona anche offline.

[Decisioni e storia](decisions-and-history.md) è in Markdown, quindi GitHub lo
mostra già formattato.

Dentro il programma, `hoster --help` mostra la guida integrata a schede.

## File

| File | Cosa |
|---|---|
| `index.html` | Pagina iniziale del sito della documentazione. |
| `User Manual.html` | Manuale utente, in inglese: installare e usare hoster; riferimento di tutte le opzioni. |
| `Technical Manual.html` | Manuale tecnico, in inglese: architettura, funzionamento interno, test, convenzioni, limiti noti. |
| `decisions-and-history.md` | Perché hoster è fatto così. |

`tools/check-docs.py` (eseguito da `make test`) verifica che i manuali siano
allineati al codice.
