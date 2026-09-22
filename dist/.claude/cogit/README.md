# cogit — contexte partagé entre utilisateurs

`cogit` porte les décisions, faits, notes et journal de l'équipe sur la branche
orpheline `refs/heads/context`, et câble trois hooks Claude Code qui les injectent,
les rafraîchissent et bloquent une écriture faite sous des règles non lues.

## Pourquoi un binaire

Ce dossier doit pouvoir être déposé dans **n'importe quel projet** — Java, Python,
Go, .NET — sans lui imposer de runtime. `cogit` est donc un binaire statique, un par
plateforme, sans aucune dépendance partagée. La seule chose qu'il exige est `git`,
ce qui est cohérent : tout le système est bâti dessus.

## Mise en route, dans un clone neuf

```sh
.claude/cogit/cogit.sh init        # macOS, Linux
.claude\cogit\cogit.cmd init       # Windows
```

`init` crée ou rejoint la branche, monte le worktree `.cogit`, et écrit
`.claude/settings.local.json` — **local et gitignoré**, parce qu'un `settings.json`
ne peut pas désigner un chemin de binaire différent selon l'OS. C'est la contrepartie
assumée des binaires commités : une commande après le clone, mais aucune installation
et aucune toolchain.

## Contenu

```
bin/cogit-<os>-<arch>    les binaires, ~3 Mo chacun
cogit.sh / cogit.cmd       choisissent le binaire de la plateforme courante
```

Les binaires sont produits par le dépôt **cogitext**, où vivent les sources et le
script de construction. Ils n'y sont reconstruits qu'**au moment d'une release** :
chaque exécution ajoute un blob par plateforme dans l'historique, et git gère mal
le binaire qui change souvent. Ce dossier ne se modifie donc pas à la main — il
se remplace, depuis cogitext, avec son installateur.
