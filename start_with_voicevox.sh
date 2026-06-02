#!/bin/bash

# StreamDeck Twitch with VoiceVox Engine 起動スクリプト
# 使用方法: ./start_with_voicevox.sh

set -e

echo "========================================"
echo "StreamDeck Twitch + VoiceVox Engine"
echo "========================================"

# カレントディレクトリをスクリプトの場所に設定
cd "$(dirname "$0")"

# VoiceVox Engineの起動チェック
VOICEVOX_URL="http://127.0.0.1:50021"
if curl -s "$VOICEVOX_URL/version" > /dev/null 2>&1; then
    echo "✅ VoiceVox Engine は既に起動しています"
else
    echo "🚀 VoiceVox Engine を起動します..."
    
    # Dockerコンテナが既に存在するか確認
    if sudo docker ps -a --format '{{.Names}}' | grep -q '^voicevox$'; then
        echo "📦 既存のVoiceVoxコンテナを起動します"
        sudo docker start voicevox
    else
        echo "📦 新しいVoiceVoxコンテナを作成します"
        sudo docker run -d --rm -p 50021:50021 --name voicevox voicevox/voicevox_engine:cpu-ubuntu20.04-latest
    fi
    
    # 起動を待機
    echo "⏳ VoiceVox Engine の起動を待機中..."
    for i in {1..30}; do
        if curl -s "$VOICEVOX_URL/version" > /dev/null 2>&1; then
            echo "✅ VoiceVox Engine 起動完了"
            break
        fi
        if [ $i -eq 30 ]; then
            echo "⚠️  VoiceVox Engine の起動に時間がかかっています"
            echo "⚠️  続行しますが、音声合成にVoiceVoxは使用されません"
        fi
        sleep 2
    done
fi

# StreamDeckアプリケーションのビルドチェック
if [ ! -f "./streamdeck-twitch" ]; then
    echo "🔨 StreamDeckアプリケーションをビルドします..."
    ./build.sh
fi

# 利用可能なTTSエンジンを表示
echo ""
echo "🔊 利用可能な音声合成エンジン:"
if curl -s "$VOICEVOX_URL/version" > /dev/null 2>&1; then
    echo "   ✅ VoiceVox Engine (最高品質)"
fi
if command -v espeak-ng > /dev/null 2>&1; then
    echo "   ✅ espeak-ng"
fi
if command -v spd-say > /dev/null 2>&1; then
    echo "   ✅ speech-dispatcher"
fi
if command -v festival > /dev/null 2>&1; then
    echo "   ✅ festival"
fi

echo ""
echo "🎮 StreamDeck Twitch を起動します..."
echo "========================================"

# StreamDeckアプリケーションを実行
exec ./streamdeck-twitch

# クリーンアップ関数（終了時に実行）
cleanup() {
    echo ""
    echo "========================================"
    echo "終了処理中..."
    
    # VoiceVoxコンテナを停止
    if sudo docker ps --format '{{.Names}}' | grep -q '^voicevox$'; then
        echo "🛑 VoiceVox Engine を停止します"
        sudo docker stop voicevox
    fi
    
    echo "✅ 終了処理完了"
    echo "========================================"
}

# 終了時にクリーンアップを実行
trap cleanup EXIT