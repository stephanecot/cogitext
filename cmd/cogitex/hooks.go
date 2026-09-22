package main

// Les trois hooks, câblés à l'identique chez Claude Code et chez GitHub Copilot.
// Ce fichier ne décrit que la politique ; la traduction vers le dialecte de l'un
// ou de l'autre est dans agent.go.
//
// Ils partagent un invariant absolu : aucune panne d'infrastructure ne doit jamais
// bloquer un tour. Seule une péremption réelle le peut. Chaque chemin d'erreur
// autorise, en silence.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// La commande à suggérer dans les messages doit être une commande qui MARCHE chez
// celui qui la lit : pas `npm run`, que le projet hôte n'a peut-être pas, mais le
// chemin réel de ce binaire. On le résout relativement au projet quand il y vit.
func selfCmd(root string) string {
	// Le shim est le point d'entrée documenté, et le seul qui soit le même pour tout
	// le monde : suggérer le chemin brut du binaire donnerait à chaque coéquipier une
	// commande différente de celle du README.
	for _, shim := range []string{filepath.Join(".claude", "cogitex", "cogitex.cmd"), filepath.Join(".claude", "cogitex", "cogitex.sh")} {
		if IsWindows != strings.HasSuffix(shim, ".cmd") {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, shim)); err == nil {
			if IsWindows {
				return filepath.FromSlash(shim)
			}
			return "./" + filepath.ToSlash(shim)
		}
	}
	self, err := os.Executable()
	if err != nil {
		return "cogitex"
	}
	if rel, err := filepath.Rel(root, self); err == nil && !strings.HasPrefix(rel, "..") {
		return "./" + filepath.ToSlash(rel)
	}
	return self
}

// ------------------------------------------------------------------ SessionStart

func hookStart(root string, in hookInput) {
	t0 := time.Now()
	// Silence total tant que le projet n'a pas adopté le système, et en CI où la
	// branche n'est pas récupérée : rien ne doit casser un déploiement pour cause de
	// contexte partagé.
	if IsCI() || !HasBranch(root) || IsShallow(root) {
		return
	}
	cfg := LoadConfig(root)
	// Le brief porte les brouillons de son auteur : il dépend donc de QUI le lit, et le
	// court-circuit de EnsureHead ne compare que les tips. On force la reconstruction
	// quand l'identité a changé — ici seulement, une fois par session, sur le chemin
	// déjà coûteux. Le hook de prompt garde son court-circuit intact.
	stale := false
	if prev := ReadHead(root); prev == nil || prev.Actor != Me(root) {
		stale = true
	}
	h := EnsureHead(root, stale)
	if h == nil {
		return
	}
	prev := ReadSession(root, in.SessionID)
	gate7 := short(h.Gate)
	context, mode := "", "brief"

	switch {
	case prev != nil && prev.Gate == h.Gate && in.Source == "resume":
		// Ré-injecter 1 700 tokens à chaque reprise est la dépense évitable la plus
		// lourde du système. Si rien n'a bougé, une ligne suffit.
		mode = "entête"
		context = fmt.Sprintf("cogitex: context %s unchanged (%d decisions). The rules already in your context still apply.", gate7, h.N.Decisions)
	case prev != nil && prev.Gate != "" && prev.Gate != h.Gate:
		changes := DescribeChanges(root, prev.Gate, h.Gate, cfg, cfg.DeltaMaxEntries)
		if len(changes) > 0 && len(changes) <= cfg.DeltaMaxEntries {
			mode, context = "delta", RenderDelta(changes, cfg.DeltaMaxEntries)
		} else {
			context = ReadBrief(root)
		}
	default:
		context = ReadBrief(root)
	}

	// Ré-épinglage systématique : le rattrapage se fait ici, où il est bon marché et
	// bien situé. Une session reprise ne doit jamais manger un refus pour quelque
	// chose qu'on vient de lui montrer.
	WriteSession(root, in.SessionID, func(s *Session) {
		s.Gate, s.GateSeq, s.Acked, s.Denies, s.Muted = h.Gate, h.GateSeq, h.Gate, nil, ""
	})
	GCSessions(root)
	MaybeRefresh(root, cfg)
	MaybePublish(root, h)

	if context == "" {
		mode = "rien"
	}
	TraceDone(root, "session-start", t0, map[string]any{"mode": mode, "bytes": len(context),
		"gate": gate7, "source": in.Source, "sid": in.SessionID, "agent": agent.String()})

	// additionalContext va au modèle et coûte des tokens ; le message humain va au
	// terminal et n'en coûte aucun. C'est la seule raison de préférer le JSON au
	// stdout brut.
	emitSessionStart(context,
		fmt.Sprintf("cogitex: %d decisions · %d facts · %s", h.N.Decisions, h.N.Facts, gate7))
}

// ------------------------------------------------------------------ UserPromptSubmit

func hookPrompt(root string, in hookInput) {
	t0 := time.Now()
	if IsCI() || !HasBranch(root) || IsShallow(root) {
		return
	}
	cfg := LoadConfig(root)
	h := EnsureHead(root, false)
	if h == nil || in.SessionID == "" {
		MaybeRefresh(root, cfg)
		return
	}
	s := ReadSession(root, in.SessionID)
	if s == nil {
		WriteSession(root, in.SessionID, func(x *Session) { x.Gate, x.GateSeq = h.Gate, h.GateSeq })
		MaybeRefresh(root, cfg)
		return
	}
	if s.Gate == h.Gate {
		TraceDone(root, "prompt", t0, map[string]any{"injected": false, "why": "à jour", "gate": short(h.Gate)})
	} else {
		changes := DescribeChanges(root, s.Gate, h.Gate, cfg, cfg.DeltaMaxEntries)
		shown := false
		if len(changes) > 0 {
			shown = emitPromptContext(RenderDelta(changes, cfg.DeltaMaxEntries) +
				"\nDetail: `" + selfCmd(root) + " sync`.")
		}
		TraceDone(root, "prompt", t0, map[string]any{"injected": shown, "agent": agent.String(),
			"changes": len(changes), "from": short(s.Gate), "to": short(h.Gate)})
		// Ré-épingler sans avoir rien montré ouvrirait le garde-fou sur un delta que
		// personne n'a lu. Chez Copilot, où aucun événement de prompt n'injecte de
		// contexte, la session reste donc périmée : c'est le refus au premier Edit
		// qui portera le message, et c'est le seul endroit où il arrive à coup sûr.
		if shown || len(changes) == 0 {
			WriteSession(root, in.SessionID, func(x *Session) {
				x.Gate, x.GateSeq, x.Acked, x.Denies, x.Muted = h.Gate, h.GateSeq, h.Gate, nil, ""
			})
		}
	}
	MaybeRefresh(root, cfg)
}

// ------------------------------------------------------------------ PreToolUse

// Le filet. Chaîne de sorties anticipées, chaque branche autorisant en silence ;
// un seul chemin atteint le refus.
func hookGuard(root string, in hookInput) {
	t0 := time.Now()
	allow := func(why string) {
		TraceDone(root, "guard", t0, map[string]any{"decision": "allow", "why": why,
			"sid": in.SessionID, "file": in.FilePath})
	}
	if _, err := os.Stat(DisabledFile(root)); err == nil {
		allow("coupe-circuit")
		return
	}
	if in.SessionID == "" {
		allow("pas de session_id") // pas de clé, donc pas de preuve de péremption
		return
	}
	h := ReadHead(root)
	if h == nil {
		allow("contexte non adopté")
		return
	}

	// Ce que l'équipe a publié PEUT avoir avancé depuis le dernier prompt, si le
	// fetch de fond a atterri en cours de tour. On le détecte par une lecture de
	// fichier ; on ne dépense un sous-processus que si le monde a bougé.
	gate, gateSeq := h.Gate, h.GateSeq
	if ref := RemoteRefSha(root); ref != "" && ref != h.Tip {
		if g := GateOf(root, ref); g != "" {
			gate, gateSeq = g, GateSeqOf(root, ref)
		}
	}

	s := ReadSession(root, in.SessionID)
	if s == nil {
		inherited := InheritGate(root, gate)
		WriteSession(root, in.SessionID, func(x *Session) { x.Gate, x.GateSeq = inherited, gateSeq })
		allow("session héritée de " + short(inherited))
		return
	}
	if s.Gate == gate {
		allow("à jour") // ← chemin courant
		return
	}
	if s.Acked == gate {
		allow("acquitté")
		return
	}
	if s.Muted == gate {
		allow("mis en sourdine")
		return
	}

	cfg := LoadConfig(root)
	if !ContentChanged(root, s.Gate, gate, cfg.FactsBlock) {
		WriteSession(root, in.SessionID, func(x *Session) { x.Gate, x.GateSeq = gate, gateSeq })
		allow("gate déplacé sans changement de contenu")
		return
	}

	denies := s.Denies[gate]
	n := gateSeq - s.GateSeq
	if n < 1 {
		n = 1
	}
	gate7 := short(gate)

	if denies >= cfg.MaxDeniesPerGate {
		// Coupe-circuit : une session ne doit jamais pouvoir se verrouiller
		// définitivement. Au troisième refus, c'est l'humain qui tranche.
		WriteSession(root, in.SessionID, func(x *Session) { x.Muted = gate })
		TraceDone(root, "guard", t0, map[string]any{"decision": "ask", "n": n, "gate": gate7,
			"agent": agent.String()})
		msg := fmt.Sprintf("cogitex: %d unread shared rule(s) (%s). Denied twice already — your call.", n, gate7)
		emitGuard("ask", msg, msg)
		return
	}

	WriteSession(root, in.SessionID, func(x *Session) {
		if x.Denies == nil {
			x.Denies = map[string]int{}
		}
		x.Denies[gate] = denies + 1
		x.LastDeny = time.Now().UnixMilli()
	})
	TraceDone(root, "guard", t0, map[string]any{"decision": "deny", "n": n, "agent": agent.String(),
		"gate": gate7, "from": short(s.Gate), "file": in.FilePath})

	// Le refus annonce un COMPTEUR, jamais les entrées — sinon un import de quarante
	// décisions injecterait des milliers de tokens dans chaque session périmée, par
	// un chemin que personne ne surveille.
	// Le refus annonce un COMPTEUR, jamais les entrées. Mais si l'une d'elles nomme
	// justement le fichier que le modèle s'apprête à écrire, la citer vaut mieux que
	// n'importe quel compteur — et une seule, le plafond de tokens du refus étant
	// précisément sa raison d'être.
	//
	// Uniquement depuis le delta DÉJÀ en cache : le garde ne doit jamais attendre git.
	// Sous Copilot, où le hook de prompt calcule le delta sans ré-épingler la session,
	// il y est systématiquement — c'est-à-dire exactement là où ce refus porte tout le
	// message.
	named := ""
	if hit := affecting(CachedChanges(root, s.Gate, gate, cfg, cfg.DeltaMaxEntries), root, in.FilePath); hit != nil {
		named = fmt.Sprintf("\n\nOne of them names this very file: %s — %s", hit.ID, hit.Title)
	}

	self := selfCmd(root)
	reason := fmt.Sprintf(
		"cogitex: this session is pinned to shared context %s, but %d decision(s)/fact(s) have landed "+
			"since (%s). Writing code against rules you have not read is exactly what this guard prevents.\n\n"+
			"Run this, read the delta, then retry your edit:\n\n    %s sync\n\n"+
			"It prints only what changed (a few lines), re-pins this session and unblocks Edit/Write. "+
			"Notes and journal entries never trigger this. Offline or cogitex broken: `%s sync --offline` "+
			"re-pins locally and unblocks you right away.%s", short(s.Gate), n, gate7, self, self, named)

	emitGuard("deny", reason,
		fmt.Sprintf("cogitex: %d new shared rule(s) — writes paused until `%s sync`.", n, self))
}
