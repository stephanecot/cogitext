#!/usr/bin/env bash
# Dépose cogit dans un projet, quel qu'il soit.
#
#   ./install.sh /chemin/vers/le/projet
#
# Copie le contenu de `dist/` dans le projet, puis ajoute au `.gitignore` et au
# `.gitattributes` de l'hôte les lignes sans lesquelles cogit fonctionne mal :
# le worktree du contexte serait commité, et les binaires seraient corrompus par
# la conversion de fins de ligne sur un clone Windows.
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
  echo "cogit : « $TARGET » n'est pas un répertoire." >&2
  exit 1
fi
if [ ! -e "$TARGET/.git" ]; then
  echo "cogit : « $TARGET » n'est pas un dépôt git — cogit porte son contexte sur une branche." >&2
  exit 1
fi

HERE="$(cd "$(dirname "$0")" && pwd)"
SRC="$HERE/dist"
[ -d "$SRC/.claude" ] || { echo "cogit : payload introuvable dans $SRC." >&2; exit 1; }

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
  [ "${rel##*.}" = "sh" ] && chmod +x "$to"
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
add_line "$TARGET/.gitignore" "/.cogit/" "# Worktree du contexte partagé, monté par \`cogit init\`."
add_line "$TARGET/.gitignore" "/.claude/cache/" "# Cache des sessions Claude Code."
add_line "$TARGET/.gitignore" "/.claude/settings.local.json" "# Réglages écrits par \`cogit init\` : ils désignent un binaire par OS."
add_line "$TARGET/.gitattributes" ".claude/cogit/bin/** binary" "# Les binaires cogit ne doivent subir aucune conversion de fins de ligne."
add_line "$TARGET/.gitattributes" ".claude/cogit/cogit.sh text eol=lf" "# Les lanceurs gardent les fins de ligne de leur plateforme."
add_line "$TARGET/.gitattributes" ".claude/cogit/cogit.cmd text eol=crlf" ""

echo
echo "cogit installé dans $TARGET — $copied fichier(s) copié(s), $kept conservé(s)."
[ "$kept" -gt 0 ] && echo "Relancez avec --force pour remplacer ce qui a été conservé."
cat <<'NEXT'

Reste à faire, dans le projet :

  .claude/cogit/cogit.sh init        # macOS, Linux, Git Bash
  .claude\cogit\cogit.cmd init       # Windows

`init` crée ou rejoint la branche de contexte, monte le worktree `.cogit`, et
câble les hooks dans `.claude/settings.local.json`.
NEXT
