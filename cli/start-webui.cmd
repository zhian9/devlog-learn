@echo off
rem Double-click this file to open the devlog-learn local web UI.
rem It uses the cache/output paths configured for this directory.
cd /d "%~dp0"

if not exist "devlog-learn.exe" (
  echo [devlog-learn] devlog-learn.exe not found.
  echo Build it first:  go build -o devlog-learn.exe ./cmd
  pause
  exit /b 1
)

devlog-learn.exe serve
if errorlevel 1 pause
