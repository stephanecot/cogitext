@echo off
REM Point d entree cogit sur Windows. Voir cogit.sh pour le pourquoi.
setlocal
set "BIN=%~dp0bin\cogit-windows-amd64.exe"
if not exist "%BIN%" (
  echo cogit : binaire absent pour windows/amd64 ^(%BIN%^) 1>&2
  exit /b 1
)
"%BIN%" %*
