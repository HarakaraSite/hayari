# Hayari

[English version](README.md)

Go と Vanilla JS フロントエンドで作られた、セルフホスト型 RSS アグリゲーターです。
Hayari は、日本語の「流行り」に由来する名前です。

## 機能

- フロントエンドアセットを埋め込んだ単一バイナリ
- SQLite データベース（外部 DB 不要）
- デスクトップトレイアイコン対応（`gui` タグでビルド）
- FreshRSS `greader.php` 互換 API のサブセット
- フィード単位の記事タイトルキーワード除外（リテラル部分一致）
- Pico CSS による軽量 UI

## スクリーンショット

フィード追加後のフィード一覧、未読数、記事一覧、閲覧ペインです。Engadgetを選択し、記事を開いています。

![Engadget のフィードと記事を表示した Hayari](docs/images/hayari-engadget.png)

## ビルド

```sh
# サーバーのみ
make build

# デスクトップトレイ版
make build-gui
```

## 起動

```sh
./hayari --addr 127.0.0.1:7070 --db path/to/hayari.db --user your-user --pass your-password
```

## 安全な公開方法

Hayari 自体は TLS を提供しません。Caddy や nginx など、TLS を終端する
リバースプロキシの背後でのみ運用してください。

```text
ブラウザ / RSS クライアント -- HTTPS --> リバースプロキシ -- HTTP --> Hayari (127.0.0.1:7070)
```

- Hayari は `127.0.0.1` に bind し、HTTP リスナーをインターネットへ直接公開しないでください。
- リバースプロキシで HTTP から HTTPS へのリダイレクトを設定してください。
- 本番運用では必ず `--user` と `--pass` の両方を設定してください。両方がない場合、Hayari はローカル開発のため認証なしのリクエストを許可します。
- Hayari は既定で、loopback 外の認証なしリスナーを拒否します。`--allow-insecure-no-auth` は意図したローカル／テスト用途に限って、この保護を明示的に解除します。
- リバースプロキシのアクセスログには要求 URL が含まれるため、機密情報として扱ってください。
- リバースプロキシで HTTPS を終端する場合は、ブラウザセッション Cookie を HTTPS 経由だけで送るため `--secure-cookie` を指定して起動してください。

### Google Reader ログイン

既定でサポートするログイン方法は `POST /accounts/ClientLogin` です。GET の
クエリ文字列に資格情報を入れると、プロキシのアクセスログへ記録される可能性があるため、
GET は既定で無効です。

GET が必要な旧式クライアントに限り、明示的に有効化できます。

```sh
./hayari --allow-greader-login-get
```

この設定は HTTPS の背後でのみ使用し、プロキシログへのアクセスを制限してください。

## API

Hayari は FreshRSS の `greader.php` を基準にした Google Reader API の互換サブセットを
実装しています。2種類の URL 形式は同じ API を提供します。

- Google Reader 形式: `/accounts/ClientLogin` と `/reader/api/0/...`
- FreshRSS 形式: `/api/greader.php/accounts/ClientLogin` と
  `/api/greader.php/reader/api/0/...`

ReadKit と NetNewsWire による動作を確認しています。認証、フィードとフォルダの同期、
未読・スター状態、記事取得、`edit-tag`、一括既読をサポートします。

FreshRSS API／Google Reader API の完全な実装ではありません。たとえば `rename-tag` と
`disable-tag` は、確認済みクライアントのワークフロー対象外のため未実装です。対応する
エンドポイントと制限事項は [API リファレンス](docs/freshrss-api.ja.md) を参照してください。

## ライセンス

MIT
