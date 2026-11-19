# fileshare

自宅サーバーで動かす、個人向けのファイル共有サービスです。ブラウザから使える Web UI と、iOS のファイル App・Windows エクスプローラ・macOS Finder・Linux からマウントできる WebDAV を提供します。LAN 内は直接、外出先は Tailscale 経由でアクセスします。

## 特長

- **ブラウザ UI**: ドラッグ＆ドロップアップロード、フォルダ管理、画像/動画/音声/PDF/テキストのプレビュー、検索、ZIP ダウンロード
- **WebDAV**: 各 OS の標準ファイルマネージャから読み書き（`/dav/`）
- **共有リンク**: ファイル/フォルダを読み取り専用で共有。有効期限と失効に対応
- **ゴミ箱**: 削除はゴミ箱へ。復元・完全削除・空にする
- **自動削除**: 一定期間を過ぎたファイルはゴミ箱へ移動し、さらに一定期間後に完全削除
- **単一バイナリ**: Go の静的バイナリ。Nix でビルドし、systemd で常駐
- **既存サービスと分離**: 専用ユーザー、専用ポート、書き込み先を 1 ディレクトリに限定した systemd サンドボックス

## 構成

```
 [ブラウザ]   [iOS Files]   [Windows Explorer]   [macOS Finder]
      │             │               │                   │
      │ LAN: http://<server>:8087
      │ Tailscale: https://<host>.<tailnet>.ts.net（tailscale serve）
      ▼             ▼               ▼                   ▼
 ┌───────────────────────────────────────────────────────────┐
 │ fileshare.service   User=fileshare   Listen=0.0.0.0:8087  │
 │   Web UI / JSON API / WebDAV                               │
 │   認証・セッション・共有リンク・ゴミ箱・クォータ・自動削除 │
 └───────────────────────────┬───────────────────────────────┘
                             ▼
                  /srv/fileshare-data/
                    files/    ← 共有ツリー
                    .trash/   ← ゴミ箱
                    .meta/    ← shares.json / trash.json
                    .tmp/     ← アップロード一時ファイル
```

インターネットへのポート開放は行いません。LAN と Tailscale tailnet からのみ到達できます。

## セキュリティと分離

- 専用のシステムユーザー `fileshare`（nologin）で実行
- 専用ポート `8087`。起動前にポート衝突を確認し、使用中ならインストールを中止
- systemd の `ProtectSystem=strict` / `ProtectHome=true` / `CapabilityBoundingSet=` / `SystemCallFilter=@system-service` / `MemoryDenyWriteExecute=yes` などでサンドボックス化
- `MemoryMax=384M` / `CPUQuota=75%` / `IOWeight=50` / `Nice=10` で既存サービスを圧迫しない
- アプリ内クォータ（インストール時に空き容量から算出）＋空き容量 3GiB 未満での書き込み拒否
- ファイル操作は `os.Root` に閉じ込め、`..`・絶対パス・シンボリックリンク脱出を拒否
- パスワードは PBKDF2-SHA256（600,000 回）。セッション Cookie は `HttpOnly` / `SameSite=Strict`
- 変更系 API は `X-Fileshare` ヘッダ必須（CSRF 対策）＋ CSP / `nosniff` / `Referrer-Policy`
- HTML / SVG などの危険な形式は強制ダウンロード。インライン表示は画像・動画・音声・PDF・プレーンテキストのみ
- 共有トークンは 32 バイト乱数。有効期限・失効を毎回検証し、ログでは伏字化
- WebDAV は Basic 認証。検証結果を短時間キャッシュして PBKDF2 の連打を防ぐ

## セットアップ

### 前提

- Linux（systemd + ufw）
- Nix（既存プロジェクトと同じく `nix build` を使います）
- Git / curl / OpenSSL

### 1. インストール

サーバー上で実行します。`sudo` の呼び出し元ユーザーがリポジトリの所有者になります。

```bash
git clone https://github.com/paleblue-2032/fileshare.git ~/fileshare
sudo bash ~/fileshare/deploy/install-server.sh
```

- `/srv/fileshare` に配備し、`nix build` の成果物を systemd から起動します
- `/srv/fileshare-data` を作成します
- 初回のみ `.env` を生成し、**初期パスワードを一度だけ表示**します
- 2 回目以降は既存の `.env` を上書きしません
- 既存サービスの restart / stop / 設定変更は行いません

`sudo -i` などで `SUDO_USER` が取れない場合は、所有者を明示してください。

```bash
sudo ADMIN_USER=<user> bash ~/fileshare/deploy/install-server.sh
```

インストール後の確認:

```bash
systemctl status fileshare
sudo botctl status fileshare
curl -fsS http://127.0.0.1:8087/healthz
```

### 2. Tailscale（外出先からアクセスする場合）

```bash
sudo bash /srv/fileshare/deploy/install-tailscale.sh
sudo tailscale up --accept-dns=false --hostname=<hostname>
```

表示された URL をブラウザで開いてこのマシンを承認します。管理コンソールでこのノードの **key expiry を無効化**してください。

続いて HTTPS で公開します。

```bash
sudo bash /srv/fileshare/deploy/enable-tailscale-serve.sh
```

`tailscale serve status` に表示される `https://<host>.<tailnet>.ts.net` が Web UI の URL、`/dav/` が WebDAV の URL です。HTTPS 証明書が未有効の場合は、表示される案内から tailnet の HTTPS を有効化してください。

### 3. 端末から接続

| 場所 | URL |
| --- | --- |
| 自宅 LAN | `http://<server-ip>:8087` |
| 外出先（Tailscale） | `https://<host>.<tailnet>.ts.net` |
| WebDAV（LAN） | `http://<server-ip>:8087/dav/` |
| WebDAV（Tailscale） | `https://<host>.<tailnet>.ts.net/dav/` |

ブラウザでパスワードを入力すると、端末ごとに 30 日間のセッションが発行されます。

## WebDAV の接続

ユーザー名は任意（例: `fileshare`）、パスワードは Web UI と同じです。Windows の Basic 認証は HTTP では制限されるため、tailnet の HTTPS URL を使ってください。

### iOS / iPadOS（ファイル App）

1. 「ファイル」→「…」→「サーバへ接続」
2. サーバ: `https://<host>.<tailnet>.ts.net/dav/`
3. ユーザ名: `fileshare`、パスワード: 設定したパスワード

### Windows（エクスプローラ）

1. サービス `WebClient` を有効化
2. 「ネットワーク ドライブの割り当て」ではなく「ネットワークの場所を追加」で `https://<host>.<tailnet>.ts.net/dav/` を指定
3. またはコマンドプロンプトで:
   ```bat
   net use Z: https://<host>.<tailnet>.ts.net/dav/ /user:fileshare
   ```

### macOS（Finder）

1. Finder →「移動」→「サーバへ接続」
2. `https://<host>.<tailnet>.ts.net/dav/`

### Linux

```bash
gio mount davs://<host>.<tailnet>.ts.net/dav/
```

## 使い方

- **アップロード**: 右上の Upload、またはファイルを画面へドラッグ＆ドロップ
- **フォルダ**: New folder。行の「…」から Rename / Move / Move to Trash
- **プレビュー**: ファイル名をクリック（画像・動画・音声・PDF・テキスト）
- **ZIP**: フォルダの「…」→ Download as ZIP
- **共有リンク**: ファイル/フォルダの「…」→ Share link。期限を選んで作成
- **検索**: ヘッダーの検索欄。現在のフォルダ以下を名前で検索
- **ゴミ箱**: ヘッダーのゴミ箱アイコン。Restore / Delete forever / Empty trash

## 自動削除（保持期間）

既定では次の 2 段階です。UI のフッターにも表示されます。

1. 最終更新から **30 日**を過ぎたファイルをゴミ箱へ移動
2. ゴミ箱に入ってから **30 日**を過ぎた項目を完全削除

`.env` で変更できます。`0` を指定するとその段階を無効化します。

```env
FILESHARE_RETENTION_DAYS=30   # 0 = 自動でゴミ箱へ移動しない
FILESHARE_TRASH_DAYS=30       # 0 = 自動で完全削除しない
FILESHARE_SWEEP_MINUTES=30    # 実行間隔
```

- 判定はファイルの mtime（最終更新）を使います。アップロードや WebDAV での更新で延命されます
- 空のディレクトリは自動削除の対象外です
- ゴミ箱の中身もクォータに算入されます

## 設定（`.env`）

`/srv/fileshare/.env` は root 所有 600 です。変更後は `sudo botctl restart fileshare`。

| 変数 | 既定値 | 内容 |
| --- | --- | --- |
| `FILESHARE_ADDR` | `0.0.0.0:8087` | 待受アドレス |
| `FILESHARE_DATA_DIR` | `/srv/fileshare-data` | データディレクトリ |
| `FILESHARE_PASSWORD_HASH` | 必須 | PBKDF2 ハッシュ |
| `FILESHARE_DAV_PASSWORD_HASH` | 空 | WebDAV 専用パスワード（空ならメインと同じ） |
| `FILESHARE_QUOTA_BYTES` | インストール時に算出 | 合計容量の上限 |
| `FILESHARE_MAX_UPLOAD_BYTES` | 5 GiB | 1 リクエストの上限 |
| `FILESHARE_MIN_FREE_BYTES` | 3 GiB | これを下回る書き込みを拒否 |
| `FILESHARE_WEBDAV` | `1` | WebDAV の有効/無効 |
| `FILESHARE_RETENTION_DAYS` | 30 | ゴミ箱へ移動するまでの日数 |
| `FILESHARE_TRASH_DAYS` | 30 | 完全削除までの日数 |
| `FILESHARE_SWEEP_MINUTES` | 30 | 自動削除の実行間隔 |
| `FILESHARE_TLS_CERT` / `FILESHARE_TLS_KEY` | 空 | 直接 TLS を終端する場合の証明書 |

### パスワード変更

```bash
printf '%s' 'new-password' | /srv/fileshare/result/bin/fileshare hash-password
sudo nano /srv/fileshare/.env   # FILESHARE_PASSWORD_HASH を置き換える
sudo botctl restart fileshare
```

## 運用

`botctl` が唯一の操作入口です。

```bash
sudo botctl status fileshare
sudo botctl logs fileshare
sudo botctl follow fileshare
sudo botctl restart fileshare
sudo botctl deploy fileshare   # git pull → nix build → restart
```

`commandcode` / `kosen` も同じ `botctl` から操作できます。

### バックアップ

データは `/srv/fileshare-data` にあります。整合性のためサービスを止めてから取得してください。

```bash
sudo systemctl stop fileshare
sudo tar -C /srv -czf /path/to/backup/fileshare-$(date +%F).tar.gz fileshare-data
sudo systemctl start fileshare
```

復元は停止中に `/srv/fileshare-data` を置き換えます。`.meta/` も忘れずに含めてください。

### データの場所

| パス | 内容 |
| --- | --- |
| `/srv/fileshare-data/files/` | ユーザーに見える共有ツリー |
| `/srv/fileshare-data/.trash/` | ゴミ箱の実体 |
| `/srv/fileshare-data/.meta/` | 共有リンク・ゴミ箱のメタデータ |
| `/srv/fileshare-data/.tmp/` | アップロード中の一時ファイル |

## 開発

```bash
nix develop
go test ./...
go vet ./...
go build ./cmd/fileshare
```

ローカル起動:

```bash
tmp=$(mktemp -d)
hash=$(printf '%s' 'test-password' | go run ./cmd/fileshare hash-password)
FILESHARE_DATA_DIR="$tmp" \
FILESHARE_ADDR=127.0.0.1:18087 \
FILESHARE_PASSWORD_HASH="$hash" \
FILESHARE_MIN_FREE_BYTES=0 \
go run ./cmd/fileshare
```

`http://127.0.0.1:18087` を開きます。テストは `internal/auth` / `internal/store` / `internal/web` にあり、パストラバーサル、シンボリックリンク脱出、クォータ、保持期間、共有リンクの期限、WebDAV の PROPFIND/PUT/DELETE を含みます。

### 本番の検証例

```bash
systemctl is-active commandcode-discordbot toyamakosen-bus-bot fileshare
systemd-analyze security fileshare.service
systemctl show fileshare -p User -p MemoryMax -p CPUQuotaPerSecUSec \
  -p CapabilityBoundingSet -p NoNewPrivileges -p ProtectSystem -p ReadWritePaths
ss -tlnp | grep 8087
ufw status numbered
curl -fsS http://127.0.0.1:8087/healthz
```

既存 2 サービスの `ActiveEnterTimestamp` / `NRestarts` がインストール前後で変わっていないことも確認してください。

## トラブルシューティング

| 症状 | 確認 |
| --- | --- |
| ログインできない | `sudo botctl logs fileshare`。パスワードハッシュの形式と `.env` の権限 |
| LAN からつながらない | `ufw status numbered`、`ss -tlnp \| grep 8087`、サーバーの IP |
| Tailscale からつながらない | `tailscale status`、`tailscale serve status`、tailnet の HTTPS 設定 |
| WebDAV で認証が通らない | HTTPS URL か、ユーザー名は任意・パスワードが正しいか |
| アップロードが 507 で失敗 | クォータ、空き容量、`FILESHARE_MIN_FREE_BYTES` |
| ファイルが消えた | ゴミ箱、自動削除のログ（`sudo botctl logs fileshare`） |
| 既存サービスに影響が出た | `systemctl status <unit>`。fileshare の停止は `sudo systemctl stop fileshare` のみ |

## 制限

- 認証は単一の共有パスワードです（ユーザーごとの権限分離はありません）
- 双方向の自動同期クライアントはありません。WebDAV マウントで代替します
- ファイルのバージョン履歴・サムネイル生成はありません
- 単一ディスク構成のため、冗長化はありません。バックアップは別途取得してください
- 自動削除は mtime ベースです。更新日時を意図的に変えると保持期間も変わります

## License

MIT
