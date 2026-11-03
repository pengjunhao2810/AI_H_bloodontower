@echo off
chcp 65001 >nul
title AI_H_bloodontower - AI Service
cd /d %~dp0

rem 用法: start_ai.bat [端口]
rem 默认: 8742
set "PORT=%~1"
if "%PORT%"=="" set "PORT=8742"
echo %PORT%| findstr /r "^[0-9][0-9]*$" >nul || set "PORT=8742"

if not exist venv (
    echo [setup] creating venv ...
    python -m venv venv
    if errorlevel 1 (
        echo [error] python not found, please install Python 3.10+
        pause
        exit /b 1
    )
)

call venv\Scripts\activate.bat

python -c "import flask, requests" 2>nul
if errorlevel 1 (
    echo [setup] installing requirements ...
    pip install -r requirements.txt
)

set AI_PORT=%PORT%
echo [start] AI service: http://127.0.0.1:%PORT%
python main.py
pause
