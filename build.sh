#!/usr/bin/env bash
# Reconstruit les binaires cogit pour toutes les plateformes cibles, dans le
# payload `dist/` — c'est lui qu'on dépose ensuite dans un projet hôte.
#
# À ne lancer qu'au moment d'une release : chaque exécution ajoute un blob par
# plateforme dans l'historique git, et git gère mal le binaire qui change souvent.
#
# CGO_ENABLED=0 garantit un binaire totalement statique : ni glibc, ni musl, ni
# aucune bibliothèque partagée à trouver sur la machine du coéquipier.
set -euo pipefail
cd "$(dirname "$0")"
OUT="dist/.claude/cogit/bin"
mkdir -p "$OUT"
for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64; do
  os="${t%/*}"; arch="${t#*/}"; ext=""
  [ "$os" = windows ] && ext=".exe"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags="-s -w" -o "$OUT/cogit-$os-$arch$ext" .
  printf '  %-22s %s\n' "cogit-$os-$arch$ext" "$(du -h "$OUT/cogit-$os-$arch$ext" | cut -f1)"
done
echo "  total : $(du -sh "$OUT" | cut -f1)"
