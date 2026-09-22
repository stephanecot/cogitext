#!/usr/bin/env bash
# Dépose cogitex dans un projet, quel qu'il soit.
#
#   ./install.sh /chemin/vers/le/projet
#
# Copie le contenu de `dist/` dans le projet : le binaire et ses lanceurs, les
# skills et l'agent pour Claude Code, les hooks et les instructions pour GitHub
# Copilot. Puis ajoute au `.gitignore` et au `.gitattributes` de l'hôte les lignes
# sans lesquelles cogitex fonctionne mal : le worktree du contexte serait commité,
# et les binaires seraient corrompus par la conversion de fins de ligne sur un
# clone Windows.
#
# Rien n'est écrasé sans le dire : un fichier déjà présent et différent est
# signalé et conservé, sauf avec --force.
set -euo pipefail

FORCE=0
TARGET=""
for arg in "$@"; do
  case "$arg" in
    --force) FORCE=1 ;;
    -h|--help) sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) TARGET="$arg" ;;
  esac
done

if [ -z "$TARGET" ]; then
  echo "usage : $0 <chemin du projet> [--force]" >&2
  exit 2
fi
if [ ! -d "$TARGET" ]; then
  echo "cogitex : « $TARGET » n'est pas un répertoire." >&2
  exit 1
fi
if [ ! -e "$TARGET/.git" ]; then
  echo "cogitex : « $TARGET » n'est pas un dépôt git — cogitex porte son contexte sur une branche." >&2
  exit 1
fi

HERE="$(cd "$(dirname "$0")" && pwd)"
SRC="$HERE/dist"
[ -d "$SRC/.claude" ] || { echo "cogitex : payload introuvable dans $SRC." >&2; exit 1; }

copied=0 kept=0
while IFS= read -r rel; do
  from="$SRC/$rel"
  to="$TARGET/$rel"
  if [ -f "$to" ] && ! cmp -s "$from" "$to"; then
    if [ "$FORCE" -eq 0 ]; then
      echo "  conservé (différent)  $rel"
      kept=$((kept + 1))
      continue
    fi
  fi
  mkdir -p "$(dirname "$to")"
  cp "$from" "$to"
  # Le bit d'exécution, sur le lanceur comme sur les binaires : un clone qui l'aurait
  # perdu — archive zip, copie via un partage Windows — donnerait un `cogitex.sh` qui
  # refuse de démarrer, avec un message qui n'aide personne.
  case "$rel" in
    *.sh | */cogitex/bin/*) [ "${rel##*.}" = "exe" ] || chmod +x "$to" ;;
  esac
  copied=$((copied + 1))
done < <(cd "$SRC" && find . -type f | sed 's#^\./##')

# Les lignes que l'hôte doit porter, ajoutées une seule fois.
add_line() {
  local file="$1" line="$2" header="$3"
  touch "$file"
  if ! grep -qxF "$line" "$file"; then
    { [ -s "$file" ] && echo ""; [ -n "$header" ] && echo "$header"; echo "$line"; } >> "$file"
    echo "  ajouté à $(basename "$file") : $line"
  fi
}
add_line "$TARGET/.gitignore" "/.cogitex/" "# Worktree du contexte partagé, monté par \`cogitex init\`."
add_line "$TARGET/.gitignore" "/.claude/cache/" "# Cache des sessions des agents."
add_line "$TARGET/.gitignore" "/.claude/settings.local.json" "# Réglages écrits par \`cogitex init\` : ils désignent un binaire par OS."
add_line "$TARGET/.gitattributes" ".claude/cogitex/bin/** binary" "# Les binaires cogitex ne doivent subir aucune conversion de fins de ligne."
add_line "$TARGET/.gitattributes" ".claude/cogitex/cogitex.sh text eol=lf" "# Les lanceurs gardent les fins de ligne de leur plateforme."
add_line "$TARGET/.gitattributes" ".claude/cogitex/cogitex.cmd text eol=crlf" ""

echo
echo "cogitex installé dans $TARGET — $copied fichier(s) copié(s), $kept conservé(s)."
[ "$kept" -gt 0 ] && echo "Relancez avec --force pour remplacer ce qui a été conservé."
cat <<'NEXT'

Reste à faire, dans le projet :

  .claude/cogitex/cogitex.sh init        # macOS, Linux, Git Bash
  .claude\cogitex\cogitex.cmd init       # Windows

`init` crée ou rejoint la branche de contexte, monte le worktree `.cogitex`, et
câble les hooks Claude Code dans `.claude/settings.local.json` — local et
gitignoré, parce qu'un chemin de binaire dépend de l'OS.

Les hooks GitHub Copilot, eux, sont déjà en place : `.github/hooks/cogitex.json`
et `.github/instructions/cogitex.instructions.md` ne dépendent d'aucun chemin
absolu. **Commitez-les** — c'est ce qui les donne à toute l'équipe, et à l'agent
cloud, qui ne lit que `.github/hooks/`.
NEXT
