@echo off
setlocal EnableExtensions EnableDelayedExpansion
cd /d "%~dp0"

set "SRC=%~dp0source"
set "EXE=%~dp0PML Studio.exe"
set "OUT=%~dp0release"
set "VERSION_FILE=%SRC%\version.txt"

echo ==========================================
echo       PML STUDIO - PREPARA RELEASE
echo ==========================================
echo.

if not exist "%VERSION_FILE%" (
    echo [FAIL] Manca source\version.txt
    goto :fail
)
set /p VERSION=<"%VERSION_FILE%"
if "%VERSION%"=="" (
    echo [FAIL] Versione vuota.
    goto :fail
)
echo [INFO] Versione sorgente: %VERSION%

echo [TEST] Validazione Windows prima della release...
call "%~dp0TEST_WINDOWS_LOCAL.bat" <nul
if errorlevel 1 (
    echo [FAIL] La validazione locale non e passata. Release bloccata.
    goto :fail
)

if not exist "%EXE%" (
    echo [FAIL] PML Studio.exe non generato.
    goto :fail
)
for %%F in ("%EXE%") do if %%~zF LEQ 0 (
    echo [FAIL] PML Studio.exe vuoto.
    goto :fail
)

if exist "%OUT%" rmdir /s /q "%OUT%"
mkdir "%OUT%" || goto :fail
copy /y "%EXE%" "%OUT%\PML Studio.exe" >nul || goto :fail

powershell -NoProfile -ExecutionPolicy Bypass -Command "$h=(Get-FileHash -LiteralPath '%OUT%\PML Studio.exe' -Algorithm SHA256).Hash.ToLowerInvariant(); Set-Content -LiteralPath '%OUT%\PML Studio.exe.sha256' -Value ($h + '  PML Studio.exe') -Encoding ASCII"
if errorlevel 1 (
    echo [FAIL] Impossibile generare SHA-256.
    goto :fail
)

for /f "tokens=1" %%H in ('powershell -NoProfile -Command "(Get-FileHash -LiteralPath '%OUT%\PML Studio.exe' -Algorithm SHA256).Hash.ToLowerInvariant()"') do set "ACTUAL=%%H"
for /f "tokens=1" %%H in ('type "%OUT%\PML Studio.exe.sha256"') do set "EXPECTED=%%H"
if /I not "!ACTUAL!"=="!EXPECTED!" (
    echo [FAIL] Verifica SHA-256 fallita.
    goto :fail
)

> "%OUT%\RELEASE_INFO.txt" echo PML Studio v%VERSION%
>>"%OUT%\RELEASE_INFO.txt" echo.
>>"%OUT%\RELEASE_INFO.txt" echo GitHub tag: v%VERSION%
>>"%OUT%\RELEASE_INFO.txt" echo Asset richiesti:
>>"%OUT%\RELEASE_INFO.txt" echo - PML Studio.exe
>>"%OUT%\RELEASE_INFO.txt" echo - PML Studio.exe.sha256
>>"%OUT%\RELEASE_INFO.txt" echo.
>>"%OUT%\RELEASE_INFO.txt" echo SHA-256: !ACTUAL!

echo.
echo ==========================================
echo          RELEASE PRONTA: v%VERSION%
echo ==========================================
echo Cartella: "%OUT%"
echo Asset updater:
echo   PML Studio.exe
echo   PML Studio.exe.sha256
echo SHA-256: !ACTUAL!
echo.
exit /b 0

:fail
echo.
echo ==========================================
echo          RELEASE NON PREPARATA
echo ==========================================
exit /b 1
