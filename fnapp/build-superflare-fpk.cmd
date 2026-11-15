@echo off
setlocal DisableDelayedExpansion
choice /C YN /N /M "Increment patch version before building? (Y/N): "
if "%ERRORLEVEL%"=="1" goto bump
if "%ERRORLEVEL%"=="2" goto build
echo Build cancelled.
pause
exit /b 1
:bump
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0build-superflare-fpk.ps1" -BumpPatch %*
goto finished
:build
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0build-superflare-fpk.ps1" %*
:finished
set "buildExitCode=%ERRORLEVEL%"
echo.
if "%buildExitCode%"=="0" (
    echo SuperFlare FPK build completed successfully.
) else (
    echo SuperFlare FPK build failed with exit code %buildExitCode%.
)
pause
exit /b %buildExitCode%
