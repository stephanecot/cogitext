#!/usr/bin/env sh
# Point d'entrée cogitex sur macOS, Linux et Git Bash.
#
# Il choisit le binaire de la plateforme courante : `.claude/settings.json` ne peut pas
# désigner un chemin différent selon l'OS, et le projet hôte n'a aucun runtime à
# fournir — c'est tout l'intérêt d'un binaire statique.
set -eu
DIR="$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)"
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
EXT=""
case "$OS" in
  # Git Bash, MSYS2 et Cygwin annoncent « mingw64_nt-10.0-26200 » et consorts, mais
  # exécutent des binaires Windows. Sans ce repli, le shim cherche un binaire qui
  # n'existe pour personne — et c'est lui que les hooks du plugin appellent sous
  # Windows.
  mingw* | msys* | cygwin*) OS=windows; EXT=".exe" ;;
esac
case "$(uname -m)" in
  arm64|aarch64) ARCH=arm64 ;;
  x86_64|amd64)  ARCH=amd64 ;;
  *) echo "cogitex : architecture non gérée : $(uname -m)" >&2; exit 1 ;;
esac
BIN="$DIR/bin/cogitex-$OS-$ARCH$EXT"
[ -x "$BIN" ] || { echo "cogitex : binaire absent pour $OS/$ARCH ($BIN)" >&2; exit 1; }
exec "$BIN" "$@"
