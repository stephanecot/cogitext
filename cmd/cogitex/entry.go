package main

// Le format des entrées : identifiants, schémas, et un couple emit/parse YAML
// volontairement restreint.
//
// Pourquoi un YAML maison plutôt qu'une dépendance : ce binaire s'exécute à chaque
// édition, il doit rester statique et sans dépendance, et on n'accepte qu'un
// sous-ensemble du format — accepté à l'écriture comme à la lecture, pour que
// l'aller-retour soit fidèle.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const DecisionMaxChars = 110

var (
	Types    = []string{"architecture", "technical", "functional", "convention"}
	Statuses = []string{"active", "proposed", "superseded", "deprecated"}
	Scopes   = []string{"global", "domain", "local"}
	Active   = []string{"active", "proposed"}
)

var scalarOrder = []string{"id", "title", "type", "status", "scope", "domain", "date",
	"decision", "context", "rationale", "supersedes", "author", "body", "ttl_days", "pinned"}
var listOrder = []string{"authors", "consequences", "tags", "affects", "refs"}

// Entry est volontairement une map : le format doit tolérer qu'une entrée écrite par
// une version plus récente porte un champ qu'on ne connaît pas, sans le perdre.
type Entry map[string]any

func (e Entry) Str(k string) string {
	switch v := e[k].(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%g", v)
	case bool:
		return fmt.Sprintf("%t", v)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func (e Entry) List(k string) []string {
	out := []string{}
	switch v := e[k].(type) {
	case []any:
		for _, it := range v {
			out = append(out, fmt.Sprint(it))
		}
	case []string:
		out = append(out, v...)
	}
	return out
}

// ---------------------------------------------------------------- identifiants

var (
	nonSlug     = regexp.MustCompile(`[^a-z0-9]+`)
	trimDash    = regexp.MustCompile(`^-+|-+$`)
	accentPairs = strings.NewReplacer(
		"à", "a", "â", "a", "ä", "a", "á", "a", "ã", "a", "å", "a",
		"ç", "c", "é", "e", "è", "e", "ê", "e", "ë", "e",
		"î", "i", "ï", "i", "í", "i", "ì", "i",
		"ô", "o", "ö", "o", "ó", "o", "õ", "o", "ò", "o",
		"ù", "u", "û", "u", "ü", "u", "ú", "u", "ÿ", "y", "ñ", "n", "œ", "oe", "æ", "ae")
)

// Slug ASCII strict et minuscule. Ce n'est pas cosmétique : sur un système de
// fichiers insensible à la casse (APFS, NTFS), « -Auth » et « -auth » sont un seul
// fichier, et deux sur Linux — une collision invisible jusqu'à la CI. Et un mot
// accentué se stocke en NFD sur macOS contre NFC ailleurs : deux octets différents
// pour le même mot.
func Slugify(in string) string {
	s := accentPairs.Replace(strings.ToLower(in))
	s = nonSlug.ReplaceAllString(s, "-")
	s = trimDash.ReplaceAllString(s, "")
	if len(s) > 60 {
		s = strings.TrimRight(s[:60], "-")
	}
	if s == "" {
		return "sans-titre"
	}
	return s
}

// Date du jour en UTC. En heure locale, deux utilisateurs à cheval sur minuit
// classent le même événement sous deux jours différents.
func Today() string { return time.Now().UTC().Format("2006-01-02") }

const crock = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ULID : trié lexicographiquement par le temps. C'est ce qui rend la fusion « union »
// du journal correcte — elle peut produire n'importe quel entrelacement et n'importe
// quelle duplication, le tri et la déduplication par identifiant redonnent la même vue.
func ULID(ms int64, rnd func() int) string {
	var b [26]byte
	t := ms
	for i := 9; i >= 0; i-- {
		b[i] = crock[t%32]
		t /= 32
	}
	for i := 10; i < 26; i++ {
		b[i] = crock[rnd()%32]
	}
	return string(b[:])
}

// L'identifiant EST le chemin — jamais un compteur. Deux utilisateurs qui
// enregistrent au même instant écrivent deux fichiers distincts, donc le merge est
// trivial ; un compteur séquentiel collisionne, mécaniquement.
func PathFor(kind string, e Entry) (string, error) {
	date := e.Str("date")
	if date == "" {
		date = Today()
	}
	slug := e.Str("slug")
	if slug == "" {
		slug = e.Str("title")
	}
	slug = Slugify(slug)
	switch kind {
	case "decision":
		dom := e.Str("domain")
		if dom == "" {
			dom = "general"
		}
		return fmt.Sprintf("decisions/%s/%s-%s.yaml", Slugify(dom), date, slug), nil
	case "fact":
		return fmt.Sprintf("facts/%s-%s.yaml", date, slug), nil
	case "note":
		return fmt.Sprintf("notes/%s-%s.md", date, slug), nil
	}
	return "", fmt.Errorf("nature non gérée : %s", kind)
}

func JournalPathFor(actor, date string) string {
	return fmt.Sprintf("journal/%s/%s.ndjson", date[:7], Slugify(actor))
}

var extRe = regexp.MustCompile(`\.(ya?ml|md)$`)

func IDFromPath(p string) string   { return extRe.ReplaceAllString(p, "") }
func KindFromPath(p string) string { return strings.TrimSuffix(strings.SplitN(p, "/", 2)[0], "s") }

// ---------------------------------------------------------------- YAML restreint

var bare = regexp.MustCompile(`^[A-Za-z0-9_./:#@+-]+$`)
var reserved = regexp.MustCompile(`(?i)^(true|false|null|~)$`)

func encodeScalar(v string) string {
	if v != "" && bare.MatchString(v) && !reserved.MatchString(v) {
		return v
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func Emit(e Entry) string {
	var sb strings.Builder
	emitList := func(k string, items []string) {
		sb.WriteString(k + ":\n")
		for _, it := range items {
			sb.WriteString("  - " + encodeScalar(it) + "\n")
		}
	}
	for _, k := range scalarOrder {
		v := e.Str(k)
		if v == "" {
			continue
		}
		sb.WriteString(k + ": " + encodeScalar(v) + "\n")
	}
	for _, k := range listOrder {
		if items := e.List(k); len(items) > 0 {
			emitList(k, items)
		}
	}
	// Les champs qu'on ne connaît pas partent en queue, plutôt qu'à la poubelle.
	// C'est ce qui rend vraie la promesse de la map juste au-dessus : une entrée
	// écrite par une version plus récente doit survivre à une réécriture par une
	// version plus ancienne. Sans cela, promouvoir un brouillon ou basculer un
	// statut amputerait l'entrée en silence.
	//
	// Le tri rend l'octet identique d'une machine à l'autre — sans lui, deux postes
	// réécrivant la même entrée produiraient deux diffs sans différence de contenu.
	for _, k := range unknownKeys(e) {
		if items := e.List(k); len(items) > 0 {
			emitList(k, items)
			continue
		}
		if v := e.Str(k); v != "" {
			sb.WriteString(k + ": " + encodeScalar(v) + "\n")
		}
	}
	return sb.String()
}

// Remplace la valeur d'un champ scalaire EN PLACE, sans toucher au reste du fichier.
//
// C'est la seule façon admise de modifier une entrée existante. Un Parse + Emit
// détruirait le corps markdown d'une note — que Parse ne sait pas relire — et
// réordonnerait des octets sans que rien n'ait changé.
//
// `FindIndex` + découpe plutôt que `ReplaceAll` : ce dernier interprète `$1` dans le
// remplacement, et une valeur contenant un `$` serait silencieusement mutilée.
func ReplaceScalar(src []byte, key, value string) ([]byte, bool) {
	limit := len(src)
	// Une note est un front-matter suivi de markdown, et sa prose peut très bien
	// contenir une ligne qui ressemble à un champ. On ne cherche que dans l'en-tête.
	if end := frontMatterEnd(src); end > 0 {
		limit = end
	}
	loc := regexp.MustCompile(`(?m)^`+regexp.QuoteMeta(key)+`:[ \t]*.*$`).FindIndex(src[:limit])
	if loc == nil {
		return src, false
	}
	line := []byte(key + ": " + encodeScalar(value))
	out := make([]byte, 0, len(src)+len(line))
	out = append(out, src[:loc[0]]...)
	out = append(out, line...)
	return append(out, src[loc[1]:]...), true
}

// Le corps d'une note vit APRÈS le front-matter, et `Parse` ne sait pas le relire :
// il ne retient que les lignes `clé: valeur`. Sans cette extraction, la prose d'une
// note n'était indexée nulle part — or c'est précisément là qu'on écrit l'impasse et
// sa raison, la connaissance la plus chère à redécouvrir, et le brief promettait au
// modèle que `find` couvrait 100 % du corpus.
func MarkdownBody(src []byte) string {
	end := frontMatterEnd(src)
	if end == 0 {
		return strings.TrimSpace(string(src))
	}
	rest := src[end+1:] // commence sur le `---` fermant
	i := bytes.IndexByte(rest, '\n')
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(string(rest[i+1:]))
}

func frontMatterEnd(src []byte) int {
	if !bytes.HasPrefix(src, []byte("---\n")) && !bytes.HasPrefix(src, []byte("---\r\n")) {
		return 0
	}
	if i := bytes.Index(src[4:], []byte("\n---")); i >= 0 {
		return 4 + i
	}
	return 0
}

// `slug` n'est pas un champ de l'entrée : il n'existe qu'en entrée de `PathFor`,
// pour choisir le nom du fichier. Le réémettre le figerait dans le corpus.
func unknownKeys(e Entry) []string {
	var out []string
	for k := range e {
		if strings.HasPrefix(k, "__") || k == "slug" ||
			contains(scalarOrder, k) || contains(listOrder, k) {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func decodeScalar(raw string) any {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if s[0] == '"' {
		var out string
		if json.Unmarshal([]byte(s), &out) == nil {
			return out
		}
	}
	switch s {
	case "true":
		return true
	case "false":
		return false
	}
	return s
}

var (
	itemRe = regexp.MustCompile(`^\s+-\s+(.*)$`)
	kvRe   = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*):\s*(.*)$`)
)

func Parse(text string) Entry {
	e := Entry{}
	listKey := ""
	blockKey := ""
	var blockLines []string
	flushBlock := func() {
		if blockKey != "" {
			e[blockKey] = strings.TrimSpace(strings.Join(blockLines, "\n"))
			blockKey, blockLines = "", nil
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if blockKey != "" {
			if strings.TrimSpace(line) == "" || (len(line) > 0 && (line[0] == ' ' || line[0] == '\t')) {
				blockLines = append(blockLines, strings.TrimPrefix(line, "  "))
				continue
			}
			flushBlock()
		}
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if m := itemRe.FindStringSubmatch(line); m != nil && listKey != "" {
			e[listKey] = append(e.List(listKey), fmt.Sprint(decodeScalar(m[1])))
			continue
		}
		m := kvRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, rest := m[1], m[2]
		listKey = ""
		// Un « clé: > » ou « clé: | » écrit à la main ne doit pas être stocké tel
		// quel : la valeur littérale « > » corrompt silencieusement l'index.
		if rest == ">" || rest == "|" || rest == ">-" || rest == "|-" {
			blockKey, blockLines = key, nil
			continue
		}
		if rest == "" {
			listKey = key
			e[key] = []string{}
			continue
		}
		e[key] = decodeScalar(rest)
	}
	flushBlock()
	return e
}

// ---------------------------------------------------------------- validation

// Le corpus partagé est en ANGLAIS, sans exception. Ce n'est pas une préférence de
// style : c'est un index cherché par mots, lu par des sessions dont rien ne garantit
// la langue, et un corpus à moitié traduit rend la recherche inutilisable — la moitié
// des entrées ne répond plus aux mots-clés de l'autre.
//
// La détection porte sur les MOTS-OUTILS français, pas sur les accents : un nom propre
// accentué dans une entrée anglaise ne doit pas la refuser. Deux occurrences distinctes
// sont exigées, ce qui écarte « de » dans un nom.
var frWords = regexp.MustCompile(`(?i)\b(le|la|les|une|des|du|et|ou|est|sont|dans|pour|avec|sans|pas|ne|que|qui|sur|aux|cette|nous|vous|leur|toujours|jamais|doit|doivent|être|avoir|faire|chaque|ainsi|donc|mais|alors)\b`)

func LooksFrench(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range frWords.FindAllString(strings.ToLower(text), -1) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	if len(out) >= 2 {
		if len(out) > 4 {
			out = out[:4]
		}
		return out
	}
	return nil
}

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func Validate(kind string, e Entry) []string {
	var errs []string
	need := func(k string) {
		if strings.TrimSpace(e.Str(k)) == "" {
			errs = append(errs, "champ requis manquant : "+k)
		}
	}
	if !dateRe.MatchString(e.Str("date")) {
		errs = append(errs, "date attendue au format AAAA-MM-JJ")
	}
	switch kind {
	case "decision":
		for _, k := range []string{"title", "type", "status", "domain", "decision"} {
			need(k)
		}
		if v := e.Str("type"); v != "" && !contains(Types, v) {
			errs = append(errs, "type inconnu : "+v)
		}
		if v := e.Str("status"); v != "" && !contains(Statuses, v) {
			errs = append(errs, "status inconnu : "+v)
		}
		if v := e.Str("scope"); v != "" && !contains(Scopes, v) {
			errs = append(errs, "scope inconnu : "+v)
		}
		if d := e.Str("decision"); len([]rune(d)) > DecisionMaxChars {
			errs = append(errs, fmt.Sprintf("« decision » fait %d caractères, le plafond est %d — c'est la règle en une phrase, le détail va dans « rationale »", len([]rune(d)), DecisionMaxChars))
		}
	case "fact":
		need("title")
		need("author")
	case "note":
		need("title")
	}

	prose := strings.Join(append([]string{e.Str("title"), e.Str("decision"), e.Str("rationale"),
		e.Str("context"), e.Str("body")}, e.List("consequences")...), " ")
	if fr := LooksFrench(prose); fr != nil {
		errs = append(errs, "le contexte partagé est en anglais — mots français détectés ("+
			strings.Join(fr, ", ")+"). Traduis title, decision, rationale et body.")
	}
	return errs
}

// `scope` décide QUAND une entrée est chargée, jamais SI elle est atteignable : la
// recherche et le catalogue couvrent 100 % du corpus en toutes circonstances.
func ScopeOf(e Entry) string {
	if s := e.Str("scope"); s != "" {
		return s
	}
	if t := e.Str("type"); t == "architecture" || t == "convention" {
		return "global"
	}
	return "domain"
}

func IsActive(e Entry) bool {
	s := e.Str("status")
	if s == "" {
		return true
	}
	return contains(Active, s)
}

// Un fait expire tout seul. C'est ce qui borne la croissance du bloc injecté sans
// qu'aucun humain n'ait à faire le ménage — donc sans que personne n'oublie.
func FactExpired(e Entry, now time.Time) bool {
	ttl := 30
	if v, ok := e["ttl_days"].(float64); ok && v > 0 {
		ttl = int(v)
	} else if s := e.Str("ttl_days"); s != "" {
		fmt.Sscanf(s, "%d", &ttl)
	}
	t, err := time.Parse("2006-01-02", e.Str("date"))
	if err != nil {
		return true
	}
	return now.After(t.AddDate(0, 0, ttl))
}
