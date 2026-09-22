package main

// Lecture du corpus et construction de l'index dérivé.
//
// Le corpus se lit sur le disque, dans le worktree .cogitex. C'est précisément ce pour
// quoi le worktree a été choisi : lire par plomberie git coûterait le même temps,
// mais ferait passer chaque lecture du modèle par un appel shell dont la commande ET
// la sortie entrent dans le transcript.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Corpus struct {
	Entries []Entry
	Journal int
}

func walkFiles(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func ReadCorpus(root string) Corpus {
	base := CogitexDir(root)
	c := Corpus{}
	for _, kind := range []string{"decisions", "facts", "notes"} {
		for _, abs := range walkFiles(filepath.Join(base, kind)) {
			rel := filepath.ToSlash(strings.TrimPrefix(abs, base+string(filepath.Separator)))
			if !extRe.MatchString(rel) {
				continue
			}
			b, err := os.ReadFile(abs)
			if err != nil {
				continue // un fichier illisible ne fait pas échouer la construction
			}
			e := Parse(string(b))
			e["__path"] = rel
			e["__id"] = IDFromPath(rel)
			e["__kind"] = KindFromPath(rel)
			if e.Str("__kind") == "decision" && e.Str("domain") == "" {
				if parts := strings.Split(rel, "/"); len(parts) > 1 {
					e["domain"] = parts[1]
				}
			}
			c.Entries = append(c.Entries, e)
		}
	}
	for _, abs := range walkFiles(filepath.Join(base, "journal")) {
		if b, err := os.ReadFile(abs); err == nil {
			for _, l := range strings.Split(string(b), "\n") {
				if strings.TrimSpace(l) != "" {
					c.Journal++
				}
			}
		}
	}
	return c
}

func (c Corpus) ActiveDecisions() []Entry {
	var out []Entry
	for _, e := range c.Entries {
		if e.Str("__kind") == "decision" && IsActive(e) {
			out = append(out, e)
		}
	}
	return out
}

func (c Corpus) LiveFacts(now time.Time) []Entry {
	var out []Entry
	for _, e := range c.Entries {
		if e.Str("__kind") == "fact" && !FactExpired(e, now) {
			out = append(out, e)
		}
	}
	return out
}

type IndexRow struct {
	ID     string   `json:"id"`
	Kind   string   `json:"kind"`
	Domain string   `json:"domain"`
	Status string   `json:"status"`
	Title  string   `json:"title"`
	Rule   string   `json:"rule"`
	Tags   []string `json:"tags"`
	Date   string   `json:"date"`
}

func BuildHead(root string, c Corpus) *Head {
	tip := WorldTip(root)
	if tip == "" {
		return nil
	}
	now := time.Now()
	notes := 0
	for _, e := range c.Entries {
		if e.Str("__kind") == "note" {
			notes++
		}
	}
	h := &Head{
		Tip: tip, Local: LocalTip(root),
		Gate: GateOf(root, tip), GateSeq: GateSeqOf(root, tip),
		BuiltAt: now.UnixMilli(),
		N: Counts{Decisions: len(c.ActiveDecisions()), Facts: len(c.LiveFacts(now)),
			Notes: notes, Journal: c.Journal},
	}
	_ = os.MkdirAll(CacheDir(root), 0o755)

	// L'index est une ligne par entrée : la recherche est alors un parcours trivial,
	// sans étape de construction, sans dépendance et sans dérive possible.
	var sb strings.Builder
	for _, e := range c.Entries {
		rule := e.Str("decision")
		if rule == "" {
			rule = e.Str("body")
		}
		row := IndexRow{ID: e.Str("__id"), Kind: e.Str("__kind"), Domain: e.Str("domain"),
			Status: e.Str("status"), Title: e.Str("title"), Rule: rule,
			Tags: e.List("tags"), Date: e.Str("date")}
		b, _ := json.Marshal(row)
		sb.Write(b)
		sb.WriteByte('\n')
	}
	_ = writeAtomic(IndexFile(root), []byte(sb.String()))
	_ = writeJSONAtomic(HeadFile(root), h)
	return h
}

// Reconstruit seulement si le tip a bougé : la reconstruction coûte ~10 ms, la
// vérification en coûte 3.
func EnsureHead(root string, force bool) *Head {
	tip := WorldTip(root)
	if tip == "" {
		return nil
	}
	if !force {
		if prev := ReadHead(root); prev != nil && prev.Tip == tip && prev.Local == LocalTip(root) {
			if _, err := os.Stat(BriefFile(root)); err == nil {
				return prev
			}
		}
	}
	c := ReadCorpus(root)
	h := BuildHead(root, c)
	if h != nil {
		_ = writeAtomic(BriefFile(root), []byte(RenderBrief(c, h, LoadConfig(root))))
	}
	return h
}

func ReadBrief(root string) string {
	b, err := os.ReadFile(BriefFile(root))
	if err != nil {
		return ""
	}
	return string(b)
}

func ReadIndex(root string) []IndexRow {
	b, err := os.ReadFile(IndexFile(root))
	if err != nil {
		return nil
	}
	var rows []IndexRow
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		var r IndexRow
		if json.Unmarshal([]byte(l), &r) == nil {
			rows = append(rows, r)
		}
	}
	return rows
}

// ------------------------------------------------------------------ configuration

type Config struct {
	RefreshThrottleMs int  `json:"refreshThrottleMs"`
	FactsBlock        bool `json:"factsBlock"`
	BriefMaxBytes     int  `json:"briefMaxBytes"`
	DeltaMaxEntries   int  `json:"deltaMaxEntries"`
	MaxDeniesPerGate  int  `json:"maxDeniesPerGate"`
}

func LoadConfig(root string) Config {
	c := Config{RefreshThrottleMs: 90000, FactsBlock: true, BriefMaxBytes: 6800,
		DeltaMaxEntries: 8, MaxDeniesPerGate: 2}
	readJSON(filepath.Join(CacheDir(root), "config.json"), &c)
	readJSON(filepath.Join(root, ".claude", "cogitex", "config.json"), &c)
	return c
}
