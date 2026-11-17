@echo off
title Legacy Router Page Opener
cd /d "%~dp0"

if exist "legacy-router-opener.exe" (
    legacy-router-opener.exe %*
    if %errorlevel% neq 0 pause
    exit /b %errorlevel%
)

where go >nul 2>nul
if %errorlevel% equ 0 (
    echo Building and starting Legacy Router Page Opener from Go source...
    go run . %*
    if %errorlevel% neq 0 pause
    exit /b %errorlevel%
)

where node >nul 2>nul
if %errorlevel% equ 0 (
    echo Starting fallback Node.js proxy...
    node --openssl-legacy-provider index.mjs %*
    if %errorlevel% neq 0 pause
    exit /b %errorlevel%
)

echo ======================================================
echo [ERROR] legacy-router-opener.exe was not found!
echo ======================================================
echo.
echo Download the standalone legacy-router-opener.exe release
echo (no Node.js or Go installation required), or install Go
echo from https://go.dev/dl/ to run from source.
echo.
pause
exit /b 1
