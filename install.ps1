<#
.SYNOPSIS
  Dépose cogitex dans un projet, quel qu'il soit.

.DESCRIPTION
  Copie le contenu de `dist/` dans le projet cible — binaire et lanceurs, skills
  et agent pour Claude Code, hooks et instructions pour GitHub Copilot — puis
  ajoute à son `.gitignore` et à son `.gitattributes` les lignes sans lesquelles
  cogitex fonctionne mal : le worktree du contexte serait commité, et les
  binaires seraient corrompus par la conversion de fins de ligne.

  Rien n'est écrasé sans le dire : un fichier déjà présent et différent est
  signalé et conservé, sauf avec -Force.

.EXAMPLE
  .\install.ps1 C:\dev\workspaces\mon-projet
#>
[CmdletBinding()]
param(
  [Parameter(Mandatory = $true, Position = 0)][string]$Target,
  [switch]$Force
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path -LiteralPath $Target -PathType Container)) {
  Write-Error "cogitex : « $Target » n'est pas un répertoire."
}
if (-not (Test-Path -LiteralPath (Join-Path $Target ".git"))) {
  Write-Error "cogitex : « $Target » n'est pas un dépôt git — cogitex porte son contexte sur une branche."
}

$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$src = Join-Path $here "dist"
if (-not (Test-Path -LiteralPath (Join-Path $src ".claude"))) {
  Write-Error "cogitex : payload introuvable dans $src."
}

$copied = 0
$kept = 0
foreach ($file in Get-ChildItem -LiteralPath $src -Recurse -File -Force) {
  $rel = $file.FullName.Substring($src.Length).TrimStart('\', '/')
  $to = Join-Path $Target $rel
  if ((Test-Path -LiteralPath $to) -and -not $Force) {
    $same = (Get-FileHash -LiteralPath $file.FullName).Hash -eq (Get-FileHash -LiteralPath $to).Hash
    if (-not $same) {
      Write-Output "  conserve (different)  $rel"
      $kept += 1
      continue
    }
  }
  $parent = Split-Path -Parent $to
  if (-not (Test-Path -LiteralPath $parent)) { New-Item -ItemType Directory -Path $parent -Force | Out-Null }
  Copy-Item -LiteralPath $file.FullName -Destination $to -Force
  $copied += 1
}

function Add-Line {
  param([string]$File, [string]$Line, [string]$Header)
  if (-not (Test-Path -LiteralPath $File)) { New-Item -ItemType File -Path $File | Out-Null }
  $lines = @(Get-Content -LiteralPath $File -ErrorAction SilentlyContinue)
  if ($lines -contains $Line) { return }
  $prefix = if ($lines.Count -gt 0) { "" } else { $null }
  if ($null -ne $prefix) { Add-Content -LiteralPath $File -Value "" -Encoding utf8 }
  if ($Header) { Add-Content -LiteralPath $File -Value $Header -Encoding utf8 }
  Add-Content -LiteralPath $File -Value $Line -Encoding utf8
  Write-Output "  ajoute a $(Split-Path -Leaf $File) : $Line"
}

$ignore = Join-Path $Target ".gitignore"
$attrs = Join-Path $Target ".gitattributes"
Add-Line $ignore "/.cogitex/" "# Worktree du contexte partage, monte par ``cogitex init``."
Add-Line $ignore "/.claude/cache/" "# Cache des sessions des agents."
Add-Line $ignore "/.claude/settings.local.json" "# Reglages personnels de Claude Code : les hooks cogitex vont dans settings.json, commite."
Add-Line $attrs ".claude/cogitex/bin/** binary" "# Les binaires cogitex ne doivent subir aucune conversion de fins de ligne."
Add-Line $attrs ".claude/cogitex/cogitex.sh text eol=lf" "# Les lanceurs gardent les fins de ligne de leur plateforme."
Add-Line $attrs ".claude/cogitex/cogitex.cmd text eol=crlf" ""

Write-Output ""
Write-Output "cogitex installe dans $Target — $copied fichier(s) copie(s), $kept conserve(s)."
if ($kept -gt 0) { Write-Output "Relancez avec -Force pour remplacer ce qui a ete conserve." }
Write-Output @"

Reste a faire, dans le projet :

  .claude\cogitex\cogitex.cmd init       # Windows
  .claude/cogitex/cogitex.sh init        # macOS, Linux, Git Bash

``init`` cree ou rejoint la branche de contexte, monte le worktree ``.cogitex``,
et fusionne les hooks Claude Code dans ``.claude/settings.json``.

Les hooks des deux agents sont identiques sous Windows, macOS et Linux :
commitez ``.claude/settings.json``, ``.github/hooks/cogitex.json`` et
``.github/instructions/cogitex.instructions.md`` — c'est ce qui les donne a toute
l'equipe, et a l'agent cloud de Copilot, qui ne lit que ``.github/hooks/``.
"@
