package main

// cogitex — le contexte partagé entre plusieurs utilisateurs, porté par la branche
// orpheline refs/heads/context.
//
// Un binaire statique sans dépendance : le projet hôte peut être en Java, en Python
// ou en quoi que ce soit, il n'a pas à fournir de runtime. La seule dépendance est
// git, et c'est cohérent — tout le système est bâti dessus.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

var (
	root string
	args []string
)

func say(f string, a ...any)  { fmt.Printf(f+"\n", a...) }
func warn(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) }
func die(f string, a ...any)  { warn("cogitex : "+f, a...); os.Exit(1) }

// Les drapeaux sans valeur. Les lister est la seule façon de ne pas avaler
// l'argument suivant : `hook-start --copilot /chemin/du/projet` perdrait sa racine.
var boolFlags = map[string]bool{
	"--offline": true, "--no-push": true, "--copilot": true, "--claude": true,
	"--no-hooks": true, "--force": true,
}

func hasFlag(n string) bool {
	for _, a := range args {
		if a == "--"+n {
			return true
		}
	}
	return false
}

func flagVal(n string) string {
	for i, a := range args {
		if a == "--"+n && i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			return args[i+1]
		}
	}
	return ""
}

func positional() []string {
	var out []string
	skip := false
	for i, a := range args {
		if skip {
			skip = false
			continue
		}
		if strings.HasPrefix(a, "--") {
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") && !boolFlags[a] {
				skip = true
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

func resolveRoot(explicit string) string {
	if explicit != "" {
		p, _ := filepath.Abs(explicit)
		return p
	}
	if v := os.Getenv("CLAUDE_PROJECT_DIR"); v != "" {
		p, _ := filepath.Abs(v)
		return p
	}
	wd, _ := os.Getwd()
	// On remonte jusqu'au dépôt : cogitex peut être appelé depuis n'importe quel
	// sous-répertoire du projet.
	for d := wd; ; {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return wd
		}
		d = parent
	}
}

const help = `cogitex — contexte partagé sur la branche orpheline « context »

  init [--no-hooks]       créer ou rejoindre la branche, monter .cogitex, câbler les hooks
  add decision|fact|note  enregistrer une entrée (JSON sur stdin) [--no-push]
  push                    publier les commits locaux en un seul mouvement
  sync [--offline]        récupérer, afficher le delta, ré-épingler la session
  find "<mots>"           chercher dans tout le corpus
  show <id>               afficher une entrée en entier
  list decisions|facts|notes
  brief                   afficher le bloc injecté au démarrage
  doctor                  vérifier le corpus, le plafond du brief et la plateforme
  compact                 compacter la base d'objets
  debug on|off|tail|clear tracer les interactions dans .claude/cache/cogitex/cogitex.log

  hook-start | hook-prompt | hook-guard    points d'entrée des hooks

Les hooks servent Claude Code et GitHub Copilot : le dialecte est reconnu sur le
payload reçu, et --copilot / --claude le forcent si besoin.`

func main() {
	all := os.Args[1:]
	if len(all) == 0 {
		say("%s", help)
		return
	}
	cmd := all[0]
	args = all[1:]

	// Les hooks reçoivent la racine en argument : les scripts restent testables à la
	// main, et rien ne dépend d'une variable d'environnement qu'on n'a pas vérifiée.
	explicit := ""
	if p := positional(); len(p) > 0 && strings.HasPrefix(cmd, "hook-") {
		explicit = p[0]
	}
	if v := flagVal("root"); v != "" {
		explicit = v
	}
	root = resolveRoot(explicit)

	switch cmd {
	case "hook-start", "hook-prompt", "hook-guard":
		in := readHookInput()
		if explicit == "" && in.CWD != "" {
			root = resolveRoot(in.CWD)
		}
		if pluginStandsDown(root) {
			return
		}
		defer func() { _ = recover() }() // une panique de cogitex ne coûte jamais un tour
		switch cmd {
		case "hook-start":
			hookStart(root, in)
		case "hook-prompt":
			hookPrompt(root, in)
		case "hook-guard":
			hookGuard(root, in)
		}
	case "init":
		cmdInit()
	case "add":
		cmdAdd()
	case "push":
		cmdPush()
	case "sync":
		cmdSync()
	case "find":
		cmdFind()
	case "show":
		cmdShow()
	case "list":
		cmdList()
	case "brief":
		cmdBrief()
	case "doctor":
		cmdDoctor()
	case "compact":
		cmdCompact()
	case "debug":
		cmdDebug()
	case "help", "--help", "-h":
		say("%s", help)
	default:
		say("%s", help)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------- init

// L'arbre vide de git est une constante universelle : partir de lui avec une identité
// et des dates épinglées fait que TOUTES les machines calculent le même SHA racine.
// Deux amorçages concurrents convergent alors, au lieu de produire deux histoires sans
// ancêtre commun, impossibles à fusionner sans bricolage.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
const epoch = "1970-01-01T00:00:00Z"

const gitattributes = `# Pas de normalisation des fins de ligne : core.autocrlf varie selon les postes,
# et une conversion silencieuse casserait la fusion « union » du journal.
* -text

# Le journal est le seul fichier réellement concurrent. Il est déjà sharded par
# acteur ; « union » couvre le cas résiduel du même acteur sur deux machines.
journal/**/*.ndjson merge=union
`

const readme = `# Shared context

Orphan branch. It carries no code and is never merged into the main branch.

- ` + "`decisions/<domain>/<date>-<slug>.yaml`" + ` — the rules. Immutable: supersede, never edit.
- ` + "`facts/<date>-<slug>.yaml`" + ` — volatile facts, expiring on their own (ttl_days).
- ` + "`notes/<date>-<slug>.md`" + ` — ideas, backlog, and above all dead ends with their reason.
- ` + "`journal/<YYYY-MM>/<actor>.ndjson`" + ` — who did what. One file per actor.

Everything here is written in English. Do not hand-edit: the identifier is the path,
and decisions and facts make other people's sessions stale.
`

// `extensions.relativeWorktrees` : la marque qu'un `cogitex init` d'une version
// précédente a laissée dans le .git/config du projet hôte, via `worktree add
// --relative-paths`. Tout git < 2.48 refuse ensuite TOUTE commande dans ce dépôt.
var relWorktreesRe = regexp.MustCompile(`(?im)^[ \t]*relativeworktrees[ \t]*=.*\r?\n?`)

// Le retrait se fait par une édition de texte, sans passer par git : la personne
// réellement bloquée est justement celle dont le git refuse déjà tout ici.
func unbrandRepo(root, wt string) {
	gitdir := filepath.Join(root, ".git")
	if st, err := os.Stat(gitdir); err != nil || !st.IsDir() {
		return
	}
	cfg := filepath.Join(gitdir, "config")
	b, err := os.ReadFile(cfg)
	if err != nil || !relWorktreesRe.Match(b) {
		return
	}
	// Un autre worktree du projet peut dépendre de cette extension. On ne retire que
	// ce qu'on a posé : si le dépôt en porte un autre que le nôtre, on se contente de
	// le dire.
	//
	// L'identification passe par le fichier `gitdir` et non par le nom du répertoire :
	// git renomme `.cogitex` en `-cogitex` sous `.git/worktrees/`, et deviner cette
	// transformation est exactement le genre de pari qui casse à la version suivante.
	mine := "/" + filepath.Base(wt) + "/.git"
	if entries, err := os.ReadDir(filepath.Join(gitdir, "worktrees")); err == nil {
		for _, e := range entries {
			g, err := os.ReadFile(filepath.Join(gitdir, "worktrees", e.Name(), "gitdir"))
			if err != nil || !strings.HasSuffix(strings.TrimSpace(filepath.ToSlash(string(g))), mine) {
				warn("cogitex : `extensions.relativeWorktrees` est actif et un autre worktree en dépend " +
					"— à retirer à la main si un coéquipier a git < 2.48.")
				return
			}
		}
	}
	if err := writeAtomic(cfg, relWorktreesRe.ReplaceAll(b, nil)); err != nil {
		warn("cogitex : `extensions.relativeWorktrees` dans .git/config, à retirer à la main (%v).", err)
		return
	}
	say("cogitex : `extensions.relativeWorktrees` retiré — il rendait toute commande git impossible " +
		"pour un coéquipier en git < 2.48.")

	// Le worktree existant porte des chemins relatifs : on le refait proprement. Jamais
	// s'il contient du travail non commité.
	if _, err := os.Stat(filepath.Join(wt, ".git")); err != nil {
		return
	}
	if r := runGit([]string{"status", "--porcelain"}, gitOpts{Dir: wt, Timeout: 10 * time.Second}); r.OK && r.Out != "" {
		warn("cogitex : `.cogitex` a des modifications non commitées — worktree laissé tel quel.")
		return
	}
	_ = runGit([]string{"worktree", "remove", "--force", wt}, gitOpts{Dir: root, Timeout: 15 * time.Second})
	_ = os.RemoveAll(wt)
	git(root, "worktree", "prune")
}

func cmdInit() {
	if IsShallow(root) {
		die("clone superficiel : `git fetch --unshallow` d'abord (le calcul de delta a besoin des anciens objets)")
	}
	git(root, "worktree", "prune")

	if !HasBranch(root) {
		fetched := FetchContext(root, 10*time.Second)
		if RevParse(root, RemoteRef) != "" {
			git(root, "branch", Branch, RemoteRef)
			say("cogitex : branche `context` récupérée depuis origin.")
		} else {
			if !fetched.OK && !strings.Contains(fetched.Err, "couldn't find remote ref") {
				say("cogitex : remote injoignable — création locale, le push se fera plus tard.")
			}
			r := runGit([]string{"-c", "user.name=ctx", "-c", "user.email=ctx@local",
				"commit-tree", "-m", "cogitex: root", emptyTree},
				gitOpts{Dir: root, Timeout: 5 * time.Second,
					Env: []string{"GIT_AUTHOR_DATE=" + epoch, "GIT_COMMITTER_DATE=" + epoch}})
			if !r.OK {
				die("création du commit racine impossible : %s", r.Err)
			}
			up := git(root, "update-ref", Ref, r.Out, "")
			if !up.OK && !strings.Contains(up.Err, "already exists") {
				die("update-ref : %s", up.Err)
			}
			say("cogitex : branche `context` créée (racine déterministe %s).", short(r.Out))
		}
	}

	wt := CogitexDir(root)
	unbrandRepo(root, wt)
	if _, err := os.Stat(filepath.Join(wt, ".git")); err == nil {
		say("cogitex : worktree `.cogitex` déjà en place.")
	} else if entries, err := os.ReadDir(wt); err == nil && len(entries) > 0 {
		die("`.cogitex` existe et n'est pas un worktree — je ne supprime rien, à toi de voir")
	} else {
		// JAMAIS `--relative-paths`. Sur git ≥ 2.48 l'option réussit et inscrit
		// `extensions.relativeWorktrees` dans le .git/config du PROJET HÔTE ; tout git
		// plus ancien refuse alors la moindre commande dans ce dépôt — pas seulement
		// les commandes de worktree — avec « unknown repository extension found ».
		// Un coéquipier sous Ubuntu 24.04 (git 2.43) ne peut plus faire un `git status`,
		// et rien dans le message ne mentionne cogitex.
		//
		// Ce qu'on perd : un worktree qui survit au déplacement du répertoire du projet.
		// C'est réparable en une commande (`git worktree repair`). L'autre panne ne
		// l'est pas, et elle frappe quelqu'un d'autre que celui qui l'a causée.
		add := runGit([]string{"worktree", "add", wt, Branch}, gitOpts{Dir: root, Timeout: 15 * time.Second})
		if !add.OK {
			die("worktree add : %s", add.Err)
		}
		say("cogitex : worktree monté sur `.cogitex`.")
	}

	seeded := false
	for _, f := range []struct{ name, body string }{{".gitattributes", gitattributes}, {"README.md", readme}} {
		p := filepath.Join(wt, f.name)
		if _, err := os.Stat(p); err != nil {
			_ = os.WriteFile(p, []byte(f.body), 0o644)
			seeded = true
		}
	}
	for _, d := range []string{"decisions", "facts", "notes", "journal"} {
		_ = os.MkdirAll(filepath.Join(wt, d), 0o755)
	}
	if seeded {
		commitCtx([]string{".gitattributes", "README.md"}, "chore: branch layout")
	}

	EnsureHead(root, true)
	ensureIgnored()
	switch {
	case hasFlag("no-hooks"):
		say("cogitex : hooks non câblés (--no-hooks).")
	case runsFromPlugin(root):
		// Le plugin déclare déjà les hooks pour tous les projets. En écrire une
		// seconde paire ici les ferait tirer deux fois par tour.
		say("cogitex : binaire fourni par un plugin — ses hooks font foi, rien n'est câblé dans le projet.")
	default:
		writeSettings()
	}
	say("cogitex : prêt. `cogitex add decision` pour enregistrer une règle.")
}

// .cogitex et le cache dérivé ne doivent jamais entrer dans un commit du projet.
func ensureIgnored() {
	gi := filepath.Join(root, ".gitignore")
	b, _ := os.ReadFile(gi)
	text := string(b)
	var missing []string
	for _, line := range []string{"/.cogitex/", "/.claude/cache/"} {
		found := false
		for _, l := range strings.Split(text, "\n") {
			if strings.TrimSpace(l) == line {
				found = true
			}
		}
		if !found {
			missing = append(missing, line)
		}
	}
	if len(missing) == 0 {
		return
	}
	out := strings.TrimRight(text, "\n") + "\n\n# Contexte partagé : worktree de la branche `context` et index dérivé\n" +
		strings.Join(missing, "\n") + "\n"
	if err := os.WriteFile(gi, []byte(out), 0o644); err == nil {
		say("cogitex : %s ajouté(s) à .gitignore.", strings.Join(missing, " et "))
	}
}

// settings.json ne peut pas désigner un chemin de binaire différent selon l'OS : il
// est donc généré localement, pour la plateforme courante, et gitignoré. C'est la
// contrepartie assumée des binaires commités.
func writeSettings() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	if rel, err := filepath.Rel(root, self); err == nil && !strings.HasPrefix(rel, "..") {
		self = "${CLAUDE_PROJECT_DIR}/" + filepath.ToSlash(rel)
	}
	hook := func(cmd string, timeout int) map[string]any {
		return map[string]any{"type": "command",
			"command": fmt.Sprintf("%q %s \"${CLAUDE_PROJECT_DIR}\"", self, cmd), "timeout": timeout}
	}
	settings := map[string]any{"hooks": map[string]any{
		"SessionStart": []any{map[string]any{"matcher": "startup|resume|clear",
			"hooks": []any{hook("hook-start", 10)}}},
		"UserPromptSubmit": []any{map[string]any{"hooks": []any{hook("hook-prompt", 5)}}},
		"PreToolUse": []any{map[string]any{"matcher": "Edit|Write|MultiEdit",
			"hooks": []any{hook("hook-guard", 5)}}},
	}}
	p := filepath.Join(root, ".claude", "settings.local.json")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	b, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(p, append(b, '\n'), 0o644); err == nil {
		say("cogitex : hooks câblés dans .claude/settings.local.json (%s/%s).", runtime.GOOS, runtime.GOARCH)
	}
}

// ---------------------------------------------------------------------- écriture

// Jamais `git add -A` : un agent de fond écrit dans le même arbre, et un staging en
// bloc emporterait son travail à moitié fait dans un commit qui ment.
func commitCtx(paths []string, message string) {
	wt := CogitexDir(root)
	if r := runGit(append([]string{"add", "--"}, paths...), gitOpts{Dir: wt}); !r.OK {
		die("git add : %s", r.Err)
	}
	r := runGit([]string{"commit", "--no-verify", "-q", "-m", message},
		gitOpts{Dir: wt, Timeout: 10 * time.Second})
	if !r.OK && !strings.Contains(r.Out+r.Err, "nothing to commit") {
		die("git commit : %s", firstLine(r.Err+r.Out))
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func gitUser() string {
	r := git(root, "config", "user.email")
	if r.OK && r.Out != "" {
		return strings.SplitN(r.Out, "@", 2)[0]
	}
	return "anonyme"
}

// Un push non forcé ne réussit que si le tip distant est un ancêtre de ce qu'on
// envoie : c'est déjà un compare-and-swap côté serveur. Surtout pas de
// --force-with-lease, qui, calculé sur un origin/context périmé, effacerait
// silencieusement le commit d'un coéquipier.
func pushCtx() (bool, bool, string) {
	wt := CogitexDir(root)
	for i := 0; i < 3; i++ {
		p := runGit([]string{"push", "--no-verify", "-q", "origin", Ref + ":" + Ref},
			gitOpts{Dir: wt, Timeout: 20 * time.Second, Net: true})
		if p.OK {
			return true, false, ""
		}
		e := p.Err
		// Deux rejets qui ne sont PAS des non-fast-forward : réessayer ne ferait que
		// tourner en rond. On s'arrête net et on le dit.
		switch {
		case strings.Contains(e, "GH013") || strings.Contains(strings.ToLower(e), "push protection"):
			return false, true, "refusé par la protection contre les secrets — une entrée cite probablement un jeton"
		case strings.Contains(e, "GH006") || strings.Contains(strings.ToLower(e), "protected branch"):
			return false, true, "refusé par une règle de protection de branche"
		}
		if !strings.Contains(e, "non-fast-forward") && !strings.Contains(e, "fetch first") &&
			!strings.Contains(e, "rejected") {
			return false, false, firstLine(e)
		}
		FetchContext(root, 15*time.Second)
		// Fusion, jamais rebase : le rebase réécrirait l'historique, rendrait
		// orphelins les gates déjà enregistrés par d'autres sessions, et rejouerait
		// le driver « union » une fois par commit en dupliquant des lignes.
		m := runGit([]string{"merge", "--no-verify", "--no-edit", "-q", RemoteRef},
			gitOpts{Dir: wt, Timeout: 15 * time.Second})
		if !m.OK {
			git(wt, "merge", "--abort")
			return false, true, "conflit sur la branche context — `cogitex sync` puis réessaie"
		}
	}
	return false, false, "trois tentatives rejetées"
}

func publish(what string) {
	ok, fatal, why := pushCtx()
	Trace(root, "push", map[string]any{"ok": ok, "fatal": fatal, "why": why, "path": what})
	switch {
	case ok:
		say("cogitex : publié.")
	case fatal:
		die("publication impossible — %s", why)
	default:
		say("cogitex : commit local gardé, publication différée (%s).", why)
	}
}

func cmdPush() {
	if !WorktreeReady(root) {
		die("worktree absent — lance `cogitex init`")
	}
	ahead := git(root, "rev-list", "--count", RemoteRef+".."+Ref)
	if ahead.OK && ahead.Out == "0" {
		say("cogitex : rien à publier.")
		return
	}
	say("cogitex : %s commit(s) à publier.", ahead.Out)
	publish("")
}

func cmdAdd() {
	p := positional()
	if len(p) == 0 {
		die("usage : cogitex add decision|fact|note  (JSON sur stdin)")
	}
	kind := p[0]
	if kind != "decision" && kind != "fact" && kind != "note" {
		die("usage : cogitex add decision|fact|note  (JSON sur stdin)")
	}
	if !WorktreeReady(root) {
		die("worktree absent — lance `cogitex init`")
	}

	raw := flagVal("json")
	if raw == "" {
		b, _ := readAllStdin()
		raw = b
	}
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	var e Entry
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &e) != nil {
		die("JSON illisible sur stdin")
	}

	if e.Str("date") == "" {
		e["date"] = Today()
	}
	switch kind {
	case "decision":
		if e.Str("status") == "" {
			e["status"] = "active"
		}
		dom := e.Str("domain")
		if dom == "" {
			dom = "general"
		}
		e["domain"] = Slugify(dom)
	case "fact":
		if e.Str("author") == "" {
			e["author"] = gitUser()
		}
		if _, ok := e["ttl_days"]; !ok {
			e["ttl_days"] = 30
		}
	case "note":
		if e.Str("author") == "" {
			e["author"] = gitUser()
		}
		if e.Str("status") == "" {
			e["status"] = "open"
		}
	}

	if errs := Validate(kind, e); len(errs) > 0 {
		die("entrée invalide :\n  - %s", strings.Join(errs, "\n  - "))
	}

	rel, err := PathFor(kind, e)
	if err != nil {
		die("%v", err)
	}
	wt := CogitexDir(root)
	// Même jour, même domaine, même slug : le seul vrai conflit de contenu possible.
	// On discrimine par l'acteur plutôt que par un compteur — un compteur exigerait
	// de lire l'état global, ce qui recrée exactement la course qu'on évite.
	if _, err := os.Stat(filepath.Join(wt, rel)); err == nil {
		ext := filepath.Ext(rel)
		suffix := Slugify(gitUser())
		if len(suffix) > 4 {
			suffix = suffix[:4]
		}
		rel = strings.TrimSuffix(rel, ext) + "-" + suffix + ext
		if _, err := os.Stat(filepath.Join(wt, rel)); err == nil {
			die("une entrée identique existe déjà : %s", rel)
		}
	}
	e["id"] = IDFromPath(rel)

	// `supersedes` ne supersédait rien : il ne déclenchait qu'un avertissement de
	// `doctor`, et l'ancienne règle restait `active` — donc injectée dans le brief de
	// tout le monde — jusqu'à ce que quelqu'un édite le fichier à la main. La cible
	// bascule maintenant dans le MÊME commit : les deux moitiés du remplacement
	// atterrissent ensemble ou pas du tout.
	supPath := ""
	if sup := e.Str("supersedes"); sup != "" && kind == "decision" {
		p, ok := resolveEntry(root, sup)
		if !ok {
			die("`supersedes` pointe sur un identifiant inconnu : %s", sup)
		}
		supPath = p
	}

	var body string
	if kind == "note" {
		meta := Entry{"title": e["title"], "date": e["date"], "author": e["author"],
			"status": e["status"], "tags": e["tags"]}
		body = "---\n" + Emit(meta) + "---\n\n" + e.Str("body") + "\n"
	} else {
		body = Emit(e)
	}

	jrel := JournalPathFor(gitUser(), e.Str("date"))
	err = WithLock(root, func() error {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(wt, rel)), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(wt, rel), []byte(body), 0o644); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(wt, jrel)), 0o755); err != nil {
			return err
		}
		line, _ := json.Marshal(map[string]any{"id": ULID(time.Now().UnixMilli(), rand.Int),
			"ts": time.Now().UTC().Format(time.RFC3339), "actor": gitUser(), "kind": kind,
			"ref": rel, "msg": e.Str("title")})
		f, err := os.OpenFile(filepath.Join(wt, jrel), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, _ = f.Write(append(line, '\n'))
		f.Close()
		paths := []string{rel, jrel}
		if supPath != "" {
			b, err := os.ReadFile(filepath.Join(wt, supPath))
			if err != nil {
				return err
			}
			out, ok := ReplaceScalar(b, "status", "superseded")
			if !ok {
				return fmt.Errorf("%s n'a pas de champ `status` à basculer", supPath)
			}
			if err := os.WriteFile(filepath.Join(wt, supPath), out, 0o644); err != nil {
				return err
			}
			paths = append(paths, supPath)
		}
		commitCtx(paths, kind+": "+e.Str("title"))
		return nil
	})
	if err != nil {
		die("%v", err)
	}

	say("cogitex : %s", rel)
	if supPath != "" {
		say("cogitex : %s passe à `superseded`.", IDFromPath(supPath))
	}
	Trace(root, "add", map[string]any{"kind": kind, "path": rel, "actor": gitUser()})
	h := EnsureHead(root, true)
	// Sans ce ré-épinglage, mes PROPRES écritures déplacent le gate et je me
	// bloquerais contre moi-même au premier Edit suivant.
	repinLatest(h)

	// Ingérer un document produit N entrées. Les pousser une par une déplacerait le
	// gate N fois, donc bloquerait N fois chaque coéquipier pour un seul apport.
	if hasFlag("no-push") {
		say("cogitex : commit local, non publié (`cogitex push` pour le lot).")
		return
	}
	publish(rel)
}

func readAllStdin() (string, error) {
	var sb strings.Builder
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		sb.WriteString(sc.Text())
		sb.WriteByte('\n')
	}
	// PowerShell préfixe un BOM UTF-8 à TOUT ce qu'il pousse dans le stdin d'un
	// exécutable natif. Sans ce retrait, `... | cogitex add decision` échoue sur
	// « JSON illisible » chez tous les utilisateurs Windows, et le message ne dit
	// rien des trois octets invisibles qui en sont la cause.
	return strings.TrimPrefix(sb.String(), "\uFEFF"), sc.Err()
}

func latestSessionID() string {
	entries, err := os.ReadDir(SessionsDir(root))
	if err != nil {
		return ""
	}
	best, bestScore := "", int64(0)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var s Session
		if !readJSON(filepath.Join(SessionsDir(root), e.Name()), &s) {
			continue
		}
		score := s.LastSeen
		if s.LastDeny > score {
			score = s.LastDeny
		}
		if score > bestScore {
			best, bestScore = strings.TrimSuffix(e.Name(), ".json"), score
		}
	}
	return best
}

func repinLatest(h *Head) {
	if h == nil {
		return
	}
	sid := flagVal("session")
	if sid == "" {
		sid = latestSessionID()
	}
	if sid == "" {
		return
	}
	WriteSession(root, sid, func(s *Session) {
		s.Gate, s.GateSeq, s.Acked, s.Denies, s.Muted = h.Gate, h.GateSeq, h.Gate, nil, ""
	})
}

// ---------------------------------------------------------------------- sync

func cmdSync() {
	if !HasBranch(root) {
		die("branche `context` absente — lance `cogitex init`")
	}
	cfg := LoadConfig(root)
	before := ""
	if h := ReadHead(root); h != nil {
		before = h.Gate
	} else {
		before = GateOf(root, LocalTip(root))
	}

	if !hasFlag("offline") {
		if f := FetchContext(root, 15*time.Second); !f.OK {
			say("cogitex : remote injoignable (%s) — synchronisation locale.", firstLine(f.Err))
		}
	}

	remote := RevParse(root, RemoteRef)
	if remote != "" && LocalTip(root) != remote {
		anc := git(root, "merge-base", "--is-ancestor", LocalTip(root), remote)
		if anc.Code == 0 {
			var r Result
			if WorktreeReady(root) {
				r = runGit([]string{"merge", "--ff-only", "-q", RemoteRef},
					gitOpts{Dir: CogitexDir(root), Timeout: 10 * time.Second})
			} else {
				r = git(root, "update-ref", Ref, remote)
			}
			if !r.OK {
				say("cogitex : avance rapide impossible — %s", firstLine(r.Err))
			}
		} else {
			say("cogitex : la branche locale a divergé de origin — `cogitex doctor` pour le détail.")
		}
	}

	h := EnsureHead(root, true)
	if h == nil {
		die("rien à synchroniser")
	}
	var changes []Change
	if before != "" && before != h.Gate {
		changes = DescribeChanges(root, before, h.Gate, cfg, cfg.DeltaMaxEntries)
	}

	if len(changes) == 0 {
		say("cogitex : à jour (%s, %d décisions).", short(h.Gate), h.N.Decisions)
	} else {
		say("cogitex : %d entrée(s) depuis ton dernier point de contrôle —", len(changes))
		for i, c := range changes {
			if i >= cfg.DeltaMaxEntries {
				say("  • +%d de plus — cogitex list decisions", len(changes)-cfg.DeltaMaxEntries)
				break
			}
			body := c.Rule
			if body == "" {
				body = c.Title
			}
			say("  • %s\n    %s", c.ID, body)
		}
	}

	sid := flagVal("session")
	if sid == "" {
		sid = latestSessionID()
	}
	Trace(root, "sync", map[string]any{"offline": hasFlag("offline"), "changes": len(changes),
		"from": short(before), "to": short(h.Gate), "sid": sid})
	if sid != "" {
		WriteSession(root, sid, func(s *Session) {
			s.Gate, s.GateSeq, s.Acked, s.Denies, s.Muted = h.Gate, h.GateSeq, h.Gate, nil, ""
		})
		say("cogitex : session ré-épinglée sur %s. Écritures débloquées.", short(h.Gate))
	}
}

// ---------------------------------------------------------------------- lecture

func cmdFind() {
	q := strings.Fields(strings.ToLower(strings.Join(positional(), " ")))
	if len(q) == 0 {
		die("usage : cogitex find \"<mots-clés>\"")
	}
	EnsureHead(root, false)
	type scored struct {
		r IndexRow
		n int
	}
	var hits []scored
	for _, r := range ReadIndex(root) {
		hay := strings.ToLower(r.ID + " " + r.Title + " " + r.Rule + " " +
			strings.Join(r.Tags, " ") + " " + r.Domain)
		n := 0
		for _, t := range q {
			if strings.Contains(hay, t) {
				n++
			}
		}
		if n > 0 {
			hits = append(hits, scored{r, n})
		}
	}
	if len(hits) == 0 {
		say("cogitex : aucune entrée ne correspond.")
		return
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].n > hits[j].n })
	for i, h := range hits {
		if i >= 8 {
			break
		}
		status := ""
		if h.r.Status != "" {
			status = "/" + h.r.Status
		}
		body := h.r.Rule
		if body == "" {
			body = h.r.Title
		}
		say("%s [%s%s]\n    %s", h.r.ID, h.r.Kind, status, body)
	}
}

// L'identifiant est le chemin, à l'extension près : la résolution est donc trois
// `stat`, sans index ni git.
func resolveEntry(root, id string) (string, bool) {
	for _, ext := range []string{".yaml", ".yml", ".md"} {
		if fileExists(filepath.Join(CogitexDir(root), id+ext)) {
			return id + ext, true
		}
	}
	return "", false
}

func cmdShow() {
	p := positional()
	if len(p) == 0 {
		die("usage : cogitex show <id>")
	}
	rel, ok := resolveEntry(root, p[0])
	if !ok {
		die("entrée inconnue : %s", p[0])
	}
	b, err := os.ReadFile(filepath.Join(CogitexDir(root), rel))
	if err != nil {
		die("%v", err)
	}
	os.Stdout.Write(b)
}

func cmdList() {
	kind := "decision"
	if p := positional(); len(p) > 0 {
		kind = strings.TrimSuffix(p[0], "s")
	}
	EnsureHead(root, false)
	var rows []IndexRow
	for _, r := range ReadIndex(root) {
		if r.Kind == kind {
			rows = append(rows, r)
		}
	}
	if len(rows) == 0 {
		say("cogitex : aucune entrée de type « %s ».", kind)
		return
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Date > rows[j].Date })
	for _, r := range rows {
		status := ""
		if r.Status != "" {
			status = " [" + r.Status + "]"
		}
		body := r.Rule
		if body == "" {
			body = r.Title
		}
		say("%s  %s%s\n    %s", r.Date, r.ID, status, body)
	}
}

func cmdBrief() {
	// `rebuild` et pas `BuildHead` : afficher le brief doit aussi le RÉÉCRIRE, sinon
	// head.json avance sans lui et la session suivante reçoit l'ancien.
	c, h := rebuild(root)
	if h == nil {
		die("branche `context` absente")
	}
	say("%s", RenderBrief(c, h, LoadConfig(root)))
}

// ---------------------------------------------------------------------- doctor

func cmdDoctor() {
	c, h := rebuild(root)
	if h == nil {
		die("branche `context` absente")
	}
	cfg := LoadConfig(root)
	brief := RenderBrief(c, h, cfg)
	size := len(brief)
	var problems []string

	// Le plafond du brief est la seule défense contre une dérive qui, sinon, ne se
	// voit qu'en facture de tokens six mois plus tard.
	if size > cfg.BriefMaxBytes {
		problems = append(problems, fmt.Sprintf("brief à %d octets, plafond %d", size, cfg.BriefMaxBytes))
	}

	ids := map[string]Entry{}
	for _, e := range c.Entries {
		id := e.Str("__id")
		if _, dup := ids[id]; dup {
			problems = append(problems, "identifiant en double : "+id)
		}
		ids[id] = e
		if errs := Validate(e.Str("__kind"), e); len(errs) > 0 {
			problems = append(problems, e.Str("__path")+" : "+strings.Join(errs, " ; "))
		}
	}
	for _, e := range c.Entries {
		if e.Str("__kind") != "decision" {
			continue
		}
		sup := e.Str("supersedes")
		if sup == "" {
			continue
		}
		target, ok := ids[sup]
		if !ok {
			problems = append(problems, e.Str("__id")+" supersède un identifiant inconnu : "+sup)
		} else if IsActive(target) {
			problems = append(problems, e.Str("__id")+" supersède "+sup+", qui est resté actif")
		}
	}
	var expired []string
	now := time.Now()
	for _, e := range c.Entries {
		if e.Str("__kind") == "fact" && FactExpired(e, now) {
			expired = append(expired, e.Str("__id"))
		}
	}
	if len(expired) > 0 {
		say("cogitex : %d fait(s) expiré(s), hors du brief — %s", len(expired), strings.Join(expired, ", "))
	}

	// Diagnostic de plateforme : ce système tourne sur macOS, Linux et Windows, et
	// les écarts qui font mal sont silencieux — fins de ligne converties, multiplexage
	// SSH absent, worktree non monté.
	gitv := git(root, "--version").Out
	extra := ""
	if IsWindows {
		extra = " · multiplexage SSH indisponible (fetch à pleine latence)"
	}
	say("cogitex : %s/%s · %s%s", runtime.GOOS, runtime.GOARCH, gitv, extra)

	attrs, _ := os.ReadFile(filepath.Join(CogitexDir(root), ".gitattributes"))
	if !strings.Contains(string(attrs), "* -text") {
		problems = append(problems, ".gitattributes sans `* -text` : git convertira les fins de ligne, "+
			"et un poste Windows recevra du CRLF que la fusion « union » du journal ne saura pas recoller")
	}
	if !WorktreeReady(root) {
		problems = append(problems, "worktree `.cogitex` absent — `cogitex init`")
	}
	// La panne la plus coûteuse du lot, et celle qui frappe quelqu'un d'autre : un
	// `cogitex init` d'une version précédente a pu marquer le dépôt, et tout git
	// antérieur à 2.48 y refuse alors la moindre commande.
	if b, err := os.ReadFile(filepath.Join(root, ".git", "config")); err == nil && relWorktreesRe.Match(b) {
		problems = append(problems, "`extensions.relativeWorktrees` dans .git/config : un coéquipier "+
			"en git < 2.48 ne peut plus lancer AUCUNE commande git dans ce dépôt — `cogitex init` le retire")
	}

	say("cogitex : brief %d/%d octets · %d décisions · %d faits · %d notes",
		size, cfg.BriefMaxBytes, h.N.Decisions, h.N.Facts, h.N.Notes)
	if len(problems) == 0 {
		say("cogitex : rien à signaler.")
		return
	}
	for _, p := range problems {
		warn("  ✗ %s", p)
	}
	os.Exit(1)
}

// Une écriture produit des objets libres ; gc.auto est désactivé sur nos appels pour
// qu'un ramasse-miettes ne se déclenche jamais DANS un hook. Il faut donc pouvoir le
// déclencher soi-même, au moment choisi.
func cmdCompact() {
	r := runGit([]string{"gc", "--prune=now", "--quiet"},
		gitOpts{Dir: CogitexDir(root), Timeout: 120 * time.Second})
	if r.OK {
		say("cogitex : base compactée.")
	} else {
		say("cogitex : compactage impossible — %s", firstLine(r.Err))
	}
}

// ---------------------------------------------------------------------- debug

func cmdDebug() {
	sub := "status"
	if p := positional(); len(p) > 0 {
		sub = p[0]
	}
	marker, logf := DebugFile(root), LogFile(root)
	switch sub {
	case "on":
		_ = os.MkdirAll(CacheDir(root), 0o755)
		_ = os.WriteFile(marker, []byte("cogitex debug on — "+time.Now().Format(time.RFC3339)+"\n"), 0o644)
		rel, _ := filepath.Rel(root, logf)
		say("cogitex : traçage activé → %s", filepath.ToSlash(rel))
	case "off":
		_ = os.Remove(marker)
		say("cogitex : traçage désactivé.")
	case "clear":
		_ = os.Remove(logf)
		_ = os.Remove(logf + ".1")
		say("cogitex : journal effacé.")
	case "tail":
		n := 30
		if v := flagVal("n"); v != "" {
			fmt.Sscanf(v, "%d", &n)
		}
		b, err := os.ReadFile(logf)
		if err != nil {
			say("cogitex : aucun journal — `cogitex debug on` puis rejoue.")
			return
		}
		lines := []string{}
		for _, l := range strings.Split(string(b), "\n") {
			if strings.TrimSpace(l) != "" {
				lines = append(lines, l)
			}
		}
		if len(lines) > n {
			lines = lines[len(lines)-n:]
		}
		for _, l := range lines {
			var rec map[string]any
			if json.Unmarshal([]byte(l), &rec) != nil {
				say("%s", l)
				continue
			}
			ts, _ := rec["ts"].(string)
			if len(ts) >= 19 {
				ts = ts[11:19]
			}
			dur := "       "
			if ms, ok := rec["ms"].(float64); ok {
				dur = fmt.Sprintf("%5dms", int(ms))
			}
			var kv []string
			for _, k := range sortedKeys(rec) {
				if k == "ts" || k == "ms" || k == "event" || k == "pid" {
					continue
				}
				kv = append(kv, fmt.Sprintf("%s=%v", k, rec[k]))
			}
			say("%s  %-14s %s  %s", ts, rec["event"], dur, strings.Join(kv, " "))
		}
	default:
		state := "inactif"
		if DebugOn(root) {
			state = "ACTIF"
		}
		size := ""
		if st, err := os.Stat(logf); err == nil {
			size = fmt.Sprintf(" · journal %d octets", st.Size())
		}
		say("cogitex : traçage %s%s", state, size)
		say("  cogitex debug on|off|tail|clear")
	}
}

func sortedKeys(m map[string]any) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
