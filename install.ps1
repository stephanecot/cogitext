<#
.SYNOPSIS
  Dépose cogit dans un projet, quel qu'il soit.

.DESCRIPTION
  Copie le contenu de `dist/` dans le projet cible, puis ajoute à son
  `.gitignore` et à son `.gitattributes` les lignes sans lesquelles cogit
  fonctionne mal : le worktree du contexte serait commité, et les binaires
  seraient corrompus par la conversion de fins de ligne.

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
  Write-Error "cogit : « $Target » n'est pas un répertoire."
}
if (-not (Test-Path -LiteralPath (Join-Path $Target ".git"))) {
  Write-Error "cogit : « $Target » n'est pas un dépôt git — cogit porte son contexte sur une branche."
}

$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$src = Join-Path $here "dist"
if (-not (Test-Path -LiteralPath (Join-Path $src ".claude"))) {
  Write-Error "cogit : payload introuvable dans $src."
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
Add-Line $ignore "/.cogit/" "# Worktree du contexte partage, monte par ``cogit init``."
Add-Line $ignore "/.claude/cache/" "# Cache des sessions Claude Code."
Add-Line $ignore "/.claude/settings.local.json" "# Reglages ecrits par ``cogit init`` : ils designent un binaire par OS."
Add-Line $attrs ".claude/cogit/bin/** binary" "# Les binaires cogit ne doivent subir aucune conversion de fins de ligne."
Add-Line $attrs ".claude/cogit/cogit.sh text eol=lf" "# Les lanceurs gardent les fins de ligne de leur plateforme."
Add-Line $attrs ".claude/cogit/cogit.cmd text eol=crlf" ""

Write-Output ""
Write-Output "cogit installe dans $Target — $copied fichier(s) copie(s), $kept conserve(s)."
if ($kept -gt 0) { Write-Output "Relancez avec -Force pour remplacer ce qui a ete conserve." }
Write-Output @"

Reste a faire, dans le projet :

  .claude\cogit\cogit.cmd init       # Windows
  .claude/cogit/cogit.sh init        # macOS, Linux, Git Bash

``init`` cree ou rejoint la branche de contexte, monte le worktree ``.cogit``,
et cable les hooks dans ``.claude/settings.local.json``.
"@
