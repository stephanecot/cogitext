package main

// Traçage des interactions avec le contexte.
//
// Éteint par défaut, et il doit le rester sans coûter : quand le mode est inactif,
// tout se résume à une lecture de variable d'environnement et un stat, soit quelques
// microsecondes. On ne fait pas plus, parce que ce code est sur le chemin de CHAQUE
// édition.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

func DebugFile(root string) string { return filepath.Join(CacheDir(root), "DEBUG") }
func LogFile(root string) string   { return filepath.Join(CacheDir(root), "cogitex.log") }

const logMaxBytes = 2 << 20 // au-delà, on bascule d'un cran ; jamais plus de deux fichiers

var debugState = -1

func DebugOn(root string) bool {
	if debugState >= 0 {
		return debugState == 1
	}
	debugState = 0
	switch os.Getenv("CTX_DEBUG") {
	case "1":
		debugState = 1
	case "0":
	default:
		if _, err := os.Stat(DebugFile(root)); err == nil {
			debugState = 1
		}
	}
	return debugState == 1
}

// Une ligne NDJSON par événement, écrite en append. Court et en mode « a » : deux
// sessions et trente éditions en rafale n'entremêlent pas leurs lignes, et aucun
// verrou n'est nécessaire.
func Trace(root, event string, fields map[string]any) {
	if !DebugOn(root) {
		return
	}
	file := LogFile(root)
	if st, err := os.Stat(file); err == nil && st.Size() > logMaxBytes {
		_ = os.Rename(file, file+".1")
	}
	rec := map[string]any{"ts": time.Now().UTC().Format(time.RFC3339), "pid": os.Getpid(), "event": event}
	for k, v := range fields {
		rec[k] = v
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return // tracer ne doit jamais casser ce qu'on trace
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func TraceDone(root, event string, t0 time.Time, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["ms"] = time.Since(t0).Milliseconds()
	Trace(root, event, fields)
}

// Le rafraîchissement réseau, toujours détaché.
//
// L'invariant de tout le système : AUCUN hook n'attend le réseau. Le fichier de
// throttle est touché AVANT le lancement, jamais après — sur une rafale de trente
// éditions, la première trouve le jeton périmé, le repose et lance UN fetch ; les
// vingt-neuf suivantes ne font rien. Touché après, les trente lanceraient chacune le
// leur, soit trente poignées de main SSH simultanées.
func MaybeRefresh(root string, cfg Config) bool {
	if IsCI() {
		Trace(root, "refresh", map[string]any{"spawned": false, "why": "hors périmètre"})
		return false
	}
	// Le throttle d'abord : c'est un `stat`, et il tranche dans le cas dominant. Les
	// deux vérifications git qui suivent coûtent un sous-processus chacune, et elles
	// étaient payées à CHAQUE prompt pour, la plupart du temps, ne rien faire.
	age := time.Duration(1<<62 - 1)
	if st, err := os.Stat(StampFile(root)); err == nil {
		age = time.Since(st.ModTime())
	}
	if age <= time.Duration(cfg.RefreshThrottleMs)*time.Millisecond {
		Trace(root, "refresh", map[string]any{"spawned": false, "why": "throttle", "ageMs": age.Milliseconds()})
		return false
	}
	if IsShallow(root) || !HasBranch(root) {
		Trace(root, "refresh", map[string]any{"spawned": false, "why": "hors périmètre"})
		return false
	}
	_ = os.MkdirAll(CacheDir(root), 0o755)
	now := time.Now()
	if f, err := os.OpenFile(StampFile(root), os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		f.Close()
		_ = os.Chtimes(StampFile(root), now, now)
	}
	ok := FetchDetached(root)
	Trace(root, "refresh", map[string]any{"spawned": ok})
	return ok
}

// Ce qui a été commité ici mais n'est jamais parti. La branche n'existe pour les
// coéquipiers que publiée : un `add --no-push` sans `push`, ou un push raté hors
// ligne, laisserait sinon des règles que personne d'autre ne voit, sans que rien
// ne le signale jamais.
//
// Une fois par démarrage de session, et gratuit dans le cas courant : le tip local
// est déjà dans head.json, la référence distante se lit dans un fichier. Les deux
// sous-processus du comptage ne sont payés que si les deux diffèrent.
func MaybePublish(root string, h *Head) bool {
	if IsCI() || h == nil || h.Local == "" || RemoteRefSha(root) == h.Local {
		return false
	}
	n := Unpublished(root)
	if n == 0 {
		return false // en retard sur origin, pas en avance : c'est l'affaire du fetch
	}
	ok := PushDetached(root)
	Trace(root, "publish", map[string]any{"spawned": ok, "pending": n})
	return ok
}
