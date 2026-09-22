package main

// Le mode brouillon.
//
// Deux propriétés portent tout le reste, et chacune a son test : un brouillon ne
// déplace JAMAIS le gate, donc ne bloque personne ; et celui d'un autre n'est visible
// nulle part. Le troisième garde-fou est budgétaire : une pile personnelle ne doit
// pas pouvoir coûter une ligne de décision à l'équipe.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDraftPathIsTheIdentifier(t *testing.T) {
	e := Entry{"title": "Dates are UTC", "domain": "Data", "date": "2026-09-21"}
	got, err := DraftPathFor("Stéphane.Cot", "decision", e)
	if err != nil || got != "drafts/stephane-cot/decisions/data/2026-09-21-dates-are-utc.yaml" {
		t.Fatalf("chemin inattendu : %q (%v)", got, err)
	}
	// LA propriété : la promotion est un retrait de préfixe, prouvé et non espéré.
	promoted, _ := PathFor("decision", e)
	if StripDraft(got) != promoted {
		t.Fatalf("StripDraft(%q) = %q, attendu %q", got, StripDraft(got), promoted)
	}
	if DraftOwner(got) != "stephane-cot" {
		t.Fatalf("propriétaire mal lu : %q", DraftOwner(got))
	}
}

func TestKindFromPathSeesThroughDrafts(t *testing.T) {
	for p, want := range map[string]string{
		"decisions/api/2026-01-01-x.yaml":           "decision",
		"facts/2026-01-01-x.yaml":                   "fact",
		"notes/2026-01-01-x.md":                     "note",
		"drafts/me/decisions/api/2026-01-01-x.yaml": "decision",
		"drafts/me/facts/2026-01-01-x.yaml":         "fact",
		"drafts/me/notes/2026-01-01-x.md":           "note",
		"drafts/x.yaml":                             "draft", // malformé : ne matche aucune nature
	} {
		if got := KindFromPath(p); got != want {
			t.Fatalf("KindFromPath(%q) = %q, attendu %q", p, got, want)
		}
	}
	if DraftOwner("drafts/x.yaml") != "" || DraftOwner("decisions/api/x.yaml") != "" {
		t.Fatal("un chemin qui n'est pas un brouillon n'a pas de propriétaire")
	}
}

// Ce test échoue le jour où quelqu'un ajoute « drafts » à rulePaths — c'est-à-dire le
// jour où un brouillon se mettrait à bloquer l'équipe.
func TestDraftsNeverEnterRulePaths(t *testing.T) {
	e := Entry{"title": "x", "domain": "api", "date": "2026-01-01"}
	for _, kind := range []string{"decision", "fact", "note"} {
		p, err := DraftPathFor("me", kind, e)
		if err != nil {
			t.Fatal(err)
		}
		for _, rp := range rulePaths {
			if strings.HasPrefix(p, rp+"/") {
				t.Fatalf("%q tombe sous le pathspec du gate %q", p, rp)
			}
		}
	}
}

func draftEntry(owner, title string) (string, string) {
	rel := "drafts/" + owner + "/decisions/api/2026-01-01-" + Slugify(title) + ".yaml"
	return rel, "id: " + IDFromPath(rel) + "\ntitle: " + title +
		"\ntype: convention\nstatus: active\nscope: global\ndomain: api\ndate: 2026-01-01\n" +
		"decision: A " + title + " rule.\n"
}

// Le point de filtrage unique couvre tous les lecteurs à la fois.
func TestAnotherAuthorsDraftIsInvisible(t *testing.T) {
	bin := buildCtx(t)
	root := initRepo(t, bin)
	for _, d := range [][2]string{{"tester", "Mine"}, {"someone-else", "Theirs"}} {
		rel, body := draftEntry(d[0], d[1])
		landEntry(t, root, rel, body)
	}

	out := run(t, bin, root, "list", "drafts")
	if !strings.Contains(out, "Mine") {
		t.Fatalf("mes brouillons doivent être listés :\n%s", out)
	}
	if strings.Contains(out, "Theirs") {
		t.Fatalf("le brouillon d'un autre n'est jamais visible :\n%s", out)
	}
	if o := run(t, bin, root, "find", "Theirs"); !strings.Contains(o, "aucune entrée") {
		t.Fatalf("`find` ne doit pas atteindre le brouillon d'un autre :\n%s", o)
	}
	if o := run(t, bin, root, "brief"); strings.Contains(o, "Theirs") {
		t.Fatalf("le brief ne doit pas porter le brouillon d'un autre :\n%s", o)
	}
	// Et les miens ne comptent pas comme des règles d'équipe.
	if o := run(t, bin, root, "list", "decisions"); strings.Contains(o, "Mine") {
		t.Fatalf("`list decisions` mélange les brouillons aux règles :\n%s", o)
	}
}

// Sans identité, AUCUN brouillon n'est visible — pas même les siens. Fermé par
// défaut : sinon deux machines sans email git partageraient un espace commun.
func TestNoIdentityMeansNoDraftsAtAll(t *testing.T) {
	bin := buildCtx(t)
	root := initRepo(t, bin)
	rel, body := draftEntry("tester", "Mine")
	landEntry(t, root, rel, body)

	cmd := exec.Command(bin, "list", "drafts", "--root", root)
	cmd.Env = append(os.Environ(),
		"COGITEX_ACTOR=", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
	// L'identité locale du dépôt est retirée pour ce seul appel.
	if out, err := exec.Command("git", "-C", root, "config", "--unset", "user.email").CombinedOutput(); err != nil {
		t.Fatalf("git config --unset : %v\n%s", err, out)
	}
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "aucune entrée") {
		t.Fatalf("sans identité, aucun brouillon ne doit être visible :\n%s", out)
	}
}

func TestDraftDoesNotMoveTheGateButPromotionDoes(t *testing.T) {
	bin := buildCtx(t)
	root := initRepo(t, bin)
	addEntry(t, bin, root, "decision",
		`{"title":"Base","slug":"base","type":"convention","domain":"api","scope":"global","decision":"A first rule.","rationale":"x"}`)
	before := GateOf(root, LocalTip(root))

	addEntry(t, bin, root, "decision",
		`{"title":"Maybe later","slug":"maybe","type":"convention","domain":"api","scope":"global","decision":"Perhaps errors follow RFC 7807.","rationale":"Testing it out."}`,
		"--draft")
	if after := GateOf(root, LocalTip(root)); after != before {
		t.Fatalf("un brouillon a déplacé le gate : %s → %s", short(before), short(after))
	}

	id := "drafts/tester/decisions/api/" + Today() + "-maybe"
	run(t, bin, root, "promote", id, "--no-push", "--force")
	if after := GateOf(root, LocalTip(root)); after == before {
		t.Fatal("la promotion doit déplacer le gate — c'est l'instant où l'équipe est interrompue")
	}
	if out := run(t, bin, root, "show", "decisions/api/"+Today()+"-maybe"); !strings.Contains(out, "RFC 7807") {
		t.Fatalf("l'entrée promue doit être au chemin réel :\n%s", out)
	}
	if _, err := tryRun(bin, root, "show", id); err == nil {
		t.Fatal("le chemin du brouillon ne doit plus exister")
	}
}

// La promotion déplace le fichier et n'édite que la ligne `id:`. Un Parse + Emit
// détruirait le corps markdown d'une note et perdrait les champs inconnus.
func TestPromotePreservesEverythingButTheID(t *testing.T) {
	bin := buildCtx(t)
	root := initRepo(t, bin)
	addEntry(t, bin, root, "note",
		`{"title":"Draft note","slug":"draft-note","body":"Tried a compound index.\nIt fails because the column is optional.","severity":"high"}`,
		"--draft")

	id := "drafts/tester/notes/" + Today() + "-draft-note"
	src := run(t, bin, root, "show", id)
	run(t, bin, root, "promote", id, "--no-push")
	got := run(t, bin, root, "show", "notes/"+Today()+"-draft-note")

	if !strings.Contains(got, "It fails because the column is optional.") {
		t.Fatalf("le corps markdown a été perdu :\n%s", got)
	}
	if !strings.Contains(got, "severity: high") {
		t.Fatalf("un champ inconnu a été perdu :\n%s", got)
	}
	a, b := strings.Split(src, "\n"), strings.Split(got, "\n")
	if len(a) != len(b) {
		t.Fatalf("le nombre de lignes a changé : %d → %d", len(a), len(b))
	}
	diff := 0
	for i := range a {
		if a[i] != b[i] {
			diff++
		}
	}
	if diff != 1 {
		t.Fatalf("%d lignes modifiées, une seule attendue (`id:`)", diff)
	}
}

// La promotion déplace le gate ; sans ré-épinglage, son auteur se bloquerait
// lui-même au premier Edit suivant.
func TestPromoteRepinsTheAuthorsSession(t *testing.T) {
	bin := buildCtx(t)
	root := initRepo(t, bin)
	addEntry(t, bin, root, "decision",
		`{"title":"Base","slug":"base","type":"convention","domain":"api","scope":"global","decision":"A first rule.","rationale":"x"}`)
	runHook(t, bin, "hook-start", root, `{"session_id":"s1","source":"startup"}`)

	addEntry(t, bin, root, "note", `{"title":"Scratch","slug":"scratch","body":"Maybe."}`, "--draft")
	run(t, bin, root, "promote", "drafts/tester/notes/"+Today()+"-scratch", "--no-push")

	if out := runHook(t, bin, "hook-guard", root, `{"session_id":"s1","tool_name":"Edit"}`); out != "" {
		t.Fatalf("l'auteur ne doit pas se bloquer sur sa propre promotion :\n%s", out)
	}
}

func TestDropRefusesWhatIsNotMyDraft(t *testing.T) {
	bin := buildCtx(t)
	root := initRepo(t, bin)
	addEntry(t, bin, root, "decision",
		`{"title":"Team rule","slug":"team","type":"convention","domain":"api","scope":"global","decision":"A team rule.","rationale":"x"}`)
	addEntry(t, bin, root, "note", `{"title":"Mine","slug":"mine","body":"Scratch."}`, "--draft")

	if out, err := tryRun(bin, root, "drop", "decisions/api/"+Today()+"-team"); err == nil {
		t.Fatalf("`drop` ne doit jamais supprimer une règle d'équipe :\n%s", out)
	}
	if out, err := tryRun(bin, root, "drop", "drafts/someone-else/notes/2026-01-01-x"); err == nil {
		t.Fatalf("`drop` ne doit pas toucher au brouillon d'un autre :\n%s", out)
	}
	run(t, bin, root, "drop", "drafts/tester/notes/"+Today()+"-mine", "--no-push")
	if out := run(t, bin, root, "list", "drafts"); !strings.Contains(out, "aucune entrée") {
		t.Fatalf("le brouillon devait disparaître :\n%s", out)
	}
	run(t, bin, root, "show", "decisions/api/"+Today()+"-team") // la règle d'équipe est intacte
}

func TestAddDraftRefusesWithoutAnIdentity(t *testing.T) {
	bin := buildCtx(t)
	root := initRepo(t, bin)
	if out, err := exec.Command("git", "-C", root, "config", "--unset", "user.email").CombinedOutput(); err != nil {
		t.Fatalf("git config --unset : %v\n%s", err, out)
	}
	cmd := exec.Command(bin, "add", "note", "--draft", "--no-push", "--root", root)
	cmd.Stdin = strings.NewReader(`{"title":"x","body":"y"}`)
	cmd.Env = append(os.Environ(),
		"COGITEX_ACTOR=", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("un brouillon sans propriétaire doit être refusé :\n%s", out)
	}
	if !strings.Contains(string(out), "propriétaire") {
		t.Fatalf("le message doit dire quoi faire :\n%s", out)
	}
	if fileExists(filepath.Join(root, ".cogitex", "drafts", "anonyme")) {
		t.Fatal("aucun espace `anonyme` ne doit être créé")
	}
}

// L'INVARIANT budgétaire : `draftsMaxBytes` est un plafond, jamais un plancher. Une
// pile personnelle ne peut pas coûter une ligne de décision à l'équipe.
func TestDraftsNeverEatTheDecisionBudget(t *testing.T) {
	mk := func(n int, draft bool) []Entry {
		var out []Entry
		for i := 0; i < n; i++ {
			e := Entry{
				"__kind": "decision", "title": "Rule", "type": "convention", "status": "active",
				"scope": "global", "domain": "api", "date": "2026-01-01",
				"decision": "A rule of representative length, roughly eighty characters here ok.",
			}
			if draft {
				e["__draft"] = "true"
				e["__id"] = "drafts/me/decisions/api/2026-01-01-draft-" + itoa(i)
			} else {
				e["__id"] = "decisions/api/2026-01-01-rule-" + itoa(i)
			}
			out = append(out, e)
		}
		return out
	}
	cfg := LoadConfig(t.TempDir())
	countDecisionLines := func(brief string) int {
		n := 0
		for _, l := range strings.Split(brief, "\n") {
			if strings.HasPrefix(l, "decisions/") {
				n++
			}
		}
		return n
	}

	teamOnly := Corpus{Entries: mk(500, false)}
	withDrafts := Corpus{Entries: append(mk(500, false), mk(500, true)...)}
	h := &Head{Gate: strings.Repeat("a", 40), N: Counts{Decisions: 500}}
	hd := &Head{Gate: strings.Repeat("a", 40), N: Counts{Decisions: 500, Drafts: 500}}

	a, b := RenderBrief(teamOnly, h, cfg), RenderBrief(withDrafts, hd, cfg)
	if len(b) > cfg.BriefMaxBytes {
		t.Fatalf("brief hors plafond avec des brouillons : %d octets", len(b))
	}
	if got, want := countDecisionLines(b), countDecisionLines(a); got != want {
		t.Fatalf("les brouillons ont coûté %d lignes de décision à l'équipe (%d contre %d)",
			want-got, got, want)
	}
	// Effacés entièrement, cadrage et navigation compris : le brief redevient octet
	// pour octet celui qu'il serait sans eux.
	if a != b {
		t.Fatal("avec 500 brouillons et un corpus déjà tronqué, le brief doit être " +
			"identique à celui sans brouillons")
	}
}

// Le bloc doit NIER le cadrage d'en-tête, qui dit que ce qui suit lie le lecteur.
func TestBriefLabelsDraftsAsBindingNobody(t *testing.T) {
	c := Corpus{Entries: []Entry{
		{"__kind": "decision", "__id": "decisions/api/2026-01-01-a", "title": "A",
			"decision": "A team rule.", "type": "convention", "status": "active",
			"scope": "global", "domain": "api", "date": "2026-01-01"},
		{"__kind": "decision", "__draft": "true", "__id": "drafts/me/decisions/api/2026-01-01-b",
			"title": "B", "decision": "A draft rule.", "type": "convention", "status": "active",
			"scope": "global", "domain": "api", "date": "2026-01-01"},
	}}
	brief := RenderBrief(c, &Head{Gate: strings.Repeat("a", 40), N: Counts{Decisions: 1, Drafts: 1}},
		LoadConfig(t.TempDir()))

	if !strings.Contains(brief, "bind nobody") {
		t.Fatalf("le bloc des brouillons doit nier le cadrage :\n%s", brief)
	}
	iDecisions := strings.Index(brief, "## Decisions (active)")
	iDrafts := strings.Index(brief, "## Your drafts")
	iReaching := strings.Index(brief, "## Reaching the rest")
	if !(iDecisions < iDrafts && iDrafts < iReaching) {
		t.Fatal("les brouillons viennent après les règles d'équipe, avant la navigation")
	}
	// Chaque ligne se ré-étiquette toute seule, même sortie de son contexte.
	if !strings.Contains(brief, "drafts/me/decisions/api/2026-01-01-b · A draft rule.") {
		t.Fatalf("la ligne doit porter son propre préfixe :\n%s", brief)
	}
}
