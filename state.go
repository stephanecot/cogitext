package main

// L'état : ce que l'équipe a publié (head), et ce que cette session a intégré
// (session). Tout le protocole de péremption tient dans la comparaison des deux.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var rulePaths = []string{"decisions", "facts"}

func CacheDir(root string) string     { return filepath.Join(root, ".claude", "cache", "cogit") }
func HeadFile(root string) string     { return filepath.Join(CacheDir(root), "head.json") }
func BriefFile(root string) string    { return filepath.Join(CacheDir(root), "brief.txt") }
func IndexFile(root string) string    { return filepath.Join(CacheDir(root), "index.ndjson") }
func StampFile(root string) string    { return filepath.Join(CacheDir(root), "fetch.stamp") }
func DisabledFile(root string) string { return filepath.Join(CacheDir(root), "DISABLED") }
func SessionsDir(root string) string  { return filepath.Join(CacheDir(root), "sessions") }

var sidSafe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// Un session_id vient de stdin : il ne doit jamais pouvoir sortir du répertoire.
func SessionFile(root, sid string) string {
	s := sidSafe.ReplaceAllString(sid, "")
	if len(s) > 64 {
		s = s[:64]
	}
	return filepath.Join(SessionsDir(root), s+".json")
}

type Counts struct {
	Decisions int `json:"decisions"`
	Facts     int `json:"facts"`
	Notes     int `json:"notes"`
	Journal   int `json:"journal"`
}

type Head struct {
	Tip     string `json:"tip"`
	Local   string `json:"local"`
	Gate    string `json:"gate"`
	GateSeq int    `json:"gateSeq"`
	BuiltAt int64  `json:"builtAt"`
	N       Counts `json:"n"`
}

type Session struct {
	Gate     string         `json:"gate"`
	GateSeq  int            `json:"gateSeq"`
	Acked    string         `json:"acked,omitempty"`
	Muted    string         `json:"muted,omitempty"`
	Denies   map[string]int `json:"denies,omitempty"`
	LastSeen int64          `json:"lastSeen"`
	LastDeny int64          `json:"lastDenyAt,omitempty"`
}

// Lecture tolérante : toute erreur vaut « inconnu », jamais une exception. C'est le
// socle de la règle « échouer ouvert » — un cache corrompu autorise l'écriture.
func readJSON(path string, v any) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, v) == nil
}

// Écriture atomique : rename() est atomique sur APFS comme sur NTFS, donc une lecture
// concurrente voit l'ancien ou le nouveau contenu, jamais un fichier tronqué. C'est
// ce qui dispense de tout fichier de verrou sur le cache.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writeJSONAtomic(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeAtomic(path, b)
}

// tip bouge à chaque entrée, y compris une note. gate ne bouge que si une DÉCISION ou
// un FAIT a changé — c'est lui, et lui seul, la clé de péremption.
//
// Le fait que gate soit DÉRIVÉ de l'historique, et non déclaré par celui qui écrit,
// est ce qui le rend insensible aux merges, aux éditions à la main et aux oublis.
func GateOf(root, tip string) string {
	if tip == "" {
		return ""
	}
	r := runGit(append([]string{"rev-list", "-1", tip, "--"}, rulePaths...),
		gitOpts{Dir: root, Timeout: 3 * time.Second})
	if r.OK && r.Out != "" {
		return r.Out
	}
	return "none"
}

func GateSeqOf(root, tip string) int {
	if tip == "" {
		return 0
	}
	r := runGit(append([]string{"rev-list", "--count", tip, "--"}, rulePaths...),
		gitOpts{Dir: root, Timeout: 3 * time.Second})
	n := 0
	if r.OK {
		strings.NewReplacer().Replace(r.Out)
		for _, c := range r.Out {
			if c < '0' || c > '9' {
				return n
			}
			n = n*10 + int(c-'0')
		}
	}
	return n
}

func WorldTip(root string) string {
	if t := RevParse(root, RemoteRef); t != "" {
		return t
	}
	return RevParse(root, Ref)
}
func LocalTip(root string) string { return RevParse(root, Ref) }

func ReadHead(root string) *Head {
	var h Head
	if !readJSON(HeadFile(root), &h) || h.Gate == "" {
		return nil
	}
	return &h
}

// Le SHA d'origin/context, lu DIRECTEMENT dans le fichier de référence.
//
// C'est ce qui rend le garde à la fois utile et gratuit. Le fetch de fond met à jour
// cette référence sans que rien ne reconstruise head.json : un garde qui ne lirait
// que le cache ne verrait jamais ce qui arrive en cours de tour, et serait purement
// décoratif. Le lire ici coûte une lecture de 41 octets — aucun sous-processus.
var shaRe = regexp.MustCompile(`^[0-9a-f]{40}$`)
var packedRe = regexp.MustCompile(`(?m)^([0-9a-f]{40}) refs/remotes/origin/context$`)

func RemoteRefSha(root string) string {
	gitdir := filepath.Join(root, ".git")
	if st, err := os.Stat(gitdir); err != nil || !st.IsDir() {
		return ""
	}
	if b, err := os.ReadFile(filepath.Join(gitdir, "refs", "remotes", "origin", "context")); err == nil {
		s := strings.TrimSpace(string(b))
		if shaRe.MatchString(s) {
			return s
		}
	}
	if b, err := os.ReadFile(filepath.Join(gitdir, "packed-refs")); err == nil {
		if m := packedRe.FindSubmatch(b); m != nil {
			return string(m[1])
		}
	}
	return ""
}

// Deux gates différents par leur SHA ne le sont pas forcément par leur contenu : un
// merge peut déplacer le gate sans rien changer. On ne bloque jamais sur la seule
// différence de SHA — on confirme par un diff de contenu, qui coûte 10 ms et
// n'arrive que quand quelque chose a réellement bougé.
func ContentChanged(root, from, to string, factsBlock bool) bool {
	if from == "" || to == "" || from == to {
		return false
	}
	if from == "none" || to == "none" {
		return true
	}
	paths := rulePaths
	if !factsBlock {
		paths = []string{"decisions"}
	}
	r := runGit(append([]string{"diff", "--quiet", from, to, "--"}, paths...),
		gitOpts{Dir: root, Timeout: 3 * time.Second})
	if r.Code == 128 {
		return true // un objet manque : on force la resynchronisation
	}
	return r.Code == 1
}

func ReadSession(root, sid string) *Session {
	if sid == "" {
		return nil
	}
	var s Session
	if !readJSON(SessionFile(root, sid), &s) || s.Gate == "" {
		return nil
	}
	return &s
}

func WriteSession(root, sid string, mutate func(*Session)) {
	if sid == "" {
		return
	}
	s := ReadSession(root, sid)
	if s == nil {
		s = &Session{}
	}
	mutate(s)
	s.LastSeen = time.Now().UnixMilli()
	_ = os.MkdirAll(SessionsDir(root), 0o755)
	_ = writeJSONAtomic(SessionFile(root, sid), s)
}

// Un sous-agent reçoit un identifiant de session neuf. Sans cette règle il serait
// épinglé au gate courant et passerait donc à travers le garde — un trou béant, juste
// là où le travail est délégué. Il hérite du gate de la session vivante la plus récente.
func InheritGate(root, fallback string) string {
	entries, err := os.ReadDir(SessionsDir(root))
	if err != nil {
		return fallback
	}
	best := ""
	var bestSeen int64
	cutoff := time.Now().Add(-2 * time.Hour).UnixMilli()
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var s Session
		if !readJSON(filepath.Join(SessionsDir(root), e.Name()), &s) || s.Gate == "" {
			continue
		}
		if s.LastSeen < cutoff {
			continue
		}
		if s.LastSeen > bestSeen {
			best, bestSeen = s.Gate, s.LastSeen
		}
	}
	if best != "" {
		return best
	}
	return fallback
}

func GCSessions(root string) {
	entries, err := os.ReadDir(SessionsDir(root))
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	for _, e := range entries {
		p := filepath.Join(SessionsDir(root), e.Name())
		if st, err := os.Stat(p); err == nil && st.ModTime().Before(cutoff) {
			_ = os.Remove(p)
		}
	}
}
