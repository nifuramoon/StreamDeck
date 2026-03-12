#!/usr/bin/env bash
set -e
cd "$(dirname "$0")"

echo "[→] 依存パッケージ確認..."
sudo apt-get install -y -qq libusb-1.0-0-dev libudev-dev 2>/dev/null || true

echo "[→] go.sum をクリーンアップ..."
rm -f go.sum

echo "[→] Go依存取得..."
go mod tidy

echo "[→] Linux用ビルド中..."
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o streamdeck-twitch .

echo "[✔] Linux ビルド完了: ./streamdeck-twitch"

echo ""
echo "[→] Windows用クロスビルド中..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o streamdeck-twitch.exe . 2>/dev/null && \
  echo "[✔] Windows ビルド完了: ./streamdeck-twitch.exe" || \
  echo "[!] Windows クロスビルドはスキップ (CGO依存がある場合はWindows環境でビルドしてください)"

echo ""
echo "実行 (Linux): ./streamdeck-twitch"
echo "実行 (Windows): streamdeck-twitch.exe"
echo "(sudoは不要。udevルールが設定済みなら一般ユーザーで動きます)"
