@echo off
setlocal EnableExtensions EnableDelayedExpansion

title PML Studio - Validazione Locale Windows
cd /d "%~dp0"

set "LOG=%~dp0PML_LOCAL_VALIDATION.log"
set "BUILD_DIR=%~dp0.build-validation"\nset "EXE=%BUILD_DIR%\\PML Studio.exe"
set "SRC=%~dp0source"
set "RUNTIME=%SRC%\runtime_templates\plm_runtime_core.zip"
set "EXPECTED_RUNTIME_SIZE=30508129"

> "%LOG%" echo PML Studio - Validazione Locale Windows
>>"%LOG%" echo Data: %DATE% %TIME%
>>"%LOG%" echo.

echo ==========================================
echo    PML Studio - Validazione Locale
echo ==========================================
echo.

if not exist "%SRC%\go.mod" (
    call :fail "source\go.mod non trovato."
    goto :end
)

where go >nul 2>nul
if errorlevel 1 (
    call :fail "Go non installato oppure non presente nel PATH."
    goto :end
)

if not exist "%RUNTIME%" (
    call :fail "Runtime template mancante: source\runtime_templates\plm_runtime_core.zip"
    goto :end
)

for %%F in ("%RUNTIME%") do set "RUNTIME_SIZE=%%~zF"
if not "!RUNTIME_SIZE!"=="%EXPECTED_RUNTIME_SIZE%" (
    call :fail "Dimensione runtime template inattesa: !RUNTIME_SIZE! byte. Attesi %EXPECTED_RUNTIME_SIZE%."
    goto :end
)

echo [PASS] Struttura base e runtime template
>>"%LOG%" echo [PASS] Struttura base e runtime template

pushd "%SRC%"
if errorlevel 1 (
    call :fail "Impossibile entrare nella cartella source."
    goto :end
)

set "GOOS=windows"
set "GOARCH=amd64"
set "CGO_ENABLED=0"

echo [INFO] Versione Go:
go version
go version >>"%LOG%" 2>&1

echo.
echo [TEST] go test ./...
>>"%LOG%" echo.
>>"%LOG%" echo [TEST] go test ./...
go test ./... >>"%LOG%" 2>&1
if errorlevel 1 (
    popd
    call :fail "go test ./... fallito. Vedi PML_LOCAL_VALIDATION.log"
    goto :end
)
echo [PASS] Test Go

echo.
echo [TEST] go vet ./...
>>"%LOG%" echo.
>>"%LOG%" echo [TEST] go vet ./...
go vet ./... >>"%LOG%" 2>&1
if errorlevel 1 (
    echo [WARN] Go vet ha rilevato segnalazioni. Vedi PML_LOCAL_VALIDATION.log
    >>"%LOG%" echo [WARN] Go vet non bloccante: la baseline 5.1 contiene segnalazioni unsafe.Pointer note.
) else (
    echo [PASS] Go vet
    >>"%LOG%" echo [PASS] Go vet
)

echo.
echo [BUILD] PML Studio.exe
>>"%LOG%" echo.
>>"%LOG%" echo [BUILD] PML Studio.exe
go build -trimpath -ldflags="-H=windowsgui" -o "%EXE%" . >>"%LOG%" 2>&1
if errorlevel 1 (
    popd
    call :fail "Compilazione fallita. Vedi PML_LOCAL_VALIDATION.log"
    goto :end
)
popd

if not exist "%EXE%" (
    call :fail "La build e' terminata senza generare PML Studio.exe."
    goto :end
)

for %%F in ("%EXE%") do set "EXE_SIZE=%%~zF"
if "!EXE_SIZE!"=="0" (
    call :fail "PML Studio.exe e' vuoto."
    goto :end
)

echo [PASS] Build Windows x64: !EXE_SIZE! byte
>>"%LOG%" echo [PASS] Build Windows x64: !EXE_SIZE! byte
echo.
echo ==========================================
echo        VALIDAZIONE COMPLETATA: PASS
echo ==========================================
>>"%LOG%" echo.
>>"%LOG%" echo RISULTATO FINALE: PASS
set "RESULT=0"
goto :end

:fail
echo.
echo [FAIL] %~1
>>"%LOG%" echo [FAIL] %~1
>>"%LOG%" echo RISULTATO FINALE: FAIL
set "RESULT=1"
goto :eof

:end
echo.
echo Log:
echo "%LOG%"
echo.
pause
exit /b %RESULT%
