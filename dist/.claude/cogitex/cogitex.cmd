@echo off
REM Point d entree cogitex sur Windows. Voir cogitex.sh pour le pourquoi.
setlocal
set "BIN=%~dp0bin\cogitex-windows-amd64.exe"
if not exist "%BIN%" (
  echo cogitex : binaire absent pour windows/amd64 ^(%BIN%^) 1>&2
  exit /b 1
)
"%BIN%" %*
