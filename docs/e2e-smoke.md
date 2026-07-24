# hayari 基本 E2E smoke ケース

最終更新: 2026-07-10

外部フィード、TLS 証明書、デスクトップクライアントに依存しないローカル smoke ケース。各実行は一時ディレクトリと一時 DB を使い、既存データを変更しない。

## 前提

- Go 1.25 以降
- `curl`
- POSIX shell

## 実行手順

以下をリポジトリルートで **同じ shell セッション** に貼り付けて実行する。

```sh
set -eu

TMP_DIR="$(mktemp -d)"
PORT=17070
BASE_URL="http://127.0.0.1:${PORT}"
USER_NAME=e2e-user
PASSWORD=e2e-password

cleanup() {
  if [ -n "${SERVER_PID:-}" ]; then
    kill -TERM "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

go build -o "$TMP_DIR/hayari" ./cmd/hayari
"$TMP_DIR/hayari" \
  --addr "127.0.0.1:${PORT}" \
  --db "$TMP_DIR/yarr.db" \
  --user "$USER_NAME" \
  --pass "$PASSWORD" \
  >"$TMP_DIR/server.log" 2>&1 &
SERVER_PID=$!

for _ in $(seq 1 30); do
  if curl --silent --fail "$BASE_URL/healthz" >/dev/null; then break; fi
  sleep 1
done
curl --silent --fail "$BASE_URL/healthz" | grep -q '"ok":true'

# 1. 認証なしの REST API は拒否される。
test "$(curl --silent --output /dev/null --write-out '%{http_code}' "$BASE_URL/api/status")" = 401

# 2. Basic 認証で status を取得できる。
curl --silent --fail --user "$USER_NAME:$PASSWORD" "$BASE_URL/api/status" \
  | grep -q '"version":"dev"'

# 3. Web ログインが Cookie を発行し、その Cookie で API を使える。
curl --silent --output /dev/null --dump-header "$TMP_DIR/login.headers" \
  --cookie-jar "$TMP_DIR/cookies.txt" \
  --data-urlencode "username=$USER_NAME" \
  --data-urlencode "password=$PASSWORD" \
  --request POST "$BASE_URL/login"
grep -q ' 303 ' "$TMP_DIR/login.headers"
curl --silent --fail --cookie "$TMP_DIR/cookies.txt" "$BASE_URL/api/status" \
  | grep -q '"running"'

# 4. GReader ClientLogin は POST でトークンを返し、そのトークンで API を使える。
AUTH_TOKEN="$(curl --silent --fail \
  --data-urlencode "Email=$USER_NAME" \
  --data-urlencode "Passwd=$PASSWORD" \
  --request POST "$BASE_URL/accounts/ClientLogin" \
  | awk -F= '/^Auth=/{print $2}')"
test -n "$AUTH_TOKEN"
curl --silent --fail \
  --header "Authorization: GoogleLogin auth=$AUTH_TOKEN" \
  "$BASE_URL/reader/api/0/user-info" \
  | grep -q '"userName":"e2e-user"'

# 5. GReader の GET ログインは既定で拒否される。
test "$(curl --silent --output /dev/null --write-out '%{http_code}' \
  "$BASE_URL/accounts/ClientLogin?Email=$USER_NAME&Passwd=$PASSWORD")" = 405

# 6. 認証済み REST API でフォルダを作成・取得できる。
curl --silent --fail --user "$USER_NAME:$PASSWORD" \
  --header 'Content-Type: application/json' \
  --data '{"title":"E2E folder"}' \
  --request POST "$BASE_URL/api/folders" \
  | grep -q '"title":"E2E folder"'
curl --silent --fail --user "$USER_NAME:$PASSWORD" "$BASE_URL/api/folders" \
  | grep -q '"title":"E2E folder"'

# 7. SIGTERM で終了できる（graceful shutdown）。
kill -TERM "$SERVER_PID"
wait "$SERVER_PID"
SERVER_PID=

echo 'E2E smoke: PASS'
```

## 期待結果

最後に `E2E smoke: PASS` と表示されること。失敗時は `"$TMP_DIR/server.log"` を確認する。`trap` が一時 DB とバイナリを削除するため、失敗時にログを残したい場合は `cleanup` 内の `rm -rf` を一時的に外す。

## 対象外

- 実フィードの取得・更新（ネットワークや外部サービスに依存するため）
- HTTPS プロキシの設定検証
- Reeder / NetNewsWire の実機互換性
- UI のブラウザ操作・レスポンシブ表示

## ブラウザ手動 smoke（2026-07-10 実施済み）

ローカル起動後に 2NN RSS（`https://www.2nn.jp/rss/index.rdf`）を追加し、次を確認した。

- フィード取得後に、フィードを選択すると記事一覧が表示される
- 記事詳細に本文・著者・日時・外部リンクが表示される
- スターを付けた記事が starred フィルターに表示される
- 既読化した記事が unread から消え、all には残る
- スター付き未読記事が starred と unread の両方の状態を維持する
- 実運用 yarr から export した OPML を import し、フォルダ構造・フィード・記事取得を確認した
- Readability モードとモバイル・タブレット・デスクトップ幅の表示を確認した

注記: フィード追加直後の UI 自動更新は未実装。再読み込みまたはフィード選択で取得済み記事を表示できる。OPML の多数フォルダで表面化したサイドバー横並びは CSS で修正済み。
