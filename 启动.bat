@echo off
chcp 65001 >nul
title AI_H_bloodontower - Launcher
cd /d %~dp0

rem 本机同时跑多个实例时用这两个端口，避免和原项目 8741/8742 冲突
set SRV_PORT=8841
set AI_PORT=8842
set AI_URL=http://127.0.0.1:%AI_PORT%

echo [1/2] starting AI service on %AI_PORT% ...
start "AI Service" cmd /c "cd /d %~dp0ai_service && start_ai.bat %AI_PORT%"

timeout /t 2 >nul

echo [2/2] starting game server on %SRV_PORT% ...
start "BOTC Server" cmd /c "cd /d %~dp0botc_server && start_server.bat %SRV_PORT% %AI_URL%"

timeout /t 3 >nul
start "" http://127.0.0.1:%SRV_PORT%/

exit /b 0
