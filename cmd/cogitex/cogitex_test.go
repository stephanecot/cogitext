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
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
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
