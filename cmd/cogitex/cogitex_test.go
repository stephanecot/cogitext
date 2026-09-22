package main

// Tests du contexte partagé.
//
// Deux familles portent l'essentiel de la valeur : l'aller-retour emit/parse sur un
// échantillon hostile, parce qu'une corruption silencieuse du YAML fausserait l'index
// sans que rien ne signale rien ; et la matrice « échouer ouvert » du garde, parce
// qu'une panne d'infrastructure ne doit JAMAIS coûter une édition à l'utilisateur.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEmitParseRoundTrip(t *testing.T) {
	src := Entry{
		"id": "api/2026-09-21-x", "title": `A title: with "quotes", a comma, and a colon`,
		"type": "architecture", "status": "active", "scope": "global", "domain": "api",
		"date": "2026-09-21", "decision": "key: value — and a # hash",
		"rationale":    "single line, with, commas",
		"consequences": []string{"c1, with comma", "c2: with colon"},
		"tags":         []string{"a-b", "c"},
	}
	back := Parse(Emit(src))
	for _, k := range []string{"id", "title", "type", "status", "scope", "domain", "date", "decision", "rationale"} {
		if back.Str(k) != src.Str(k) {
			t.Fatalf("champ %s : %q ≠ %q", k, back.Str(k), src.Str(k))
		}
	}
	if got := back.List("consequences"); len(got) != 2 || got[0] != "c1, with comma" || got[1] != "c2: with colon" {
		t.Fatalf("consequences mal restituées : %#v", got)
	}
}

func TestLiteralAngleBracketIsNotABlockScalar(t *testing.T) {
	// Stocker la valeur littérale « > » comme un scalaire bloc corrompt l'index en
	// silence : c'est un vrai bug déjà rencontré, il reste couvert.
	if got := Parse(Emit(Entry{"title": ">"})).Str("title"); got != ">" {
		t.Fatalf("attendu \">\", obtenu %q", got)
	}
}

func TestParseHandwrittenBlockScalar(t *testing.T) {
	if got := Parse("title: x\nbody: |\n  une ligne\n  une autre\n").Str("body"); got != "une ligne\nune autre" {
		t.Fatalf("scalaire bloc mal lu : %q", got)
	}
}

func TestSlugifyIsAsciiLowercase(t *testing.T) {
	// Sur un système insensible à la casse, « -Auth » et « -auth » sont un seul
	// fichier ici et deux sur Linux : une collision invisible jusqu'à la CI.
	for in, want := range map[string]string{
		"Décision Générale": "decision-generale",
		"  Ça va !! ":       "ca-va",
		"Auth":              "auth",
	} {
		if got := Slugify(in); got != want {
			t.Fatalf("Slugify(%q) = %q, attendu %q", in, got, want)
		}
	}
}

func TestPathForIsTheIdentifier(t *testing.T) {
	p, err := PathFor("decision", Entry{"title": "Dates are UTC", "domain": "Data", "date": "2026-09-21"})
	if err != nil || p != "decisions/data/2026-09-21-dates-are-utc.yaml" {
		t.Fatalf("chemin inattendu : %q (%v)", p, err)
	}
}

func TestDecisionCharCap(t *testing.T) {
	base := Entry{"title": "t", "type": "convention", "status": "active", "domain": "d", "date": "2026-09-21"}
	ok := Entry{}
	for k, v := range base {
		ok[k] = v
	}
	ok["decision"] = strings.Repeat("x", DecisionMaxChars)
	if errs := Validate("decision", ok); len(errs) != 0 {
		t.Fatalf("le plafond exact doit passer : %v", errs)
	}
	ok["decision"] = strings.Repeat("x", DecisionMaxChars+1)
	if errs := Validate("decision", ok); len(errs) != 1 {
		t.Fatalf("un caractère de trop doit échouer, obtenu %v", errs)
	}
}

func TestEnglishOnly(t *testing.T) {
	// Un corpus à moitié traduit rend la recherche inutilisable : la moitié des
	// entrées ne répond plus aux mots-clés de l'autre. La détection porte sur les
	// mots-outils, pas sur les accents.
	base := Entry{"title": "t", "type": "convention", "status": "active", "domain": "d", "date": "2026-09-21"}
	en := Entry{}
	for k, v := range base {
		en[k] = v
	}
	en["decision"] = "Every stored date is UTC, never local time."
	if errs := Validate("decision", en); len(errs) != 0 {
		t.Fatalf("l'anglais doit passer : %v", errs)
	}
	fr := Entry{}
	for k, v := range base {
		fr[k] = v
	}
	fr["decision"] = "Toute date stockee est en UTC et jamais locale."
	found := false
	for _, e := range Validate("decision", fr) {
		if strings.Contains(e, "anglais") {
			found = true
		}
	}
	if !found {
		t.Fatal("la prose française doit être refusée")
	}
	if LooksFrench("Interview with Amélie Rousseau about the radar") != nil {
		t.Fatal("faux positif sur un nom propre accentué")
	}
}

func TestFactExpiry(t *testing.T) {
	now := time.Now()
	day := func(n int) string { return now.AddDate(0, 0, -n).Format("2006-01-02") }
	if FactExpired(Entry{"date": day(5), "ttl_days": float64(30)}, now) {
		t.Fatal("un fait récent ne doit pas expirer")
	}
	if !FactExpired(Entry{"date": day(40), "ttl_days": float64(30)}, now) {
		t.Fatal("un fait de 40 jours avec ttl 30 doit expirer")
	}
	if !FactExpired(Entry{"date": day(20), "ttl_days": float64(14)}, now) {
		t.Fatal("le ttl explicite doit être respecté")
	}
}

func TestULIDSortsByTime(t *testing.T) {
	seq := func() int { return 0 }
	a, b := ULID(1_000_000, seq), ULID(2_000_000, seq)
	if !(a < b) {
		t.Fatalf("le tri lexicographique doit suivre le temps : %s ≥ %s", a, b)
	}
	if len(a) != 26 {
		t.Fatalf("longueur ULID = %d", len(a))
	}
}

func TestBriefStaysUnderCapAt500Entries(t *testing.T) {
	// La propriété à défendre : le coût est PLAT quelle que soit la taille du corpus.
	var c Corpus
	for i := 0; i < 500; i++ {
		c.Entries = append(c.Entries, Entry{
			"__kind": "decision", "__id": "api/2026-01-01-rule-" + Slugify(string(rune('a'+i%26))) + itoa(i),
			"title": "Rule", "decision": "A rule of representative length, roughly eighty characters here ok.",
			"type": "architecture", "status": "active", "scope": "global", "domain": "api", "date": "2026-01-01",
		})
	}
	h := &Head{Gate: strings.Repeat("a", 40), N: Counts{Decisions: 500}}
	brief := RenderBrief(c, h, LoadConfig(t.TempDir()))
	if len(brief) > 6800 {
		t.Fatalf("brief hors plafond : %d octets", len(brief))
	}
	if !strings.Contains(brief, "more — cogitex find") {
		t.Fatal("la troncature doit être annoncée")
	}
	// Sans cette phrase, un plafond apprend silencieusement au modèle que ce qu'il
	// voit est tout ce qui existe.
	if !strings.Contains(brief, "covers 100% of it") {
		t.Fatal("la phrase qui rend la troncature sûre doit rester")
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestEmptyCorpusInjectsNothing(t *testing.T) {
	if RenderBrief(Corpus{}, &Head{Gate: "x"}, LoadConfig(t.TempDir())) != "" {
		t.Fatal("un corpus vide ne doit rien injecter du tout")
	}
}

func TestDeltaIsBounded(t *testing.T) {
	var ch []Change
	for i := 0; i < 30; i++ {
		ch = append(ch, Change{ID: "d/" + itoa(i), Title: "title"})
	}
	out := RenderDelta(ch, 8)
	if n := len(strings.Split(out, "\n")); n != 10 {
		t.Fatalf("attendu 10 lignes (en-tête + 8 + renvoi), obtenu %d", n)
	}
	if !strings.Contains(out, "+22 more") {
		t.Fatalf("le renvoi manque : %s", out)
	}
}

// ------------------------------------------------------------- « échouer ouvert »

func buildCtx(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ctx")
	// -buildvcs=false : la suite ne doit pas dépendre de l'état du dépôt qui l'héberge.
	// Un git qui bronche sur ce dépôt faisait échouer la compilation, donc TOUS les
	// tests, pour une raison sans rapport avec le code testé.
	out, err := exec.Command("go", "build", "-buildvcs=false", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build : %v\n%s", err, out)
	}
	return bin
}

func emptyRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init : %v\n%s", err, out)
	}
	return dir
}

func runHook(t *testing.T, bin, sub, root, payload string) string {
	t.Helper()
	cmd := exec.Command(bin, sub, root)
	cmd.Stdin = strings.NewReader(payload)
	out, _ := cmd.Output() // le code de sortie doit toujours être 0 ; on vérifie stdout
	return string(out)
}

// ------------------------------------------------------- piloter un vrai corpus

// Un dépôt avec la branche `context` montée et une identité git connue — ce que
// `emptyRepo` ne donne pas, et dont tout ce qui écrit dans le corpus a besoin.
//
// `--root` plutôt que le répertoire courant : le test reste insensible à
// CLAUDE_PROJECT_DIR, que l'agent qui le lance a peut-être exporté.
func initRepo(t *testing.T, bin string) string {
	t.Helper()
	dir := emptyRepo(t)
	for _, kv := range [][2]string{{"user.email", "tester@cogitex.local"}, {"user.name", "tester"}} {
		if out, err := exec.Command("git", "-C", dir, "config", kv[0], kv[1]).CombinedOutput(); err != nil {
			t.Fatalf("git config %s : %v\n%s", kv[0], err, out)
		}
	}
	run(t, bin, dir, "init", "--no-hooks")
	return dir
}

func run(t *testing.T, bin, root string, args ...string) string {
	t.Helper()
	out, err := tryRun(bin, root, args...)
	if err != nil {
		t.Fatalf("cogitex %s : %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func tryRun(bin, root string, args ...string) (string, error) {
	cmd := exec.Command(bin, append(args, "--root", root)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func addEntry(t *testing.T, bin, root, kind, payload string, flags ...string) string {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"add", kind, "--no-push", "--root", root}, flags...)...)
	cmd.Stdin = strings.NewReader(payload)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("add %s : %v\n%s", kind, err, out)
	}
	return string(out)
}

const decisionB = `id: decisions/api/2026-01-01-b
title: B
type: convention
status: active
scope: global
domain: api
date: 2026-01-01
decision: The rule a teammate just landed.
`

// Écrit et commite une entrée DIRECTEMENT dans le worktree, comme le ferait
// l'atterrissage d'un fetch : le tip bouge sans que cette machine soit passée par
// `add`.
func landEntry(t *testing.T, root, rel, body string) {
	t.Helper()
	wt := filepath.Join(root, ".cogitex")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(wt, rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, rel), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "--", rel}, {"commit", "-q", "-m", "landed"}} {
		c := exec.Command("git", append([]string{"-C", wt}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %s : %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

func TestGuardFailsOpen(t *testing.T) {
	bin := buildCtx(t)
	for _, tc := range []struct{ label, payload string }{
		{"stdin vide", ""},
		{"stdin non JSON", "pas du json"},
		{"session_id absent", `{"tool_name":"Edit"}`},
		{"charge inattendue", `{"session_id":"s","tool_input":null}`},
		{"dépôt sans branche context", `{"session_id":"s","tool_name":"Edit"}`},
		{"camelCase sans session", `{"toolName":"edit"}`},
		{"camelCase, dépôt sans branche", `{"sessionId":"s","toolName":"edit"}`},
	} {
		t.Run(tc.label, func(t *testing.T) {
			if out := runHook(t, bin, "hook-guard", emptyRepo(t), tc.payload); out != "" {
				t.Fatalf("le garde doit rester silencieux, obtenu : %q", out)
			}
		})
	}
}

func seedStale(t *testing.T, root string) {
	t.Helper()
	cache := filepath.Join(root, ".claude", "cache", "cogitex")
	if err := os.MkdirAll(filepath.Join(cache, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	head, _ := json.Marshal(Head{Gate: strings.Repeat("b", 40), GateSeq: 5, Tip: strings.Repeat("b", 40)})
	_ = os.WriteFile(filepath.Join(cache, "head.json"), head, 0o644)
	sess, _ := json.Marshal(Session{Gate: strings.Repeat("a", 40), GateSeq: 3})
	_ = os.WriteFile(filepath.Join(cache, "sessions", "s.json"), sess, 0o644)
}

func TestGuardDisabledSwitch(t *testing.T) {
	bin, root := buildCtx(t), emptyRepo(t)
	seedStale(t, root)
	_ = os.WriteFile(filepath.Join(root, ".claude", "cache", "cogitex", "DISABLED"), []byte("test"), 0o644)
	if out := runHook(t, bin, "hook-guard", root, `{"session_id":"s","tool_name":"Edit"}`); out != "" {
		t.Fatalf("le coupe-circuit doit autoriser, obtenu : %q", out)
	}
}

func TestGuardDeniesAndBreaksCircuit(t *testing.T) {
	// Le seul chemin de refus du garde, et la porte de sortie qui l'accompagne.
	bin, root := buildCtx(t), emptyRepo(t)
	seedStale(t, root)
	payload := `{"session_id":"s","tool_name":"Edit"}`

	out := runHook(t, bin, "hook-guard", root, payload)
	if strings.Count(strings.TrimRight(out, "\n"), "\n") != 0 {
		t.Fatalf("une seule ligne de sortie attendue : %q", out)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("sortie non JSON : %v — %q", err, out)
	}
	hso := d["hookSpecificOutput"].(map[string]any)
	if hso["permissionDecision"] != "deny" {
		t.Fatalf("attendu deny, obtenu %v", hso["permissionDecision"])
	}
	reason := hso["permissionDecisionReason"].(string)
	for _, want := range []string{" sync", "--offline", "2 decision"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("le message de refus doit contenir %q :\n%s", want, reason)
		}
	}

	runHook(t, bin, "hook-guard", root, payload)
	third := runHook(t, bin, "hook-guard", root, payload)
	var d3 map[string]any
	_ = json.Unmarshal([]byte(third), &d3)
	if d3["hookSpecificOutput"].(map[string]any)["permissionDecision"] != "ask" {
		t.Fatalf("au 3e refus la main doit repasser à l'humain, obtenu : %s", third)
	}
}

func TestSessionStartEmitsOneJSONLine(t *testing.T) {
	// Claude Code cesse de lire stdout après la première ligne commençant par « { » :
	// une sortie parasite tronquerait le bloc injecté au mauvais endroit.
	bin := buildCtx(t)
	out := runHook(t, bin, "hook-start", emptyRepo(t), `{"session_id":"s","source":"startup"}`)
	if out == "" {
		return // dépôt non adopté : silence légitime
	}
	if strings.Count(strings.TrimRight(out, "\n"), "\n") != 0 || out[0] != '{' {
		t.Fatalf("une seule ligne JSON attendue : %q", out)
	}
	var v any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("JSON invalide : %v", err)
	}
}

func TestGuardHotPathUnder50ms(t *testing.T) {
	// Sans cette barrière, « ultra optimisé » se dégrade en silence à la première
	// fonctionnalité ajoutée au hook.
	bin, root := buildCtx(t), emptyRepo(t)
	payload := `{"session_id":"perf","tool_name":"Edit"}`
	runHook(t, bin, "hook-guard", root, payload)
	const n = 20
	start := time.Now()
	for i := 0; i < n; i++ {
		runHook(t, bin, "hook-guard", root, payload)
	}
	per := time.Since(start) / n
	if per > 50*time.Millisecond {
		t.Fatalf("garde à %v par appel, budget 50 ms", per)
	}
	t.Logf("garde : %v par appel", per)
}

// ------------------------------------------------------- les deux dialectes

// Copilot envoie du camelCase et attend la décision à la racine de l'objet, sans
// l'enveloppe `hookSpecificOutput` de Claude Code. Se tromper de dialecte ne
// produit pas une erreur : le refus est simplement ignoré, et le garde devient
// décoratif sans que rien ne le signale.
func TestCopilotGuardSpeaksItsOwnDialect(t *testing.T) {
	bin, root := buildCtx(t), emptyRepo(t)
	seedStale(t, root)

	out := runHook(t, bin, "hook-guard", root, `{"sessionId":"s","toolName":"edit","toolArgs":{"path":"a.go"}}`)
	var d map[string]any
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("sortie non JSON : %v — %q", err, out)
	}
	if _, wrapped := d["hookSpecificOutput"]; wrapped {
		t.Fatalf("l'enveloppe de Claude Code n'a rien à faire ici : %s", out)
	}
	if d["permissionDecision"] != "deny" {
		t.Fatalf("attendu deny à la racine, obtenu : %s", out)
	}
	if !strings.Contains(d["permissionDecisionReason"].(string), " sync") {
		t.Fatalf("le refus doit dire comment en sortir : %s", out)
	}
}

// Le drapeau tranche quand le payload ne suffit pas — et, accessoirement, il ne
// doit pas avaler la racine passée en argument juste après lui.
func TestDialectFlagOverridesPayload(t *testing.T) {
	bin, root := buildCtx(t), emptyRepo(t)
	seedStale(t, root)

	cmd := exec.Command(bin, "hook-guard", "--copilot", root)
	cmd.Stdin = strings.NewReader(`{"session_id":"s","tool_name":"Edit"}`)
	raw, _ := cmd.Output()

	var d map[string]any
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("sortie non JSON : %v — %q", err, raw)
	}
	if d["permissionDecision"] != "deny" {
		t.Fatalf("--copilot doit imposer le dialecte Copilot, et la racine suivre : %s", raw)
	}
}

// Le plugin sert tous les projets ; un projet qui embarque son propre cogitex a
// déjà câblé les siens. Sans cette règle, chaque tour paierait deux fois le même
// hook — et deux refus comptés pour une seule écriture ouvrent le garde trop tôt.
func TestPluginStandsDownWhenProjectWiresItsOwnHooks(t *testing.T) {
	bin, root := buildCtx(t), emptyRepo(t)
	seedStale(t, root)
	settings := filepath.Join(root, ".claude", "settings.local.json")
	if err := os.WriteFile(settings,
		[]byte(`{"hooks":{"PreToolUse":[{"hooks":[{"command":"cogitex hook-guard"}]}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "hook-guard", root)
	cmd.Stdin = strings.NewReader(`{"session_id":"s","tool_name":"Edit"}`)
	cmd.Env = append(os.Environ(), "CLAUDE_PLUGIN_ROOT="+t.TempDir())
	out, _ := cmd.Output()
	if len(out) != 0 {
		t.Fatalf("le plugin doit s'effacer devant le câblage du projet, obtenu : %q", out)
	}

	// Sans câblage dans le projet, en revanche, c'est bien le plugin qui garde.
	if err := os.Remove(settings); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(bin, "hook-guard", root)
	cmd.Stdin = strings.NewReader(`{"session_id":"s","tool_name":"Edit"}`)
	cmd.Env = append(os.Environ(), "CLAUDE_PLUGIN_ROOT="+t.TempDir())
	if out, _ = cmd.Output(); len(out) == 0 {
		t.Fatal("sans câblage dans le projet, le plugin doit garder")
	}
}

// PowerShell préfixe un BOM UTF-8 à tout ce qu'il pousse dans le stdin d'un
// exécutable natif. Sans son retrait, `... | cogitex add decision` échoue chez
// TOUS les utilisateurs Windows, sur un message qui ne dit rien des trois octets
// invisibles qui en sont la cause.
func TestStdinToleratesAUTF8BOM(t *testing.T) {
	bin, root := buildCtx(t), emptyRepo(t)
	payload := "\uFEFF" + `{"session_id":"s","tool_name":"Edit"}`

	// Le hook doit rester silencieux (dépôt non adopté) plutôt que de partir sur un
	// payload vide : ici on vérifie surtout qu'il ne panique pas et lit bien la clé.
	if out := runHook(t, bin, "hook-guard", root, payload); out != "" {
		t.Fatalf("attendu le silence, obtenu : %q", out)
	}

	seedStale(t, root)
	out := runHook(t, bin, "hook-guard", root, payload)
	var d map[string]any
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("un BOM en tête de payload rend le garde aveugle : %v — %q", err, out)
	}
	if d["hookSpecificOutput"].(map[string]any)["permissionDecision"] != "deny" {
		t.Fatalf("attendu deny malgré le BOM, obtenu : %s", out)
	}
}

// ------------------------------------------------------------- dérivés cohérents

// Le bug qu'on ne voit jamais : une commande avance head.json sans régénérer le
// brief, et la session suivante reçoit l'ANCIEN bloc — le court-circuit de
// EnsureHead est satisfait (tip identique, brief présent) et ne reconstruit rien.
func TestAReadCommandNeverLeavesTheBriefBehind(t *testing.T) {
	bin := buildCtx(t)
	root := initRepo(t, bin)
	addEntry(t, bin, root, "decision",
		`{"title":"First","type":"convention","domain":"api","scope":"global","decision":"The first rule.","rationale":"Because."}`)

	// Un coéquipier publie ; le fetch atterrit.
	landEntry(t, root, "decisions/api/2026-01-01-b.yaml", decisionB)

	for _, cmd := range []string{"brief", "doctor"} {
		t.Run(cmd, func(t *testing.T) {
			root := initRepo(t, bin)
			addEntry(t, bin, root, "decision",
				`{"title":"First","type":"convention","domain":"api","scope":"global","decision":"The first rule.","rationale":"Because."}`)
			landEntry(t, root, "decisions/api/2026-01-01-b.yaml", decisionB)

			run(t, bin, root, cmd)

			brief, err := os.ReadFile(filepath.Join(root, ".claude", "cache", "cogitex", "brief.txt"))
			if err != nil {
				t.Fatalf("brief.txt absent après `%s` : %v", cmd, err)
			}
			if !strings.Contains(string(brief), "teammate just landed") {
				t.Fatalf("`%s` a avancé head.json en laissant le brief en arrière :\n%s", cmd, brief)
			}
		})
	}
}

// Ne tester que le brief rendait la disparition de l'index définitive : `find` et
// `list` répondaient « aucune entrée » pour toujours.
func TestASuppressedIndexIsRebuilt(t *testing.T) {
	bin := buildCtx(t)
	root := initRepo(t, bin)
	addEntry(t, bin, root, "decision",
		`{"title":"Slug identifiers","type":"convention","domain":"api","scope":"global","decision":"Identifiers are slugs.","rationale":"Collisions."}`)

	index := filepath.Join(root, ".claude", "cache", "cogitex", "index.ndjson")
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	if out := run(t, bin, root, "find", "slug"); !strings.Contains(out, "slug-identifiers") {
		t.Fatalf("l'index doit se reconstruire tout seul, obtenu :\n%s", out)
	}
	if !fileExists(index) {
		t.Fatal("index.ndjson n'a pas été réécrit")
	}
}

// `worktree add --relative-paths` réussit sur git ≥ 2.48 et inscrit
// `extensions.relativeWorktrees` dans le .git/config du PROJET HÔTE. Tout git plus
// ancien refuse alors la moindre commande dans ce dépôt — `git status` compris — avec
// un message qui ne mentionne pas cogitex. Un poste récent casse le dépôt pour tous
// les coéquipiers en git < 2.48, et c'est eux qui le découvrent.
func TestInitNeverBrandsTheHostRepository(t *testing.T) {
	bin := buildCtx(t)
	root := initRepo(t, bin)

	cfg, err := os.ReadFile(filepath.Join(root, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if relWorktreesRe.Match(cfg) {
		t.Fatalf("init a marqué le dépôt hôte :\n%s", cfg)
	}
}

// Et les dépôts déjà marqués par une version précédente doivent guérir — sans passer
// par git, puisque c'est précisément git qui refuse de tourner chez la victime.
func TestInitHealsAnAlreadyBrandedRepository(t *testing.T) {
	bin := buildCtx(t)
	root := initRepo(t, bin)
	cfgPath := filepath.Join(root, ".git", "config")

	cfg, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, append(cfg, []byte("[extensions]\n\trelativeWorktrees = true\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	// `doctor` sort en 1 quand il a quelque chose à signaler : c'est le cas ici.
	out, err := tryRun(bin, root, "doctor")
	if err == nil {
		t.Fatalf("doctor devait échouer sur un dépôt marqué :\n%s", out)
	}
	if !strings.Contains(out, "relativeWorktrees") {
		t.Fatalf("doctor doit nommer la marque :\n%s", out)
	}

	run(t, bin, root, "init", "--no-hooks")

	cfg, err = os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if relWorktreesRe.Match(cfg) {
		t.Fatalf("la marque devait être retirée :\n%s", cfg)
	}
	if !WorktreeReady(root) {
		t.Fatal("le worktree doit être remonté après la réparation")
	}
}

// ------------------------------------------------------- réécrire sans détruire

// `Entry` est une map précisément pour qu'une entrée écrite par une version plus
// récente survive à une relecture par une version plus ancienne. `Emit` démentait
// cette promesse en silence — et c'est ce qui rend une réécriture sûre.
func TestEmitKeepsUnknownFields(t *testing.T) {
	back := Parse(Emit(Entry{
		"title": "t", "date": "2026-01-01",
		"severity":  "high",                // scalaire d'une version future
		"reviewers": []string{"ana", "bo"}, // liste d'une version future
	}))
	if got := back.Str("severity"); got != "high" {
		t.Fatalf("champ scalaire inconnu perdu : %q", got)
	}
	if got := back.List("reviewers"); len(got) != 2 || got[0] != "ana" {
		t.Fatalf("liste inconnue perdue : %#v", got)
	}
	// `slug` ne fait pas partie de l'entrée : il ne sert qu'à nommer le fichier.
	if Parse(Emit(Entry{"title": "t", "slug": "x"})).Str("slug") != "" {
		t.Fatal("`slug` ne doit pas être figé dans le corpus")
	}
}

func TestReplaceScalarTouchesOneLineOnly(t *testing.T) {
	src := []byte("id: decisions/api/x\nstatus: active\ndecision: The $1 rule stays.\n")
	out, ok := ReplaceScalar(src, "status", "superseded")
	if !ok {
		t.Fatal("le champ devait être trouvé")
	}
	want := "id: decisions/api/x\nstatus: superseded\ndecision: The $1 rule stays.\n"
	if string(out) != want {
		t.Fatalf("réécriture non chirurgicale :\n%s", out)
	}
	if _, ok := ReplaceScalar(src, "absent", "x"); ok {
		t.Fatal("un champ absent ne doit pas être signalé comme remplacé")
	}
}

// Le corps d'une note peut contenir une ligne qui ressemble à un champ — souvent
// parce qu'elle cite une entrée. L'en-tête seul doit être touché.
func TestReplaceScalarIgnoresANoteBody(t *testing.T) {
	src := []byte("---\nid: notes/2026-01-01-x\nstatus: open\n---\n\nTried this:\nstatus: active\nand it failed.\n")
	out, _ := ReplaceScalar(src, "status", "closed")
	if !strings.Contains(string(out), "---\nid: notes/2026-01-01-x\nstatus: closed\n---") {
		t.Fatalf("l'en-tête n'a pas été mis à jour :\n%s", out)
	}
	if !strings.Contains(string(out), "\nstatus: active\nand it failed.") {
		t.Fatalf("le corps a été touché :\n%s", out)
	}
}

// `supersedes` ne faisait que déclencher un avertissement de `doctor` : l'ancienne
// règle restait `active`, donc injectée dans le brief de toute l'équipe, jusqu'à ce
// que quelqu'un édite le fichier à la main.
func TestSupersedesFlipsItsTargetInTheSameCommit(t *testing.T) {
	bin := buildCtx(t)
	root := initRepo(t, bin)
	addEntry(t, bin, root, "decision",
		`{"title":"Old rule","slug":"old-rule","type":"convention","domain":"api","scope":"global","decision":"Errors are plain strings.","rationale":"History."}`)

	old := "decisions/api/" + Today() + "-old-rule"
	addEntry(t, bin, root, "decision",
		`{"title":"New rule","slug":"new-rule","type":"convention","domain":"api","scope":"global","decision":"Errors follow RFC 7807.","rationale":"One shape.","supersedes":"`+old+`"}`)

	if out := run(t, bin, root, "show", old); !strings.Contains(out, "status: superseded") {
		t.Fatalf("la cible devait basculer :\n%s", out)
	}
	if out := run(t, bin, root, "brief"); strings.Contains(out, "plain strings") {
		t.Fatalf("une règle supersédée ne doit plus être injectée :\n%s", out)
	}
	// Les deux moitiés du remplacement atterrissent ensemble : doctor ne signalait
	// justement que le cas où l'une manquait.
	if out, err := tryRun(bin, root, "doctor"); err != nil {
		t.Fatalf("doctor doit être vert après une supersession propre :\n%s", out)
	}
}

func TestSupersedesRefusesAnUnknownTarget(t *testing.T) {
	bin := buildCtx(t)
	root := initRepo(t, bin)
	cmd := exec.Command(bin, "add", "decision", "--no-push", "--root", root)
	cmd.Stdin = strings.NewReader(
		`{"title":"New","type":"convention","domain":"api","scope":"global","decision":"A rule.","rationale":"x","supersedes":"decisions/api/nope"}`)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("une cible inconnue doit être refusée :\n%s", out)
	}
	if !strings.Contains(string(out), "identifiant inconnu") {
		t.Fatalf("message peu clair :\n%s", out)
	}
}
