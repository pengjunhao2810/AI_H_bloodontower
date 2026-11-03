@echo off
chcp 65001 >nul
title AI_H_bloodontower - Go Server
cd /d %~dp0

rem 用法: start_server.bat [端口] [AI服务地址]
rem 默认: 8741  http://127.0.0.1:8742
set PORT=%~1
if "%PORT%"=="" set PORT=8741
set AIURL=%~2
if "%AIURL%"=="" set AIURL=http://127.0.0.1:8742

rem SQLite 驱动需要 CGO
set CGO_ENABLED=1

if not exist botc-server.exe (
    echo [build] building botc-server.exe ...
    go build -o botc-server.exe .
    if errorlevel 1 (
        echo [error] build failed, please check Go environment
        pause
        exit /b 1
    )
)

if not exist data mkdir data

set BOTC_ADDR=127.0.0.1:%PORT%
set BOTC_AI_URL=%AIURL%
set BOTC_DB_PATH=%~dp0data\botc.db

echo [start] Go game server: http://127.0.0.1:%PORT%
echo [info] AI service: %AIURL%
botc-server.exe
pause
