package main

// Les deux agents qui savent appeler cogitex — Claude Code et GitHub Copilot —
// parlent le même protocole de hooks à trois détails près : le nom des champs
// reçus, le nom des champs émis, et ce que chaque événement a le droit de
// renvoyer. Tout est isolé ici ; hooks.go ne décrit plus qu'une politique.
//
// Le reste du binaire ignore jusqu'à l'existence de ce fichier : rien de ce qui
// touche au corpus, à la branche ou au rendu ne dépend de l'agent appelant.

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Agent int

const (
	Claude Agent = iota
	Copilot
)

// L'agent courant, arrêté une fois pour toutes au démarrage d'un hook.
var agent = Claude

func (a Agent) String() string {
	if a == Copilot {
		return "copilot"
	}
	return "claude"
}

// ------------------------------------------------------------------ entrée

type hookInput struct {
	SessionID string
	Source    string
	ToolName  string
	CWD       string
	FilePath  string
}

// Claude envoie du snake_case (`session_id`, `tool_name`, `tool_input`), Copilot
// du camelCase (`sessionId`, `toolName`, `toolArgs`). On accepte les deux plutôt
// que de faire dépendre la lecture d'un drapeau : un payload mal étiqueté doit
// être compris quand même, sans quoi le hook part sur une session vide et le
// garde-fou s'ouvre en grand.
type rawHook struct {
	SessionSnake string         `json:"session_id"`
	SessionCamel string         `json:"sessionId"`
	Source       string         `json:"source"`
	ToolSnake    string         `json:"tool_name"`
	ToolCamel    string         `json:"toolName"`
	CWD          string         `json:"cwd"`
	ToolInput    map[string]any `json:"tool_input"`
	ToolArgs     map[string]any `json:"toolArgs"`
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// Le chemin du fichier ne sert qu'à la trace : aucune décision n'en dépend. On
// ratisse donc large sur les clés plutôt que de suivre un schéma par outil, qui
// changerait à chaque version d'un agent sans qu'on le sache.
func pickPath(maps ...map[string]any) string {
	for _, m := range maps {
		for _, k := range []string{"file_path", "filePath", "path", "filepath", "notebook_path", "target_file"} {
			if s, ok := m[k].(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func readHookInput() hookInput {
	var r rawHook
	b, err := io.ReadAll(os.Stdin)
	if err == nil && len(b) > 0 {
		_ = json.Unmarshal(b, &r)
	}
	// Détection par le payload, sauf si l'appelant a tranché avec --copilot /
	// --claude : le camelCase n'existe que chez Copilot.
	if !hasFlag("claude") && (hasFlag("copilot") || (r.SessionCamel != "" && r.SessionSnake == "")) {
		agent = Copilot
	}
	return hookInput{
		SessionID: firstNonEmpty(r.SessionSnake, r.SessionCamel),
		Source:    r.Source,
		ToolName:  firstNonEmpty(r.ToolSnake, r.ToolCamel),
		CWD:       r.CWD,
		FilePath:  pickPath(r.ToolInput, r.ToolArgs),
	}
}

// ------------------------------------------------------------------ sortie

// Sortie : EXACTEMENT une ligne JSON. Les deux agents cessent de lire stdout
// après la première ligne commençant par « { » — une seconde ligne serait
// ignorée, ou pire, couperait le bloc au mauvais endroit.
func emitJSON(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	os.Stdout.Write(append(b, '\n'))
}

// Démarrage de session. Le contexte va au modèle et coûte des tokens ; le message
// humain va au terminal et n'en coûte aucun — Copilot n'ayant pas d'équivalent de
// `systemMessage`, il ne reçoit que le premier.
func emitSessionStart(context, human string) {
	if context == "" {
		return
	}
	if agent == Copilot {
		emitJSON(map[string]any{"additionalContext": context})
		return
	}
	emitJSON(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName": "SessionStart", "additionalContext": context},
		"systemMessage": human,
	})
}

// Delta injecté au prompt. Copilot n'a aucun champ pour cela — `modifiedPrompt`
// n'est honoré que par les hooks programmatiques du SDK — d'où le faux qu'il
// renvoie : l'appelant doit alors laisser la session périmée, pour que le
// garde-fou fasse le travail au premier Edit.
func emitPromptContext(context string) bool {
	if agent == Copilot || context == "" {
		return false
	}
	emitJSON(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "UserPromptSubmit", "additionalContext": context}})
	return true
}

// Décision du garde-fou. « ask » vaut « deny » chez l'agent cloud de Copilot :
// la raison porte donc toute l'information, dans les deux cas.
func emitGuard(decision, reason, human string) {
	if agent == Copilot {
		emitJSON(map[string]any{
			"permissionDecision":       decision,
			"permissionDecisionReason": reason,
		})
		return
	}
	out := map[string]any{"hookEventName": "PreToolUse", "permissionDecision": decision}
	if reason != "" {
		out["permissionDecisionReason"] = reason
	}
	emitJSON(map[string]any{"hookSpecificOutput": out, "systemMessage": human})
}

// ------------------------------------------------------------------ installation

// Vrai quand le projet câble lui-même les hooks, dans des réglages écrits par
// `init`. On y cherche le nom du binaire ET un point d'entrée de hook : un simple
// chemin vers cogitex dans un réglage de permissions ne prouve rien.
func projectWiresHooks(root string) bool {
	for _, name := range []string{"settings.local.json", "settings.json"} {
		b, err := os.ReadFile(filepath.Join(root, ".claude", name))
		if err != nil {
			continue
		}
		s := string(b)
		if strings.Contains(s, "cogitex") && strings.Contains(s, "hook-") {
			return true
		}
	}
	return false
}

// Le plugin sert tous les projets ; un projet qui embarque son propre cogitex a
// déjà câblé les siens. Quand les deux sont là, c'est le projet qui gagne : sinon
// chaque tour paierait deux fois le même hook — brief injecté en double, et deux
// refus comptés pour une seule écriture, ce qui ouvre le garde-fou trop tôt.
func pluginStandsDown(root string) bool {
	return runsFromPlugin(root) && projectWiresHooks(root)
}

// Vrai quand le binaire ne vit pas dans le projet : il est alors fourni par un
// plugin, qui déclare déjà les hooks pour tout le monde. `init` ne doit surtout
// pas en câbler une seconde paire dans le projet — ils tireraient deux fois.
func runsFromPlugin(root string) bool {
	if os.Getenv("CLAUDE_PLUGIN_ROOT") != "" || os.Getenv("PLUGIN_ROOT") != "" {
		return true
	}
	self, err := os.Executable()
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, self)
	return err == nil && strings.HasPrefix(rel, "..")
}
