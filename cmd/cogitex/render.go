package main

// Rendu du bloc injecté, plafonné EN OCTETS.
//
// Un plafond exprimé en tokens n'est pas vérifiable au moment de la construction ; un
// plafond en octets l'est, donc il tient. 6 800 octets ≈ 1 700 tokens, soit ~1 % d'une
// fenêtre de 200 k : la part qu'on accepte de payer en permanence.
//
// La propriété à défendre : ce coût est PLAT quelle que soit la taille du corpus. Il
// croît avec le nombre d'invariants, jamais avec le nombre d'entrées.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const framing = "These rules were already settled by the team and they bind you. If your plan\n" +
	"contradicts one, say so and stop — do not deviate in silence."

const staleness = "## Staleness\n" +
	"This session is pinned to the context above. If a teammate publishes a decision\n" +
	"or a fact, Edit/Write are denied until `cogitex sync`, which prints only the delta.\n" +
	"Notes, drafts and journal entries never block anyone.\n" +
	"Nothing above is the whole corpus: `cogitex find` covers 100% of it, including what\n" +
	"was truncated here — the team's entries and your own drafts. Other people's drafts\n" +
	"are theirs alone: you never see them, and they never bind you."

// Le bloc des brouillons doit NIER le cadrage d'en-tête, qui dit « these rules bind
// you » et colore tout ce qui suit. Sans cette contre-phrase, un griffonnage
// personnel se lirait comme une règle que l'équipe a arrêtée.
const draftFraming = "## Your drafts (yours alone — not team rules, they bind nobody)\n" +
	"These are your own unfinished entries. The team has not agreed to them, other\n" +
	"sessions never see them, and they never block anyone. Do not cite one as a rule\n" +
	"and do not enforce one on anybody. `cogitex promote <id>` is what turns a draft\n" +
	"into a team rule — that, and only that, makes it binding."

func reaching(n Counts) string {
	s := "## Reaching the rest\n" +
		"one entry in full          cogitex show <id>\n" +
		"search everything          cogitex find \"<keywords>\"\n" +
		fmt.Sprintf("notes (%d) and journal (%d), never auto-loaded   cogitex list notes\n", n.Notes, n.Journal)
	if n.Drafts > 0 {
		s += fmt.Sprintf("your drafts (%d), yours and never shared      cogitex list drafts\n", n.Drafts)
	}
	return s + "record a new one           cogitex add decision|fact|note [--draft]"
}

func short(sha string) string {
	if sha == "" || sha == "none" {
		return "none"
	}
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func pinned(e Entry) bool { return e.Str("pinned") == "true" }

// Tri de troncature, déterministe pour que le brief soit identique d'une machine à
// l'autre : épinglé d'abord, puis les invariants transverses, puis la récence.
func rankDecisions(ds []Entry) {
	sort.SliceStable(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		if pinned(a) != pinned(b) {
			return pinned(a)
		}
		ga, gb := ScopeOf(a) == "global", ScopeOf(b) == "global"
		if ga != gb {
			return ga
		}
		if a.Str("date") != b.Str("date") {
			return a.Str("date") > b.Str("date")
		}
		return a.Str("__id") < b.Str("__id")
	})
}

func ruleOf(e Entry) string {
	if r := e.Str("decision"); r != "" {
		return r
	}
	if b := summarize(e.Str("body"), 120); b != "" {
		return b
	}
	return e.Str("title")
}

// On part de toutes les décisions et on en retire par le bas jusqu'à repasser sous le
// plafond. Rien n'est perdu : la queue « +N more » et `cogitex find` gardent 100 % du
// corpus atteignable.
func fitDecisions(decisions []Entry, budget int) ([]string, int) {
	var lines []string
	used, kept := 0, 0
	for _, d := range decisions {
		line := d.Str("__id") + " · " + ruleOf(d)
		if used+len(line)+1 > budget-60 {
			break
		}
		lines = append(lines, line)
		used += len(line) + 1
		kept++
	}
	if kept < len(decisions) {
		lines = append(lines, fmt.Sprintf("+%d more — cogitex find \"<keywords>\"", len(decisions)-kept))
	}
	return lines, kept
}

func fitDrafts(drafts []Entry, max int) []string {
	var lines []string
	used, kept := 0, 0
	for _, d := range drafts {
		line := d.Str("__id") + " · " + ruleOf(d)
		if used+len(line)+1 > max {
			break
		}
		lines = append(lines, line)
		used += len(line) + 1
		kept++
	}
	if kept < len(drafts) {
		lines = append(lines, fmt.Sprintf("+%d more — cogitex list drafts", len(drafts)-kept))
	}
	return lines
}

func RenderBrief(c Corpus, h *Head, cfg Config) string {
	now := time.Now()
	decisions := c.ActiveDecisions()
	rankDecisions(decisions)
	facts := c.LiveFacts(now)
	sort.SliceStable(facts, func(i, j int) bool { return facts[i].Str("date") > facts[j].Str("date") })
	if len(facts) > 10 {
		facts = facts[:10]
	}
	// Un brief composé uniquement de MES griffonnages, sous un en-tête qui annonce des
	// règles qui lient, serait pire que pas de brief du tout : la condition reste celle
	// du corpus d'équipe.
	if len(decisions) == 0 && len(facts) == 0 {
		return ""
	}

	var factLines []string
	for _, f := range facts {
		line := f.Str("date") + " · " + f.Str("title")
		if aff := f.List("affects"); len(aff) > 0 {
			line += " [" + strings.Join(aff, " ") + "]"
		}
		factLines = append(factLines, line)
	}

	drafts := c.MyDrafts()
	sort.SliceStable(drafts, func(i, j int) bool {
		if drafts[i].Str("date") != drafts[j].Str("date") {
			return drafts[i].Str("date") > drafts[j].Str("date")
		}
		return drafts[i].Str("__id") < drafts[j].Str("__id")
	})
	var draftBlock []string
	if cfg.DraftsInBrief && len(drafts) > 0 {
		draftBlock = fitDrafts(drafts, cfg.DraftsMaxBytes)
	}

	// Quand les brouillons cèdent la place, ils la cèdent ENTIÈREMENT : ni bloc, ni
	// compte dans l'en-tête, ni ligne de navigation. Sinon leur simple existence
	// coûterait encore une décision à l'équipe.
	assemble := func(decisionLines, draftLines []string) string {
		n := h.N
		if len(draftLines) == 0 {
			n.Drafts = 0
		}
		header := fmt.Sprintf("# Shared context (cogitex) — %s · %d decisions · %d facts · %d notes · %d journal",
			short(h.Gate), n.Decisions, n.Facts, n.Notes, n.Journal)
		if n.Drafts > 0 {
			header += fmt.Sprintf(" · %d drafts", n.Drafts)
		}
		out := append([]string{header, "", framing, "", "## Decisions (active)"}, decisionLines...)
		if len(factLines) > 0 {
			out = append(out, "", "## Facts (volatile)")
			out = append(out, factLines...)
		}
		if len(draftLines) > 0 {
			out = append(out, "", draftFraming)
			out = append(out, draftLines...)
		}
		return strings.Join(append(out, "", reaching(n), "", staleness), "\n")
	}

	budget := cfg.BriefMaxBytes - len(assemble(nil, draftBlock))
	lines, kept := fitDecisions(decisions, budget)

	// L'INVARIANT : `draftsMaxBytes` est un plafond, jamais un plancher. Si les règles
	// de l'équipe ont dû être tronquées, les brouillons s'effacent — cadrage et
	// navigation compris — et le brief redevient octet pour octet celui qu'il serait
	// sans eux. Zéro ligne de décision perdue, que j'en aie un ou cinq cents.
	//
	// Leur auteur les retrouve par `cogitex list drafts`, que le brief lui rappelle
	// dès que la place le permet.
	if kept < len(decisions) && len(draftBlock) > 0 {
		draftBlock = nil
		budget = cfg.BriefMaxBytes - len(assemble(nil, nil))
		lines, _ = fitDecisions(decisions, budget)
	}
	return assemble(lines, draftBlock)
}

type Change struct {
	Status  string
	Path    string
	ID      string
	Kind    string
	Title   string
	Rule    string
	Affects []string
}

// Le delta : le SEUL chemin qui se répète, donc celui qu'il faut border le plus. Une
// injection de milieu de session s'accumule dans le contexte jusqu'à la fin,
// contrairement au brief de démarrage qui est payé une fois.
func RenderDelta(changes []Change, max int) string {
	if len(changes) == 0 {
		return ""
	}
	n := len(changes)
	head := changes
	if n > max {
		head = changes[:max]
	}
	plural := "entries"
	if n == 1 {
		plural = "entry"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "cogitex: %d new %s since your last checkpoint.\n", n, plural)
	for _, c := range head {
		t := c.Title
		if t == "" {
			t = "(untitled)"
		}
		fmt.Fprintf(&sb, "  • %s — %s\n", c.ID, t)
	}
	if n > max {
		fmt.Fprintf(&sb, "  • +%d more — cogitex sync\n", n-max)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// Ce qui a changé entre deux gates. Volontairement paresseux : on liste d'abord les
// CHEMINS (une commande, sortie minuscule), et on ne lit le contenu que des entrées
// réellement affichées. Une fusion de quarante décisions ne doit jamais se
// transformer en quarante lectures.
func DescribeChanges(root, from, to string, cfg Config, limit int) []Change {
	if from == "" || to == "" || from == to {
		return nil
	}
	// Le couple de gates est immuable, donc le résultat aussi : on ne le calcule
	// qu'une fois. Sans ce mémo, une session périmée repayait un `git diff` et jusqu'à
	// huit `git show` à CHAQUE prompt — et sous Copilot, où la session reste périmée à
	// dessein jusqu'au premier refus, la facture se répétait tour après tour.
	memo := DeltaFile(root, from, to, limit, cfg.FactsBlock)
	if cached := CachedChanges(root, from, to, cfg, limit); len(cached) > 0 {
		return cached
	}
	paths := []string{"decisions", "facts"}
	if !cfg.FactsBlock {
		paths = []string{"decisions"}
	}
	args := []string{"diff", "--name-status"}
	if from == "none" {
		args = append(args, to)
	} else {
		args = append(args, from, to)
	}
	args = append(args, "--")
	args = append(args, paths...)
	r := runGit(args, gitOpts{Dir: root, Timeout: 4 * time.Second})
	if !r.OK || r.Out == "" {
		return nil
	}
	var out []Change
	for _, line := range strings.Split(r.Out, "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) < 2 || parts[0] == "" || parts[0][0] == 'D' {
			continue
		}
		p := parts[len(parts)-1]
		out = append(out, Change{Status: parts[0][:1], Path: p, ID: IDFromPath(p), Kind: KindFromPath(p)})
	}
	for i := range out {
		if i >= limit {
			break
		}
		r := git(root, "show", to+":"+out[i].Path)
		if !r.OK {
			continue
		}
		e := Parse(r.Out)
		out[i].Title = e.Str("title")
		out[i].Affects = e.List("affects")
		out[i].Rule = e.Str("decision")
		if out[i].Rule == "" {
			out[i].Rule = e.Str("body")
		}
	}
	// Un échec git ne se mémoïse pas : il serait figé pour toujours sur un couple de
	// gates qui, lui, ne bougera plus.
	if len(out) > 0 {
		_ = os.MkdirAll(filepath.Dir(memo), 0o755)
		_ = writeJSONAtomic(memo, out)
	}
	return out
}

// Le delta DÉJÀ calculé, ou rien. Aucun sous-processus, aucune attente : c'est ce
// qui permet au garde de s'en servir sans jamais risquer un tour. Le calculer là
// coûterait un `git diff` et jusqu'à huit `git show`, sous un hook plafonné à cinq
// secondes — et un dépassement fait échouer ouvert, donc laisse passer l'écriture
// que le garde était censé retenir.
func CachedChanges(root, from, to string, cfg Config, limit int) []Change {
	var cached []Change
	if readJSON(DeltaFile(root, from, to, limit, cfg.FactsBlock), &cached) {
		return cached
	}
	return nil
}
