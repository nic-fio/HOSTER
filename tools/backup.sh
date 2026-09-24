#!/bin/sh
# Impacchetta tutto il progetto in un solo file: un git bundle con ogni ramo,
# ogni tag e la storia completa. Si ripristina senza rete e senza GitHub:
#
#     tools/backup.sh ~/backup                 # scrive hoster-AAAA-MM-GG.bundle
#     git clone hoster-2026-09-24.bundle HOSTER
#     cd HOSTER && tools/setup-dev.sh --install && make test
#
# Con --personal aggiunge accanto al bundle un archivio con i file personali
# (account, links, passwords.txt) che il repository pubblico non contiene.
# ATTENZIONE: quell'archivio contiene le password in chiaro. Non caricarlo
# da nessuna parte pubblica; tienilo su un disco o una chiavetta tua.
set -eu

PERSONAL=no
if [ "${1:-}" = "--personal" ]; then PERSONAL=yes; shift; fi
DEST=${1:-.}
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DAY=$(date +%F)
OUT="$DEST/hoster-$DAY.bundle"

[ -d "$DEST" ] || mkdir -p "$DEST"
git -C "$ROOT" bundle create "$OUT" --all
git -C "$ROOT" bundle verify "$OUT" >/dev/null

if [ -n "$(git -C "$ROOT" status --porcelain)" ]; then
    echo "attenzione: le modifiche non ancora registrate NON sono nel bundle:"
    git -C "$ROOT" status --short
fi
if [ -n "$(git -C "$ROOT" log --oneline '@{upstream}..HEAD' 2>/dev/null)" ]; then
    echo "nota: il bundle ha commit che non sono ancora su GitHub."
fi

printf '%s: %s, %s commit, tag: %s\n' "$OUT" "$(du -h "$OUT" | cut -f1)" \
    "$(git -C "$ROOT" rev-list --count HEAD)" \
    "$(git -C "$ROOT" tag | tr '\n' ' ')"

if [ "$PERSONAL" = yes ]; then
    P="$DEST/hoster-personali-$DAY.tar.gz"
    files=
    for f in account links passwords.txt; do
        [ -f "$ROOT/$f" ] && files="$files $f"
    done
    if [ -n "$files" ]; then
        # shellcheck disable=SC2086
        (umask 077 && tar -C "$ROOT" -czf "$P" $files)
        echo "$P: file personali ($files ) — contiene password in chiaro, tienilo privato"
    else
        echo "nessun file personale da salvare"
    fi
fi
