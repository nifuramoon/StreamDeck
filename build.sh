#!/usr/bin/env bash
set -e
cd "$(dirname "$0")"

echo "[→] 依存パッケージ確認..."
sudo apt-get install -y -qq libusb-1.0-0-dev libudev-dev 2>/dev/null

echo "[→] go.sum をクリーンアップ..."
rm -f go.sum

echo "[→] Go依存取得..."
go mod tidy

echo "[→] ビルド中..."
CGO_ENABLED=1 go build -o streamdeck-twitch .

echo "[✔] ビルド完了: ./streamdeck-twitch"
echo ""
echo "実行: ./streamdeck-twitch"
echo "(sudoは不要。udevルールが設定済みなら一般ユーザーで動きます)"
