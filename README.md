# cogit — contexte partagé entre sessions

`cogit` porte les **décisions, faits, notes et journal** d'une équipe sur une
branche git orpheline, `refs/heads/context`, et câble trois hooks Claude Code qui
les injectent au démarrage d'une session, les rafraîchissent, et bloquent une
écriture faite sous des règles que la session n'a pas lues.

Le contexte voyage donc avec le dépôt : pas de service, pas de base, pas de
compte. Ce que sait une session, la suivante le sait aussi — y compris chez
quelqu'un d'autre.

## Pourquoi un binaire

Ce dépôt doit pouvoir s'installer dans **n'importe quel projet** — Java, Python,
Go, .NET, TypeScript — sans lui imposer de runtime. `cogit` est donc un binaire
statique, un par plateforme, sans aucune dépendance partagée. La seule chose
qu'il exige est `git`, ce qui est cohérent : tout le système est bâti dessus.

C'est la raison d'être de ce dépôt séparé : le code vivait dans un projet
applicatif, qui n'avait aucune raison de porter l'outillage de tous les autres.

## Installer dans un projet

```sh
./install.sh /chemin/vers/le/projet          # macOS, Linux, Git Bash
.\install.ps1 C:\chemin\vers\le\projet       # Windows
```

L'installateur copie `dist/` dans le projet et ajoute à son `.gitignore` et à son
`.gitattributes` les lignes sans lesquelles cogit fonctionne mal — sans le second,
les binaires sont corrompus par la conversion de fins de ligne sur un clone
Windows. Rien n'est écrasé sans le dire : un fichier déjà présent et différent est
signalé et conservé, sauf avec `--force` / `-Force`.

Puis, dans le projet :

```sh
.claude/cogit/cogit.sh init        # macOS, Linux, Git Bash
.claude\cogit\cogit.cmd init       # Windows
```

`init` crée ou rejoint la branche, monte le worktree `.cogit`, et écrit
`.claude/settings.local.json` — **local et gitignoré**, parce qu'un
`settings.json` ne peut pas désigner un chemin de binaire différent selon l'OS.
C'est la contrepartie assumée des binaires commités : une commande après le
clone, mais aucune installation et aucune toolchain.

Chaque personne qui clone le projet lance `init` une fois.

## Commandes

```
init                    créer ou rejoindre la branche, monter .cogit, câbler les hooks
add decision|fact|note  enregistrer une entrée (JSON sur stdin) [--no-push]
push                    publier les commits locaux en un seul mouvement
sync [--offline]        récupérer, afficher le delta, ré-épingler la session
find "<mots>"           chercher dans tout le corpus
show <id>               afficher une entrée en entier
list decisions|facts|notes
brief                   afficher le bloc injecté au démarrage
doctor                  vérifier le corpus, le plafond du brief et la plateforme
compact                 compacter la base d'objets
debug on|off|tail|clear tracer les interactions

hook-start | hook-prompt | hook-guard    points d'entrée des hooks Claude Code
```

## Structure du dépôt

```
*.go                     les sources du binaire (module Go à la racine)
build.sh                 reconstruit les cinq binaires dans dist/
install.sh / install.ps1 déposent dist/ dans un projet hôte

dist/                    ce qui est déposé dans un projet, tel quel
└── .claude/
    ├── cogit/
    │   ├── bin/cogit-<os>-<arch>   les binaires, ~3 Mo chacun
    │   ├── cogit.sh / cogit.cmd    choisissent le binaire de la plateforme
    │   └── README.md               la mise en route, côté projet hôte
    ├── agents/cogit-curator.md     l'agent qui tient le corpus en ordre
    └── skills/
        ├── cogit-recall/           lire le contexte avant de choisir une approche
        └── cogit-record/           enregistrer une décision, un fait, une note
```

## Développer

Les sources sont un module Go ordinaire, à la racine.

```sh
go build ./...     # compile
go test ./...      # ctx_test.go couvre le corpus, l'état et le rendu
./build.sh         # reconstruit les cinq binaires dans dist/
```

`build.sh` n'est à lancer **qu'au moment d'une release** : chaque exécution
ajoute un blob par plateforme dans l'historique git, et git gère mal le binaire
qui change souvent. Les binaires présents dans `dist/` sont ceux de la dernière
release ; ils ne sont pas régénérés à chaque modification des sources.

## Ce qui est lié à Claude Code, et ce qui ne l'est pas

Le cœur — corpus, branche, worktree, rendu, recherche — ne dépend que de `git`.
Trois points d'ancrage seulement supposent Claude Code :

| Chemin | Rôle |
|---|---|
| `.claude/settings.local.json` | déclare les trois hooks, écrit par `init` |
| `.claude/cogit/` | le binaire et ses lanceurs |
| `.claude/cache/cogit/` | l'état d'épinglage d'une session |

Porter cogit vers un autre agent revient donc à changer la façon dont les hooks
sont déclarés, pas le reste.
