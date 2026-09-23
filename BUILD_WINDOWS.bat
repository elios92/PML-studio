@echo off
setlocal EnableExtensions EnableDelayedExpansion

title PML Studio - Build Windows x64

cd /d "%~dp0"

echo.
echo ==========================================
echo       PML Studio - Build Windows x64
echo ==========================================
echo.

set "SRC="

REM Cerca prima la struttura consigliata: source\go.mod
if exist "%~dp0source\go.mod" (
    set "SRC=%~dp0source"
)

REM Se il BAT e' gia' accanto a go.mod usa la cartella corrente
if not defined SRC (
    if exist "%~dp0go.mod" (
        set "SRC=%~dp0"
    )
)

REM Ultimo tentativo: cerca go.mod nelle sottocartelle
if not defined SRC (
    for /r "%~dp0" %%F in (go.mod) do (
        if not defined SRC (
            set "SRC=%%~dpF"
        )
    )
)

if not defined SRC (
    echo ERRORE: go.mod non trovato.
    echo.
    echo Struttura consigliata:
    echo   PML Studio\
    echo      BUILD_WINDOWS.bat
    echo      source\
    echo         go.mod
    echo         *.go
    echo.
    echo Controlla anche che il file non sia stato rinominato in go.mod.txt
    echo.
    pause
    exit /b 1
)

where go >nul 2>nul
if errorlevel 1 (
    echo ERRORE: Go non e' installato oppure non e' nel PATH.
    echo.
    pause
    exit /b 1
)

echo Sorgente trovato:
echo "%SRC%"
echo.

pushd "%SRC%"
if errorlevel 1 (
    echo ERRORE: impossibile entrare nella cartella sorgente.
    pause
    exit /b 1
)

echo Directory di compilazione:
cd
echo.

echo Modulo Go:
go env GOMOD
echo.

set "GOOS=windows"
set "GOARCH=amd64"
set "CGO_ENABLED=0"

echo Compilazione PML Studio...
echo.

go build -trimpath -ldflags="-H=windowsgui" -o "%~dp0PML Studio.exe" .

set "BUILD_RESULT=%ERRORLEVEL%"

popd

echo.

if not "%BUILD_RESULT%"=="0" (
    echo ==========================================
    echo       COMPILAZIONE NON RIUSCITA
    echo ==========================================
    echo.
    pause
    exit /b %BUILD_RESULT%
)

echo ==========================================
echo       COMPILAZIONE COMPLETATA
echo ==========================================
echo.
echo EXE creato:
echo "%~dp0PML Studio.exe"
echo.

pause
