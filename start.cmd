@echo off
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0start.ps1" %*
set "STUDYVAULT_EXIT=%ERRORLEVEL%"
if not "%STUDYVAULT_EXIT%"=="0" pause
exit /b %STUDYVAULT_EXIT%
