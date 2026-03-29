# StreamDeck Twitch - AI Agent Instructions

## プロジェクト概要
- **目的**: Elgato Stream Deck用Twitch統合アプリケーション
- **言語**: Go 1.25.0
- **プラットフォーム**: Linux / Windows 対応
- **状態**: パフォーマンス最適化済み、ログ出力階層化済み

## 主要ファイル構成
```
StreamDeck/
├── main.go              # メインアプリケーション (2,300+行)
├── config.go            # 設定ファイル管理
├── oauth.go             # Twitch OAuth認証
├── token_manager.go     # トークン管理
├── log_analyzer.go      # ログ分析機能
├── auto_fix.go          # 自動修正システム ★新機能
├── device_linux.go      # Linux用Stream Deck制御
├── device_windows.go    # Windows用Stream Deck制御
├── build.sh             # Linuxビルドスクリプト
├── build.bat            # Windowsビルドスクリプト
└── AGENTS.md            # このファイル
```

## コーディング規約
### 基本方針
1. **簡潔に書く**: 過剰な抽象化・冗長なコメント禁止
2. **実用的**: 動作するコードを最優先
3. **保守性**: 既存のコードスタイルを尊重

### 具体的なルール
- **ログ出力**: 階層化ログを使用（debugLog/infoLog/warnLog/errorLog）
- **エラー処理**: エラーは即座に処理、適切なレベルでログ出力
- **命名**: 説明的だが簡潔な名前（英語推奨、日本語可）
- **コメント**: 複雑なロジックのみ、なぜそうするか説明

### 禁止事項
- 過剰なデザインパターン適用
- 不要なインターフェース抽象化
- 機能しない「将来のため」のコード
- 自己満足的なリファクタリング

## 重要な設定
```go
// main.go の重要なグローバル変数
var debugMode = false      // デバッグログ制御
var CID, CS, AT, RT, UID  // Twitch API認証情報
```

### 設定ファイル
- パス: `~/.config/streamdeck-twitch/config.json`
- 初回起動時に対話型セットアップ実行
- 環境変数でも設定可能

## よく使うコマンド

### ビルド
```bash
# 通常ビルド
./build.sh

# 自動修正モードでビルド
./build.sh auto

# 自動修正モードでビルド＆実行
./build.sh run

# Windows用ビルド（Linuxからクロスコンパイル）
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o streamdeck-twitch.exe .
```

### 実行
```bash
# 通常実行
./streamdeck-twitch

# 自動修正モードで実行
go run . --auto-fix
```

### 開発・デバッグ
```bash
# 依存関係更新
go mod tidy

# コードフォーマット
go fmt ./...

# 静的解析
go vet ./...

# テスト（テストファイルがあれば）
go test ./...
```

## 自動修正システム（新機能）
### 機能概要
1. **自動ログ保存**: `./logs/`配下にタイムスタンプ付きログ
2. **リアルタイム監視**: エラー検知時に自動分析
3. **自動修正**: 一般的なエラーパターンに対応
4. **安全制限**: 最大5回リトライ、無限ループ防止

### 対応エラー例
- Goビルドエラー（未定義変数、パッケージ不足など）
- ファイルシステムエラー（権限、存在しないファイル）
- Stream Deck接続エラー
- Twitch APIエラー（認証、レート制限）

### ログ構造
```
logs/
├── attempt_1_YYYYMMDD_HHMMSS.log  # 各試行の詳細ログ
├── latest.log                     # 最新のログ集約
└── fix_history.log               # 修正試行履歴
```

## パフォーマンス最適化済み項目
1. **CPU使用率**: mainLoop間隔 15ms → 50ms に最適化
2. **ログ出力**: 230箇所のログ出力を階層化・整理
3. **メモリ**: LRUキャッシュ実装（プロフィール画像など）
4. **ネットワーク**: HTTPクライアントのタイムアウト設定

## 注意事項
### 起動時
- 初回起動時は設定ファイル作成ガイドが表示される
- Twitch Developer Consoleでのアプリ登録が必要
- Client ID/Secretの設定必須

### 実行時
- Stream Deckデバイス接続が必要（なくても起動可能）
- インターネット接続必須（Twitch API呼び出し）
- ログは`~/.cache/streamdeck-twitch/logs/`にも保存

### デバッグ時
```go
// main.go の先頭付近で有効化
var debugMode = true
```

## コミット履歴からの学び
- リモート環境変数ストアは削除済み（env_remote.go）
- すべての機能は1つのリポジトリに統合済み
- 不要なリポジトリ（TwitchOauth）は削除済み

## 作業開始前の確認事項
1. このAGENTS.mdを読む
2. 現在のgit状態を確認 (`git status`)
3. 直近のコミットを確認 (`git log --oneline -3`)
4. ビルドが通るか確認 (`./build.sh` または `go build .`)
5. デバッグモードが必要か判断

---
*このファイルはAIエージェントがプロジェクトを理解し、効率的に作業するためのガイドです。定期的に更新してください。*