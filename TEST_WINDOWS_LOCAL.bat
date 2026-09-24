@echo off
setlocal EnableExtensions EnableDelayedExpansion

title PML Studio - Validazione Locale Windows
cd /d "%~dp0"

set "LOG=%~dp0PML_LOCAL_VALIDATION.log"
set "BUILD_DIR=%~dp0.build-validation"\if exist "%BUILD_DIR%" rmdir /s /q "%BUILD_DIR%"
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
>>"%LOG%" echo [PASS] Build Windows x64: !EXE_SIZE! byte\nrmdir /s /q "%BUILD_DIR%" >nul 2>nul
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
