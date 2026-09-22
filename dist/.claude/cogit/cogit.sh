#!/usr/bin/env sh
# Point d'entrée cogit sur macOS et Linux.
#
# Il choisit le binaire de la plateforme courante : `.claude/settings.json` ne peut pas
# désigner un chemin différent selon l'OS, et le projet hôte n'a aucun runtime à
# fournir — c'est tout l'intérêt d'un binaire statique.
set -eu
DIR="$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)"
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$(uname -m)" in
  arm64|aarch64) ARCH=arm64 ;;
  x86_64|amd64)  ARCH=amd64 ;;
  *) echo "cogit : architecture non gérée : $(uname -m)" >&2; exit 1 ;;
esac
BIN="$DIR/bin/cogit-$OS-$ARCH"
[ -x "$BIN" ] || { echo "cogit : binaire absent pour $OS/$ARCH ($BIN)" >&2; exit 1; }
exec "$BIN" "$@"
