@echo off
cd /d "%~dp0"

echo [→] Go依存取得...
go mod tidy

echo [→] Windows用ビルド中...
set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64
go build -o streamdeck-twitch.exe .

if %ERRORLEVEL% EQU 0 (
    echo [✔] ビルド完了: streamdeck-twitch.exe
) else (
    echo [✗] ビルド失敗
    pause
    exit /b 1
)

echo.
echo 実行: streamdeck-twitch.exe
pause
