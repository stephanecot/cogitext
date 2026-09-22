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
	"sort"
	"strings"
	"time"
)

const framing = "These rules were already settled by the team and they bind you. If your plan\n" +
	"contradicts one, say so and stop — do not deviate in silence."

const staleness = "## Staleness\n" +
	"This session is pinned to the context above. If a teammate publishes a decision\n" +
	"or a fact, Edit/Write are denied until `cogitex sync`, which prints only the delta.\n" +
	"Notes and journal entries never block anyone.\n" +
	"Nothing above is the whole corpus: `cogitex find` covers 100% of it, including what\n" +
	"was truncated here."

func reaching(n Counts) string {
	return "## Reaching the rest\n" +
		"one entry in full          cogitex show <id>\n" +
		"search everything          cogitex find \"<keywords>\"\n" +
		fmt.Sprintf("notes (%d) and journal (%d), never auto-loaded   cogitex list notes\n", n.Notes, n.Journal) +
		"record a new one           cogitex add decision|fact|note"
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

func RenderBrief(c Corpus, h *Head, cfg Config) string {
	now := time.Now()
	decisions := c.ActiveDecisions()
	rankDecisions(decisions)
	facts := c.LiveFacts(now)
	sort.SliceStable(facts, func(i, j int) bool { return facts[i].Str("date") > facts[j].Str("date") })
	if len(facts) > 10 {
		facts = facts[:10]
	}
	if len(decisions) == 0 && len(facts) == 0 {
		return ""
	}

	header := fmt.Sprintf("# Shared context (cogitex) — %s · %d decisions · %d facts · %d notes · %d journal",
		short(h.Gate), h.N.Decisions, h.N.Facts, h.N.Notes, h.N.Journal)

	var factLines []string
	for _, f := range facts {
		line := f.Str("date") + " · " + f.Str("title")
		if aff := f.List("affects"); len(aff) > 0 {
			line += " [" + strings.Join(aff, " ") + "]"
		}
		factLines = append(factLines, line)
	}

	fixed := strings.Join(append([]string{header, "", framing, "", "## Decisions (active)", "", "",
		"## Facts (volatile)"}, append(factLines, "", reaching(h.N), "", staleness)...), "\n")
	budget := cfg.BriefMaxBytes - len(fixed)

	// On part de toutes les décisions et on en retire par le bas jusqu'à repasser sous
	// le plafond. Rien n'est perdu : la queue « +N more » et `cogitex find` gardent 100 %
	// du corpus atteignable.
	var lines []string
	used, kept := 0, 0
	for _, d := range decisions {
		rule := d.Str("decision")
		if rule == "" {
			rule = d.Str("title")
		}
		line := d.Str("__id") + " · " + rule
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

	out := append([]string{header, "", framing, "", "## Decisions (active)"}, lines...)
	if len(factLines) > 0 {
		out = append(out, "", "## Facts (volatile)")
		out = append(out, factLines...)
	}
	out = append(out, "", reaching(h.N), "", staleness)
	return strings.Join(out, "\n")
}

type Change struct {
	Status string
	Path   string
	ID     string
	Kind   string
	Title  string
	Rule   string
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
		out[i].Rule = e.Str("decision")
		if out[i].Rule == "" {
			out[i].Rule = e.Str("body")
		}
	}
	return out
}
