package main

// Les globs de `affects`, qui disent quels fichiers une entrée concerne.
//
// `path.Match` de la bibliothèque standard ne connaît pas `**` : il ne traverse pas
// les séparateurs. Or `src/**/*.go` est exactement ce que les gens écrivent. D'où ce
// matcher, découpé par segments — vingt lignes qui ont l'air justes et qui ne le sont
// qu'avec leur table de tests.

import (
	"path"
	"path/filepath"
	"strings"
)

// Le chemin reçu d'un hook est absolu ; les globs d'une entrée sont relatifs à la
// racine du dépôt. On ramène le premier dans le repère du second, en barres obliques
// pour que Windows et Unix se comparent au même motif.
func relToRoot(root, p string) string {
	if p == "" {
		return ""
	}
	if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(p)
}

func MatchGlob(pattern, p string) bool {
	if pattern == "" || p == "" {
		return false
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(p, "/"))
}

func matchSegments(pat, seg []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			// `**` en fin de motif avale tout le reste ; ailleurs, il faut essayer
			// chaque point de reprise, y compris zéro segment.
			if len(pat) == 1 {
				return true
			}
			for i := 0; i <= len(seg); i++ {
				if matchSegments(pat[1:], seg[i:]) {
					return true
				}
			}
			return false
		}
		if len(seg) == 0 {
			return false
		}
		if ok, err := path.Match(pat[0], seg[0]); err != nil || !ok {
			return false
		}
		pat, seg = pat[1:], seg[1:]
	}
	return len(seg) == 0
}

// La première entrée du delta qui nomme ce fichier, s'il y en a une. Une seule :
// le refus a un budget de tokens, et c'est sa raison d'être.
func affecting(changes []Change, root, file string) *Change {
	rel := relToRoot(root, file)
	if rel == "" {
		return nil
	}
	for i := range changes {
		for _, g := range changes[i].Affects {
			if MatchGlob(g, rel) {
				return &changes[i]
			}
		}
	}
	return nil
}
