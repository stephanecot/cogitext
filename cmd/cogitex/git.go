package main

// Exécution de git, durcie.
//
// Ce module existe pour une seule raison : un hook n'a pas de terminal. Toute
// commande git capable d'ouvrir une invite — signature GPG, passphrase SSH,
// confirmation de clé d'hôte, hook pre-commit — ne demande pas, elle PEND, sans
// message et sans cause diagnosticable. Chaque option ci-dessous ferme une de ces
// portes. Aucune n'est décorative.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	Branch    = "context"
	Ref       = "refs/heads/context"
	RemoteRef = "refs/remotes/origin/context"
	FetchSpec = "+refs/heads/context:refs/remotes/origin/context"
)

var IsWindows = runtime.GOOS == "windows"

func devNull() string {
	if IsWindows {
		return `\\.\NUL`
	}
	return "/dev/null"
}

func hardened() []string {
	return []string{
		"-c", "commit.gpgsign=false", // sinon pinentry ouvre une fenêtre DANS le hook
		"-c", "core.hooksPath=" + devNull(), // sinon un pre-commit tourne sur une branche sans dépendances
		"-c", "gc.auto=0", // sinon un gc de 5 à 20 s se déclenche un jour au pire moment
		"-c", "maintenance.auto=false",
		"-c", "core.fsmonitor=false",
		"-c", "advice.detachedHead=false",
		"-c", "advice.statusHints=false",
	}
}

// Environnement des invocations RÉSEAU.
//
// Le multiplexage SSH est le seul levier de performance qui compte : le coût d'un
// fetch, c'est 1,15 s de poignée de main pour 35 ms de trajet réseau ; réutiliser la
// connexion fait tomber l'appel de 1,5 s à 0,37 s. Mais il n'existe pas dans
// l'OpenSSH de Windows, où il ferait échouer la connexion — d'où la séparation.
func netEnv() []string {
	env := append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"SSH_ASKPASS_REQUIRE=never",
	)
	ssh := "ssh -o BatchMode=yes -o ConnectTimeout=5 -o StrictHostKeyChecking=accept-new"
	if IsWindows {
		// Sur Windows l'invite ne vient pas d'SSH mais de Git Credential Manager, et
		// /usr/bin/true n'existe pas : y pointer GIT_ASKPASS produirait une erreur
		// au lieu de l'empêcher.
		env = append(env, "GCM_INTERACTIVE=Never")
	} else {
		env = append(env, "GIT_ASKPASS=/usr/bin/true")
		ssh += " -o ControlMaster=auto -o ControlPath=~/.ssh/cm-%C -o ControlPersist=60"
	}
	return append(env, "GIT_SSH_COMMAND="+ssh)
}

type Result struct {
	OK   bool
	Code int
	Out  string
	Err  string
}

type gitOpts struct {
	Dir     string
	Timeout time.Duration
	Net     bool
	Env     []string
	Stdin   string
}

// Le délai est imposé par le contexte, avec une tuerie franche du processus : macOS
// n'a pas de binaire `timeout`, et un SSH bloqué sur une poignée de main ignore un
// signal doux.
func runGit(args []string, o gitOpts) Result {
	if o.Timeout == 0 {
		o.Timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), o.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append(hardened(), args...)...)
	cmd.Dir = o.Dir
	if o.Net {
		cmd.Env = netEnv()
	} else if o.Env != nil {
		cmd.Env = append(os.Environ(), o.Env...)
	}
	if o.Stdin != "" {
		cmd.Stdin = strings.NewReader(o.Stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	// Le maître SSH que `ControlPersist` laisse en arrière-plan hérite des tuyaux de
	// git. Sans cette borne, Wait attend leur fermeture — donc la mort du maître, une
	// minute plus tard — bien après la fin de git, et le délai ci-dessus n'y peut
	// rien. Une fois git sorti, tout ce qu'il avait à dire est déjà dans les tampons.
	cmd.WaitDelay = 500 * time.Millisecond
	err := cmd.Run()
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil // git a réussi ; seul un orphelin tenait encore les tuyaux
	}
	r := Result{Out: strings.TrimSpace(out.String()), Err: strings.TrimSpace(errb.String())}
	if err == nil {
		r.OK, r.Code = true, 0
		return r
	}
	r.Code = -1
	if ee, ok := err.(*exec.ExitError); ok {
		r.Code = ee.ExitCode()
	}
	if r.Err == "" {
		r.Err = err.Error()
	}
	return r
}

func git(dir string, args ...string) Result { return runGit(args, gitOpts{Dir: dir}) }

// Le fetch est notre unique primitive réseau. `ls-remote` coûte exactement le même
// prix — la poignée de main, pas la charge utile — et rapporte strictement moins :
// enchaîner les deux, c'est payer 3 s pour ce qu'un fetch donne en 1,5 s.
func FetchContext(root string, timeout time.Duration) Result {
	return runGit([]string{"fetch", "--no-tags", "--no-write-fetch-head",
		"--no-recurse-submodules", "-q", "origin", FetchSpec},
		gitOpts{Dir: root, Timeout: timeout, Net: true})
}

// Le même fetch, détaché : le hook rend la main immédiatement et ne lit jamais le
// résultat. C'est ce qui permet de contrôler à chaque prompt sans payer le réseau.
func FetchDetached(root string) bool {
	cmd := exec.Command("git", append(hardened(), "fetch", "--no-tags",
		"--no-write-fetch-head", "-q", "origin", FetchSpec)...)
	cmd.Dir = root
	cmd.Env = netEnv()
	cmd.Stdout, cmd.Stderr, cmd.Stdin = nil, nil, nil
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return false
	}
	_ = cmd.Process.Release()
	return true
}

func RevParse(root, rev string) string {
	r := runGit([]string{"rev-parse", "--verify", "-q", rev}, gitOpts{Dir: root, Timeout: 3 * time.Second})
	if r.OK && r.Out != "" {
		return r.Out
	}
	return ""
}

func HasBranch(root string) bool { return RevParse(root, Ref) != "" }

// Un clone superficiel ne contient pas les anciens objets : le calcul de delta
// échouerait, et pousser depuis un tel clone est refusé. On se met en retrait.
func IsShallow(root string) bool {
	return git(root, "rev-parse", "--is-shallow-repository").Out == "true"
}

func IsCI() bool { return os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" }

func CommonDir(root string) string {
	r := git(root, "rev-parse", "--git-common-dir")
	if !r.OK {
		return ""
	}
	if filepath.IsAbs(r.Out) {
		return r.Out
	}
	return filepath.Join(root, r.Out)
}

func CogitexDir(root string) string { return filepath.Join(root, ".cogitex") }

func WorktreeReady(root string) bool {
	_, err := os.Stat(filepath.Join(CogitexDir(root), ".git"))
	return err == nil
}

// Le verrou d'écriture. La CORRECTION n'en dépend pas — l'index.lock de git fait
// déjà qu'une course perdue est un échec propre, jamais une corruption. Il n'existe
// que pour l'ergonomie : sans lui, deux sessions de la même machine s'échangent des
// échecs incompréhensibles.
func WithLock(root string, fn func() error) error {
	dir := CommonDir(root)
	if dir == "" {
		return fn()
	}
	lock := filepath.Join(dir, "cogitex.lock")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := os.Mkdir(lock, 0o755); err == nil {
			break
		}
		if st, err := os.Stat(lock); err == nil && time.Since(st.ModTime()) > 2*time.Minute {
			_ = os.RemoveAll(lock)
			continue
		}
		if time.Now().After(deadline) {
			return errStr("verrou d'écriture occupé — une autre session écrit, réessaie")
		}
		time.Sleep(50 * time.Millisecond)
	}
	defer os.RemoveAll(lock)
	return fn()
}

type errStr string

func (e errStr) Error() string { return string(e) }
