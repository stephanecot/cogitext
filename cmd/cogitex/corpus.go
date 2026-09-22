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
			if strings.HasSuffix(rel, ".md") && e.Str("body") == "" {
				e["body"] = MarkdownBody(b)
			}
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
	c.Journal = countJournal(root, base)
	return c
}

// Un shard de journal ne change qu'en s'allongeant, et on n'en veut qu'un nombre de
// lignes. Les relire TOUS à chaque reconstruction faisait croître le coût avec
// l'historique, indéfiniment, pour un chiffre qui n'apparaît que dans l'en-tête du
// brief. Le mémo vit dans le cache, local à la machine.
type journalShard struct {
	Mod   int64 `json:"mod"`
	Size  int64 `json:"size"`
	Lines int   `json:"n"`
}

func countJournal(root, base string) int {
	memo := map[string]journalShard{}
	readJSON(filepath.Join(CacheDir(root), "journal.json"), &memo)
	next := make(map[string]journalShard, len(memo))
	total, changed := 0, false
	for _, abs := range walkFiles(filepath.Join(base, "journal")) {
		st, err := os.Stat(abs)
		if err != nil {
			continue
		}
		key := filepath.ToSlash(strings.TrimPrefix(abs, base+string(filepath.Separator)))
		if m, ok := memo[key]; ok && m.Mod == st.ModTime().UnixNano() && m.Size == st.Size() {
			next[key], total = m, total+m.Lines
			continue
		}
		changed = true
		n := 0
		if b, err := os.ReadFile(abs); err == nil {
			for _, l := range strings.Split(string(b), "\n") {
				if strings.TrimSpace(l) != "" {
					n++
				}
			}
		}
		next[key] = journalShard{Mod: st.ModTime().UnixNano(), Size: st.Size(), Lines: n}
		total += n
	}
	if changed || len(next) != len(memo) {
		_ = os.MkdirAll(CacheDir(root), 0o755)
		_ = writeJSONAtomic(filepath.Join(CacheDir(root), "journal.json"), next)
	}
	return total
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
	// Ce qu'on cherche sans jamais l'afficher : le « pourquoi » d'une entrée vit dans
	// `rationale`, `context` et `consequences`, et la moitié des recherches porte
	// dessus. Les lister ici les rend trouvables sans alourdir la ligne de résultat.
	Text string `json:"text,omitempty"`
}

// La ligne de résultat montre une phrase, pas un document : le corps d'une note peut
// faire trente lignes, et `find` doit en rendre huit au total.
func summarize(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if len(s) > max {
		return strings.TrimSpace(s[:max]) + "…"
	}
	return s
}

// Les tips sont passés, jamais recalculés : l'appelant les connaît déjà, et les
// redemander ici coûtait deux sous-processus git de plus à chaque reconstruction,
// pour une réponse identique à la milliseconde près.
func BuildHead(root string, c Corpus, tip, local string) *Head {
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
		Tip: tip, Local: local,
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
			rule = summarize(e.Str("body"), 160)
		}
		row := IndexRow{ID: e.Str("__id"), Kind: e.Str("__kind"), Domain: e.Str("domain"),
			Status: e.Str("status"), Title: e.Str("title"), Rule: rule,
			Tags: e.List("tags"), Date: e.Str("date"),
			Text: strings.Join(append([]string{e.Str("rationale"), e.Str("context"), e.Str("body")},
				e.List("consequences")...), " ")}
		b, _ := json.Marshal(row)
		sb.Write(b)
		sb.WriteByte('\n')
	}
	_ = writeAtomic(IndexFile(root), []byte(sb.String()))
	_ = writeJSONAtomic(HeadFile(root), h)
	return h
}

// Les trois fichiers dérivés — index, head et brief — se reconstruisent ENSEMBLE,
// et c'est le seul endroit qui les écrit tous les trois.
//
// Les séparer a déjà coûté un bug silencieux : une commande qui avançait head.json
// sans régénérer le brief laissait le court-circuit ci-dessous satisfait (tip
// identique, brief présent), et la session suivante recevait l'ANCIEN brief sans que
// rien ne le signale.
func rebuild(root, tip, local string) (Corpus, *Head) {
	c := ReadCorpus(root)
	h := BuildHead(root, c, tip, local)
	if h != nil {
		_ = writeAtomic(BriefFile(root), []byte(RenderBrief(c, h, LoadConfig(root))))
	}
	return c, h
}

func rebuildNow(root string) (Corpus, *Head) { return rebuild(root, WorldTip(root), LocalTip(root)) }

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Reconstruit seulement si le tip a bougé : la reconstruction coûte ~10 ms, la
// vérification en coûte 3.
func EnsureHead(root string, force bool) *Head {
	tip := WorldTip(root)
	if tip == "" {
		return nil
	}
	local := LocalTip(root)
	if !force {
		if prev := ReadHead(root); prev != nil && prev.Tip == tip && prev.Local == local {
			// Les DEUX dérivés doivent être là. Ne tester que le brief rendait la
			// disparition de l'index définitive : `find` et `list` répondaient « aucune
			// entrée » pour toujours, puisque la reconstruction n'avait jamais lieu.
			if fileExists(BriefFile(root)) && fileExists(IndexFile(root)) {
				return prev
			}
		}
	}
	_, h := rebuild(root, tip, local)
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
