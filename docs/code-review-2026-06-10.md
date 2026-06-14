# コードレビュー結果 — 論理エラー調査 (2026-06-10)

ビルド・全テスト通過済みの状態で、全 Go ソース（約3,100行）を対象に論理エラーを調査した結果。
重要度順。各項目はファイル:行番号付き。**修正時はこのファイルのチェックボックスを更新すること。**

> **関連（XSS のサーバー側対応）**: クライアントレビュー C1/C2 は C方式（サーバー側
> サニタイズ）で対応すると決定済み。bluemonday を導入し `CreateItems` 直前で
> `item.Content` を浄化、`item.Link` のスキーム検証も行う。詳細は
> `docs/code-review-client-2026-06-10.md` の C1 参照。本ファイルの #1（status を
> INSERT に追加）と同じ `CreateItems` 周辺を触るため、まとめて対応すると効率的。

---

## 🔴 重大（機能が壊れている）

### 1. `CreateItems` が `item.Status` を INSERT していない — フィルター機能が無効化

- [x] 修正済み

**場所**: `src/storage/item.go:161`（INSERT 文）、`src/worker/worker.go:183`（status 設定側）

INSERT 文に `status` カラムが含まれていない:

```sql
INSERT INTO items (feed_id, guid, title, link, date, content, author, image)
```

worker 側では `item.Status = applyFilters(item, filters)` でフィルター結果（`"read"`）を
設定しているが、この値は捨てられ、全アイテムが DB デフォルトの `'unread'` になる。
**フィルターの `mark_read` / `hide` アクションは現状まったく機能していない。**

**修正方針**: INSERT のカラムリストと VALUES に `status` を追加し、`item.Status` を渡す。
`applyFilters` が空文字を返すケースはない（必ず "read" か "unread"）が、念のため
空なら `'unread'` にフォールバックしてもよい。

---

### 2. `LastInsertId() == 0` による重複判定は SQLite では不正確

- [x] 修正済み（item.go）
- [x] 修正済み（feed.go）

**場所**: `src/storage/item.go:182`、`src/storage/feed.go:70`

両方に同じパターンがある:

```go
res, _ := insertItem.Exec(...)  // ON CONFLICT DO NOTHING
id, _ := res.LastInsertId()
if id == 0 { /* 重複としてスキップ */ }
```

SQLite の `last_insert_rowid()` は**コネクション単位で最後に成功した INSERT の rowid を
返し続ける**。`DO NOTHING` で挿入がスキップされても 0 にはならず、同じコネクションで
過去に挿入された別の行の ID（stale 値）が返る。database/sql のコネクションプールにより
他リクエストの INSERT 履歴も影響するため、再現は非決定的。

**影響**:
- `CreateFeed`: 既存 URL の再購読時に `GetFeed(staleID)` が別のフィードを返す、
  またはエラーになる可能性
- `CreateItems`: 重複アイテムの FTS エントリが誤った rowid で search テーブルに
  挿入される可能性

**修正方針**: `res.RowsAffected() == 0` で重複を判定する。

---

### 3. GReader API / Web UI でフィードをフォルダから外せない（no-op）

- [x] 修正済み（UpdateFeedFolder 追加。Web UI は folder_id の RawMessage で「キー欠落」と「null」を区別、GReader removeLabel も対応。テスト3件追加、2026-06-11）

**場所**: `src/server/greader.go:238-241`、`src/storage/feed.go:77-84`

GReader の `subscription/edit` で removeLabel 処理として
`UpdateFeed(feed.ID, titlePtr, nil)` を呼んでいるが、`UpdateFeed` は
`COALESCE(?, folder_id)` を使うため **`nil` は「変更しない」を意味し、フォルダ解除は
何も起きない**。コメント（"Remove from folder: pass nil explicitly"）と実装が矛盾。

同じ理由で Web UI の `PUT /api/feeds/:id` でもフィードをフォルダ外に移動する手段がない。

**修正方針**: 「未指定（変更なし）」と「NULL に設定（フォルダ解除）」を区別できる
シグネチャに変更する。例:
- `UpdateFeed(id, title *string, folderID *int64, clearFolder bool)` を追加、または
- 専用メソッド `ClearFeedFolder(id)` を追加し、greader.go の removeLabel 分岐と
  Web UI ハンドラー（JSON で `"folder_id": null` を明示送信された場合）から呼ぶ。
  JSON 側は `json.RawMessage` や二重ポインタ等で「キー欠落」と「null」を区別する必要あり。

---

## 🟡 中程度

### 4. `GET /api/settings` が `auth_secret`（HMAC 秘密鍵）をクライアントに返す

- [x] 修正済み（GET で除外）
- [x] 修正済み（PUT で書き込み禁止）— ホワイトリスト方式（theme/font_size/refresh_rate のみ許可）、2026-06-11

**場所**: `src/server/routes.go:453-478`（handleSettings）、`src/storage/settings.go:22`

`GetAllSettings()` は settings テーブル全件を返すため、認証改善で追加した
`auth_secret` がブラウザに漏れる。XSS があれば永続セッション偽造が可能。
また `PUT /api/settings` は任意キーを受け付けるため `auth_secret` を上書き破壊できる
（全セッション無効化、または攻撃者既知の鍵に差し替え）。

**修正方針**: handleSettings の GET/PUT 両方で `auth_secret` を予約キーとして除外する。
ついでに許可キーのホワイトリスト方式（`theme` / `font_size` / `refresh_rate`）にすると安全。

---

### 5. `refreshFeed` のメタデータ更新が既存値を上書き破壊

- [x] 修正済み（UpdateFeedMeta は CASE WHEN で空フィールドのみ更新。favicon は専用の UpdateFeedIcon（icon IS NULL 時のみ書込・競合も解消）に分離。テスト3件追加、2026-06-11）

**場所**: `src/worker/worker.go:142-146`、`src/storage/feed.go:86-91`

`needsMeta` は「title か siteURL の**どちらか**が欠けている」で真になるが、
`UpdateFeedMeta` は title / site_url の**両方を無条件で上書き**する。

**問題ケース**:
- `feed.SiteURL == ""` かつ `feed.Title` がユーザー設定済み
  → `parsed.Title` でユーザーのカスタムタイトルが上書きされる
- `feed.Title == ""` かつ `feed.SiteURL` 設定済み、`parsed.SiteURL == ""`
  → site_url が空文字で消される

**修正方針**: 欠けているフィールドだけ更新する。SQL を
`title = CASE WHEN title = '' THEN ? ELSE title END` のようにするか、
Go 側で更新対象フィールドを選んで個別 UPDATE する。
なお `fetchAndStoreFavicon`（worker.go:194-209）も `UpdateFeedMeta` を使っており、
favicon 保存とメタデータ更新が競合すると同様の上書きが起きうる点に注意
（icon だけ更新する専用メソッド `UpdateFeedIcon` の追加を推奨）。

---

### 6. GReader のスター付けで既読/未読状態が消失

- [ ] 修正済み（要設計判断）

**場所**: `src/storage/migration.go:31`（status 単一カラム設計）、
`src/server/greader.go:601-636`（greaderEditTag）、`src/server/greader.go:539-555`（xt= 処理）

status が単一カラム（`unread` / `read` / `starred`）のため:

1. 未読アイテムにスターを付けると `starred` になり「未読」情報が消える
2. スター解除で一律 `read` に戻すため、未読だったアイテムが既読化される
3. `xt=user/-/state/com.google/read`（既読除外）は `status='unread'` に変換されるため、
   **スター付き未読アイテムがクライアントの未読リストから消える**

Reeder / NetNewsWire 等の実クライアントテストで顕在化する。

**修正方針（推奨）**: `starred` を独立した BOOLEAN カラムに分離するマイグレーションを追加。
status は `unread` / `read` の2値にする。影響範囲が広い（ItemFilter、Web UI の
starred タブ、GReader のストリーム/タグ処理、既存データの移行）ので、修正項目の中では
最も工数が大きい。既存 DB の移行: `status='starred'` の行は `starred=1, status='read'`
に変換（未読情報は既に失われているため復元不能）。

---

## 🟢 軽微

### 7. GReader トークンが無期限・無制限に蓄積

- [x] 修正済み（TTL 30日を導入。ログイン時に期限切れトークンを掃除、検証時に期限チェック、2026-06-11）

**場所**: `src/server/greader.go:17-26, 62-65`

ログインごとに in-memory map にトークンが追加され、削除されることがない。
シングルユーザーでは実害は小さいが、有効期限（例: 30日）と期限切れ削除を入れる価値あり。
再起動でトークン全消失する点はクライアントが 401 で再ログインするため許容範囲。
（代替案: Web UI と同じ HMAC 署名トークンにすれば map 自体を廃止できる）

### 8. `refresh_rate=0`（無効）にすると設定変更が最大24時間反映されない

- [x] 修正済み（毎分の設定再チェック + 経過時間判定方式に変更。無効⇔有効の切替が1分以内に反映、2026-06-11）

**場所**: `src/worker/worker.go:61-86`

`refreshInterval()` が 0 を返すと `refreshCh` が nil になり、select は cleanup tick
（24時間）か stop までブロックする。その間に設定を変更しても再読込されない。
**修正方針**: 1分程度の設定再チェック用 ticker を追加するか、設定変更時に worker へ
通知するチャネルを設ける。

### 9. `handlePage` がステータスコード未チェック + SSRF 注意

- [x] 修正済み（非200は 502 を返す。SSRF のプライベートIP遮断は自己ホスト前提の許容リスクとして見送り、2026-06-11）

**場所**: `src/server/routes.go:501-527`

- 404/500 ページの HTML もそのまま `content` として返す → `resp.StatusCode != 200`
  ならエラーを返すべき
- 認証保護下とはいえ、任意 URL を取得できるため localhost / 内部ネットワークに到達可能
  （SSRF）。シングルユーザーの自己ホストでは低リスクだが、private IP への接続を
  拒否するなら `net.LookupIP` + プライベートレンジチェックで対応可能

### 10. `MarkAllRead` の SQL がフィルター空のとき偶然動いている

- [x] 修正済み（常に WHERE を明示出力。空フィルターのテスト追加、2026-06-11）

**場所**: `src/storage/item.go:199-211`

where 句が空のとき `AND i.status = 'unread'` が直前の `JOIN ... ON` 句に連結される。
有効な SQL で結果も等価（INNER JOIN のため）だが、意図と異なる構造で壊れやすい。
**修正方針**: `i.status = 'unread'` を where() の結果と明示的に結合し、常に `WHERE` を
出力する形にする。

### 11. 静的アセット（index.html 含む）が認証なしで配信される

- [x] 修正済み（"/" ハンドラを authMiddleware で保護。/login は自己完結（CDN+インライン）のため公開のまま、2026-06-11）

**場所**: `src/server/routes.go:22-28`

`/` と静的ファイルは authMiddleware を通らない。API は保護されているためデータ漏洩は
ないが、アプリの存在と UI 構造が無認証で露出する。気にするなら index.html と静的
アセットも認証下に置く（login.html と CSS/JS の依存関係に注意）。

### 12. gofeed パーサーをグローバル共有（並行性の懸念）

- [x] 修正済み（Parse 呼び出しごとに gofeed.NewParser() を生成、2026-06-11）

**場所**: `src/parser/feed.go:13`

`var fp = gofeed.NewParser()` を 5 並列の refresh から共有している。gofeed の
translator 遅延初期化に data race の懸念がある（確度は低め、`go test -race` で要確認）。
**修正方針**: `Parse()` 内で毎回 `gofeed.NewParser()` を生成する（十分軽量）。

---

## 修正の推奨順序

1. **#1, #2** — storage 層の独立した小修正。テスト追加も容易
2. **#4** — 認証改善（HMAC クッキー導入済み）のフォローアップとして即時対応すべき
3. **#3, #5** — UpdateFeed / UpdateFeedMeta のシグネチャ変更を伴う
4. **#9, #10, #12** — 各数行の修正
5. **#7, #8, #11** — 任意
6. **#6** — スキーマ変更を伴う最大の修正。FreshRSS クライアント実機テストの前に対応推奨
