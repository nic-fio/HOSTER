#!/usr/bin/env python3
"""Controlla che i manuali siano allineati al codice. Eseguito da 'make test'.

Fallisce se:
  - un'opzione della guida integrata (helpSections in help.go) non ha la sua
    voce nel riferimento del manuale utente, o il manuale documenta
    un'opzione che non esiste;
  - un file .go manca dalla mappa dei file del manuale tecnico, o il numero
    di righe indicato si scosta di più del 10% da quello vero;
  - l'eseguibile ./hoster (registrato nel repository) manca, non è un ELF
    x86-64 o non contiene la versione di help.go;
  - la versione di help.go non coincide con quella di README.md,
    docs/index.html e dei due manuali;
  - un link interno #ancora dei manuali punta a un id inesistente.
"""
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
DOCS = ROOT / "docs"
USER = DOCS / "HOSTER_Manuale_Utente.html"
TECH = DOCS / "HOSTER_Manuale_Tecnico.html"

errors = []


def err(msg):
    errors.append(msg)


help_go = (ROOT / "help.go").read_text()
user = USER.read_text()
tech = TECH.read_text()

# ---- opzioni ----
sections = help_go[help_go.index("var helpSections"):help_go.index("// hostsTested")]
help_flags = set()
for spec in re.findall(r'\{"(-[^"]+)",', sections):
    for part in spec.split(","):
        help_flags.add(part.strip().split()[0])

ref = user[user.index('id="riferimento"'):user.index('id="file-usati"')]
doc_flags = set()
for idx in re.findall(r'<dt[^>]*data-kind="opzione"[^>]*data-index="([^"]+)"', ref):
    doc_flags.update(x.strip() for x in idx.split(";") if x.strip())

for f in sorted(help_flags - doc_flags):
    err(f"manuale utente: manca l'opzione {f} nel riferimento")
for f in sorted(doc_flags - help_flags):
    err(f"manuale utente: l'opzione {f} non esiste nella guida integrata")

# ---- mappa dei file ----
fmap = tech[tech.index('id="file-map"'):]
fmap = fmap[:fmap.index("</table>")]
rows = dict(re.findall(r"<tr><td><code>([\w.]+\.go)</code></td><td>(\d+)</td>", fmap))
for go in sorted(p.name for p in ROOT.glob("*.go")):
    real = len((ROOT / go).read_text().splitlines())
    if go not in rows:
        err(f"manuale tecnico: {go} ({real} righe) manca dalla mappa dei file")
        continue
    doc = int(rows.pop(go))
    if abs(doc - real) > max(3, real // 10):
        err(f"manuale tecnico: {go} ha {real} righe, la mappa dice {doc}")
for go in sorted(rows):
    err(f"manuale tecnico: la mappa cita {go}, che non esiste")

# ---- versione ----
m = re.search(r'"hoster (\d+\.\d+(?:\.\d+)?)"', help_go)
version = m.group(1) if m else None
if not version:
    err('help.go: versione "hoster X.Y" non trovata')
else:
    checks = {
        "README.md": rf"Versione {re.escape(version)}\b",
        "docs/index.html": rf"<span>Versione</span><b>{re.escape(version)}</b>",
        "docs/HOSTER_Manuale_Utente.html": rf"<span>Versione</span><b>{re.escape(version)}</b>",
        "docs/HOSTER_Manuale_Tecnico.html": rf"<span>Versione</span><b>{re.escape(version)}</b>",
    }
    for rel, pat in checks.items():
        if not re.search(pat, (ROOT / rel).read_text()):
            err(f"{rel}: la versione non è {version} (come in help.go)")

# ---- eseguibile registrato ----
exe = ROOT / "hoster"
if not exe.is_file():
    err("manca l'eseguibile ./hoster: esegui 'make' e registralo")
else:
    data = exe.read_bytes()
    # ELF, 64 bit, x86-64 (e_machine = 0x3E)
    if data[:4] != b"\x7fELF" or data[4] != 2 or data[18:20] != b"\x3e\x00":
        err("./hoster non è un eseguibile Linux x86-64: rigeneralo con 'make'")
    elif version and ("hoster " + version).encode() not in data:
        err(f"./hoster non contiene la versione {version}: rigeneralo con 'make'")

# ---- ancore interne ----
for path, text in ((USER, user), (TECH, tech)):
    ids = set(re.findall(r'\bid="([^"]+)"', text))
    for anchor in sorted(set(re.findall(r'href="#([^"]+)"', text))):
        if anchor not in ids:
            err(f"{path.name}: link a #{anchor}, che non esiste")
    other = TECH if path == USER else USER
    other_ids = set(re.findall(r'\bid="([^"]+)"', other.read_text()))
    for anchor in sorted(set(re.findall(r'href="' + other.name + r'#([^"]+)"', text))):
        if anchor not in other_ids:
            err(f"{path.name}: link a {other.name}#{anchor}, che non esiste")

if errors:
    print("controlli della documentazione: FALLITI")
    for e in errors:
        print("  - " + e)
    sys.exit(1)
print(f"controlli della documentazione: ok ({len(help_flags)} opzioni, "
      f"{len(list(ROOT.glob('*.go')))} file, versione {version})")
