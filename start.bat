@echo off
title Legacy Router Page Opener
cd /d "%~dp0"

where node >nul 2>nul
if %errorlevel% equ 0 goto :RUN_NODE

echo ======================================================
echo [ERROR] Node.js was not found on your computer!
echo ======================================================
echo.
echo This tool requires Node.js v18 or higher to run.
echo Please download and install it for free from:
echo   https://nodejs.org/
echo.
echo After installing Node.js, run this file again.
echo.
pause
exit /b 1

:RUN_NODE
echo Starting Legacy Router Page Opener...
node index.mjs
pause
