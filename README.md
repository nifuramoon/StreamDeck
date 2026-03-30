# StreamDeck Twitch Controller

Elgato Stream Deck用の高度なTwitch統合アプリケーション。フォロー中の配信者を監視し、配信開始時に通知、チャット操作、クイックメッセージ送信などを可能にします。

## 🎯 特徴

- **リアルタイム配信監視**: フォロー中の配信者を自動監視
- **高品質音声通知**: VoiceVox Engineによる自然な日本語音声
- **チャット操作**: ワンクリックでエモート/定型文送信
- **OAuth認証**: 安全なTwitch API認証
- **クロスプラットフォーム**: Linux/Windows対応
- **自動修正機能**: エラー検出と自動修復

## 📦 インストール

### 前提条件
- Go 1.25.0以上
- Elgato Stream Deck (物理デバイスがなくても動作可能)
- Twitch Developerアカウント

### クイックスタート

```bash
# リポジトリのクローン
git clone https://github.com/yourusername/streamdeck-twitch.git
cd streamdeck-twitch

# 依存関係のインストール
go mod download

# ビルド
./build.sh

# 実行
./streamdeck-twitch
```

### VoiceVox Engineのセットアップ（推奨）

より自然な音声通知のため、VoiceVox Engineをセットアップ：

```bash
# Dockerのインストール（未インストールの場合）
sudo pacman -S docker docker-compose
sudo systemctl enable --now docker
sudo usermod -aG docker $USER

# VoiceVox Engineの起動
./start_with_voicevox.sh
```

## ⚙️ 設定

### 初回セットアップ

1. Twitch Developer Portalでアプリケーションを登録
2. Client IDとClient Secretを取得
3. StreamDeckアプリ起動後、OAuthボタンで認証

### 設定ファイル

設定は `~/.config/streamdeck-twitch/config.json` に保存：

```json
{
  "client_id": "your_client_id",
  "client_secret": "your_client_secret",
  "scope": "user:read:email chat:read chat:edit",
  "notifications_enabled": true
}
```

## 🎮 使用方法

### 画面構成

1. **HOME画面**
   - Twitch: 配信者一覧へ
   - OAuth: Twitch認証
   - Setting: 設定画面

2. **Twitch画面**
   - 配信中のフォロワーを表示
   - 各ボタンで配信者詳細画面へ

3. **配信者画面**
   - エモート送信（6種類）
   - 配信視聴（ブラウザ起動）
   - 定型文送信

4. **設定画面**
   - 通知ON/OFF切り替え
   - テスト音声再生
   - デバイス再起動

### 通知機能

配信開始時にVoiceVox Engineで自然な音声通知：
- 例: 「四国めたんさんが配信開始」

## 🔧 開発

### ビルドオプション

```bash
# 通常ビルド
./build.sh

# 自動修正モードでビルド
./build.sh auto

# 自動修正モードでビルド＆実行
./build.sh run

# Windows用クロスコンパイル
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o streamdeck-twitch.exe .
```

### プロジェクト構造

```
StreamDeck/
├── main.go              # メインアプリケーション
├── config.go            # 設定管理
├── oauth.go             # Twitch OAuth認証
├── token_manager.go     # トークン管理
├── log_analyzer.go      # ログ分析機能
├── auto_fix.go          # 自動修正システム
├── device_linux.go      # Linux用デバイス制御
├── device_windows.go    # Windows用デバイス制御
├── build.sh             # Linuxビルドスクリプト
├── build.bat            # Windowsビルドスクリプト
├── start_with_voicevox.sh # VoiceVox統合起動スクリプト
└── README.md            # このファイル
```

### コーディング規約

- **簡潔さ**: 過剰な抽象化・冗長なコメント禁止
- **実用性**: 動作するコードを最優先
- **保守性**: 既存のコードスタイルを尊重
- **ログ出力**: 階層化ログ（debugLog/infoLog/warnLog/errorLog）

## 🐛 トラブルシューティング

### 音声が流れない場合

1. **通知設定の確認**:
   ```bash
   cat ~/.config/streamdeck-twitch/config.json | grep notification
   ```

2. **VoiceVox Engineの状態確認**:
   ```bash
   curl http://127.0.0.1:50021/version
   ```

3. **TTSエンジンのテスト**:
   ```bash
   # VoiceVoxテスト
   curl -s -X POST "http://127.0.0.1:50021/audio_query?text=テスト&speaker=2" | \
   curl -s -X POST -H "Content-Type: application/json" -d @- "http://127.0.0.1:50021/synthesis?speaker=2" > test.wav && \
   pw-play test.wav
   
   # espeakテスト
   espeak-ng -v ja -s 130 "テストです"
   ```

### 認証エラー

1. Twitch Developer Portalでアプリケーションを登録
2. 正しいRedirect URIを設定（`http://localhost:3000`）
3. 必要なスコープを設定

### Stream Deckが認識されない

Linuxの場合：
```bash
# udevルールの追加
echo 'SUBSYSTEM=="usb", ATTRS{idVendor}=="0fd9", ATTRS{idProduct}=="006d", MODE="0666"' | sudo tee /etc/udev/rules.d/50-streamdeck.rules
sudo udevadm control --reload-rules
```

## 📝 ライセンス

MIT License

## 🤝 貢献

1. フォークしてリポジトリをクローン
2. 機能ブランチを作成
3. 変更をコミット
4. プルリクエストを作成

## 📞 サポート

問題や質問がある場合は、GitHub Issuesを作成してください。

---

**注意**: このプロジェクトはTwitch APIを使用しています。Twitchの利用規約に従ってご利用ください。