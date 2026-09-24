@echo off
setlocal EnableExtensions EnableDelayedExpansion

title PML Studio - Validazione Locale Windows
cd /d "%~dp0"

set "LOG=%~dp0PML_LOCAL_VALIDATION.log"
set "BUILD_DIR=%~dp0.build-validation"
set "EXE=%BUILD_DIR%\PML Studio.exe"
set "TEST_OUT=%~dp0test-build"
set "TEST_EXE=%TEST_OUT%\PML Studio.exe"
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
if exist "%BUILD_DIR%" rmdir /s /q "%BUILD_DIR%"
mkdir "%BUILD_DIR%"
if errorlevel 1 (
    popd
    call :fail "Impossibile creare la cartella build temporanea."
    goto :end
)
echo [BUILD] PML Studio.exe temporaneo
>>"%LOG%" echo.
>>"%LOG%" echo [BUILD] PML Studio.exe temporaneo
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

if exist "%TEST_OUT%" rmdir /s /q "%TEST_OUT%"
mkdir "%TEST_OUT%"
if errorlevel 1 (
    call :fail "Impossibile creare test-build."
    goto :end
)
copy /y "%EXE%" "%TEST_EXE%" >nul
if errorlevel 1 (
    call :fail "Impossibile copiare la build validata in test-build."
    goto :end
)
if not exist "%TEST_EXE%" (
    call :fail "test-build\PML Studio.exe non e' stato creato."
    goto :end
)
for %%F in ("%TEST_EXE%") do set "TEST_EXE_SIZE=%%~zF"
if "!TEST_EXE_SIZE!"=="0" (
    call :fail "test-build\PML Studio.exe e' vuoto."
    goto :end
)
rmdir /s /q "%BUILD_DIR%" >nul 2>nul

echo.
echo ==========================================
echo        VALIDAZIONE COMPLETATA: PASS
echo ==========================================
>>"%LOG%" echo.
>>"%LOG%" echo RISULTATO FINALE: PASS
echo.
echo BUILD DI TEST DA USARE:
echo "%TEST_EXE%"
>>"%LOG%" echo BUILD DI TEST: %TEST_EXE%
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
