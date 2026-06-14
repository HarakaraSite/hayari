# コードレビュー結果 — クライアント（HTML/JS/CSS）(2026-06-10)

対象: `src/assets/index.html`, `login.html`, `javascripts/app.js`, `key.js`, `api.js`,
`stylesheets/app.css`。重要度順。**修正時はチェックボックスを更新すること。**

---

## 🔴 重大（セキュリティ）

### C1. 記事本文の innerHTML 挿入による XSS

- [x] 修正済み（C方式 + a方式: サーバー側 bluemonday 浄化、2026-06-11）

**場所**: `app.js:386`（`showDetail`）、`app.js:456`（readability off 復帰時）

```js
detailBody.innerHTML = item.content || '<p><em>No content.</em></p>';
```

`item.content` は購読フィードから来る**第三者の生 HTML**。innerHTML 挿入では
`<script>` は実行されないが、`<img src=x onerror=...>`・`<svg onload=...>`・
`<iframe>` 等のイベントハンドラ/埋め込みは**実行される**。

セッション Cookie は HttpOnly（認証改善で対応済み）なので Cookie 窃取は防げるが、
XSS から**同一オリジンの API を Cookie 付きで叩ける**（フィード削除・OPML エクスポート流出・
設定改変など）。RSS リーダーで最も典型的かつ深刻な脆弱性。

**修正方針: C方式（サーバー側サニタイズ）で確定（2026-06-10 ユーザー承認済み）**

クライアントで innerHTML 前にサニタイズするのではなく、**サーバーが保存時に
`item.Content` を無害化（sanitize）してから DB に書き込む**。RSS リーダーでは
「危険な記事を弾く」のではなく「危険な部分だけ除去して残りは正常表示」する無害化が正解。

- **ライブラリ**: bluemonday（純 Go・依存少・実績十分）を採用。Go 側の依存が1つ増えるが、
  サニタイズは自前実装が最も危険な領域なので確立されたライブラリを使う。
  「Go は最小ライブラリ」方針の例外として承認済み。
- **浄化タイミング**: **保存時**（本家 yarr 流）。`src/worker/worker.go` の
  `CreateItems` 呼び出し直前、または `src/parser/feed.go` のパース時に `item.Content` を
  bluemonday の UGCPolicy ベースのポリシーで浄化する。
  - 配信時浄化（API レスポンス生成時）と比較: 保存時は1回処理で配信が速い。規則変更時は
    既存データが古いままになるが、再取得 or 移行で対応する。
- **ポリシー設計**: `bluemonday.UGCPolicy()` をベースに、必要なら `img`/`figure`/`pre`/
  `code`/`blockquote` 等のフィードでよく使うタグを許可。`a` の href・`img` の src は
  http/https/mailto のみ許可（`javascript:`/`data:`/`vbscript:` を除去）→ これにより
  **本文中のリンク XSS も同時に解決**。
- **C2 との関係**: bluemonday は本文中の `href`/`src` スキームを浄化するので C1 と一緒に
  本文側は解決。ただし `item.Link`（記事本体のリンク、別カラム）は別途対応が必要（C2 参照）。

**作業範囲**:
1. サーバー: bluemonday を導入し、`CreateItems` 直前で `item.Content` を浄化（**主作業**）
2. サーバー: `item.Link` のスキーム検証も保存時に行う（無効なら空に）→ C2 のサーバー側対応
3. サーバー: **`/page`（readability 用フルテキスト取得）のレスポンスも同じポリシーで浄化**
   （`handlePage`, `routes.go:501`）→ 下記「readability 経路」参照。a方式で確定
4. クライアント C1: **無修正**
5. クライアント C2: 多層防御の保険として `safeURL()` を入れるかは任意（推奨だが必須でない）
6. 既存データ移行: **不要（2026-06-10 ユーザー確認済み）**。実運用データはまだないため、
   浄化は新規取得分にのみ適用されればよい。マイグレーションや全再取得は行わない。

**readability 経路の対応（a方式で確定、2026-06-10 ユーザー承認済み）**:
`extractContent`（`app.js:462`、readability モード）はサーバーを経由せず `/page` で取得した
生 HTML をクライアントで処理しているため、本文（`item.Content`）の浄化だけでは XSS が残る。
→ **C1 と同じタイミングで `/page` のレスポンスもサーバー側で bluemonday ポリシーを
通して浄化する**（`handlePage` 内、`io.ReadAll` 後・JSON 返却前）。これにより
`extractContent` はクライアント無修正のままで安全になる（DOM 整形のみ担当）。
別タスクには切り出さず、C1 とまとめて実装する。

**実装メモ（2026-06-11 実装時の判断）**: `/page` は保存コンテンツと同一ポリシーではなく
**拡張ポリシー `content.PageHTML()`** を使う。UGCPolicy は `main` 要素・`class`/`role`
属性を除去するため、同一ポリシーだとクライアント `extractContent` のセレクタ
（`main, [role=main], .entry-content` 等）がマッチせず抽出精度が落ちる。拡張ポリシーは
`main`（+ AllowNoAttrs 明示。bluemonday のデフォルト属性なし許可リストに main が
含まれないため）と `class`/`role`（SpaceSeparatedTokens 制限付き）のみ追加で許可。
スクリプト実行に関わる要素・属性は引き続きすべて除去される。
保存コンテンツ（`content.HTML()`）は厳格ポリシーのまま（class は code/pre/span のみ）。

---

### C2. `javascript:` スキーム URL によるリンク XSS

- [x] 修正済み（サーバー側: content.Link() で item.Link を保存時検証、2026-06-11）

**場所**: `app.js:361`（`btnOpen.href = item.link`）、`app.js:372`
（meta の Original リンク `href="${encHTML(item.link)}"`）、`app.js:473`
（`window.open(state.selectedItem.link, ...)`）

`item.link` はフィード由来。`item.link = "javascript:fetch('/api/feeds/1',{method:'DELETE'})"`
のような値だと、ツールバーの「↗ Open」やメタの「Original」リンククリックで実行される。
`encHTML` は `"` `'` をエスケープするので**属性からの脱出は防げる**が、`javascript:`
スキーム自体は素通りする。

**修正方針（C方式に合わせて確定）**: 主対応は**サーバー側**で `item.Link` を保存時に
スキーム検証する（C1 の bluemonday 導入と同じ箇所、`CreateItems` 直前）。http/https
以外なら空文字にする。
- クライアント側 `safeURL(url)`（3か所適用）は**多層防御の保険として任意**。サーバーで
  浄化済みなら必須ではないが、`/page` 経由 readability や将来の浄化漏れに備えて入れておく
  価値はある（数行）。優先度は C1 サーバー対応より下。

---

## 🟡 中程度（機能バグ）

### C3. テーマ "Auto (system)" がシステム設定に追従せず常にライト固定

- [x] 修正済み（auto 時は data-theme 属性を削除、HTML のハードコードも除去。ブラウザでダーク/ライト追従を確認済み、2026-06-11）

**場所**: `app.js:711-714`（`applySettings`）、`index.html:2` / `login.html:2`
（`<html data-theme="auto">` ハードコード）

```js
document.documentElement.setAttribute('data-theme', s.theme === 'auto' ? 'auto' : s.theme);
```

Pico CSS v2 はシステム追従を `:root:not([data-theme])`（属性なし）の
`@media (prefers-color-scheme: dark)` で実現する。`data-theme="auto"` は
「dark でも light でもなく、かつ属性は存在する」状態なので、**ライトテーマで固定され
システムのダークモードを無視する**。

**修正方針**: auto のときは属性を**削除**する。
```js
if (s.theme === 'auto') document.documentElement.removeAttribute('data-theme');
else document.documentElement.setAttribute('data-theme', s.theme);
```
HTML 側のハードコード `data-theme="auto"` も削除（属性なしが正しい初期状態）。

---

### C4. レスポンシブの破綻 — タブレットで記事が読めず、モバイルでサイドバー消失

- [x] 修正済み（#app の show-detail / show-sidebar クラスでペイン遷移。≤900px: 記事選択で詳細が全面表示＋←戻るボタン、≤600px: リスト単独＋☰でサイドバー表示・ソース選択で自動復帰。q キーも詳細を閉じる。768px/375px/デスクトップの全幅でブラウザ検証済み、2026-06-11）

**場所**: `app.css:537-550`

- `max-width: 900px`: `#item-detail { display: none }` にするが、項目クリック時の
  `showDetail` は detail を表示しようとする → **600〜900px では記事本文が一切読めない**
- `max-width: 600px`: `#sidebar, #item-detail { display: none }` で項目リストのみ表示 →
  **フィード一覧・設定・追加に到達する手段がない**（サイドバー内のボタンが全部消える）

3カラム前提の固定グリッドを、画面幅で「単一ペインを切り替える」設計にしていないのが原因。

**修正方針**: モバイルは「サイドバー→項目リスト→詳細」を1ペインずつ遷移させる
（選択状態に応じて表示ペインを切り替え、戻るボタン `q`/← を用意）。
最低限の暫定対応として、狭幅では詳細を `display:none` ではなくオーバーレイ表示にし、
サイドバーへ戻るトグルを出す。

---

### C5. モーダル表示中もキーボードショートカットが背後で発火する

- [x] 修正済み（key.js で dialog[open] 検出時にスキップ。モーダル開閉両状態でブラウザ確認済み、2026-06-11）

**場所**: `key.js:6-24`、`app.js:823-840`

`Keys` の keydown は document 全体に付き、INPUT/TEXTAREA/contentEditable のみ除外。
`<dialog open>`（設定・編集・フィード追加モーダル）を開いて `<select>` 操作中や
モーダルにフォーカスがある状態で `j`/`k`/`s`/`r` 等を押すと、**背後のリスト操作や
スター/既読トグルが発火する**。select は INPUT ではないので除外されない。

**修正方針**: ハンドラ先頭で開いているモーダルを検出したら無視する。
```js
if (document.querySelector('dialog[open]')) return;
```

---

### C6. メタ内フィード名クリックが二重にハンドリングされる

- [x] 修正済み（showDetail 内の個別リスナを削除、委譲のみに。ブラウザで /api/items リクエストが1回になることを確認、2026-06-11）

**場所**: `app.js:376-383`（showDetail 内で都度 addEventListener）+
`app.js:785-793`（setupEventListeners の delegation）

同じ `[data-source-feed]` クリックに対し、(1) showDetail で要素ごとに張る個別リスナと
(2) detailMeta への委譲リスナの**両方が発火**し、`selectSource` →`loadItems` が2回走る。
要素は innerHTML 置換で作り直されるためリスナ蓄積はしないが、毎クリック2回実行は無駄。

**修正方針**: 委譲（setupEventListeners 側）だけ残し、showDetail 内の個別 addEventListener
（376-383行）を削除する。

---

## 🟢 軽微（品質・パフォーマンス・堅牢性）

### C7. 記事を開くたびにサイドバー全体を再描画

- [x] 修正済み（refreshBadges/setBadge による差分更新。フィード＋所属フォルダのバッジのみ更新し、DOM 再構築なしをブラウザで確認（既読化 62→61、未読戻し 61→62、ノード同一性保持）、2026-06-11）

**場所**: `app.js:320-324`（selectItem 内）、他 setItemStatus/markAllRead でも

未読→既読でバッジを1減らすために `renderSidebar()` でサイドバー DOM を全再構築している。
favicon `<img>` も作り直され（Cache-Control で再ネットワークは抑止されるが DOM churn と
チラつき）、さらに直後の `updateActiveSidebarItem()` は **renderSidebar が末尾(162行)で
既に呼んでいるため冗長**。

**修正方針**: 対象フィード/フォルダのバッジ要素だけを更新する差分関数
（`updateBadge(feedId, count)`）を作る。冗長な updateActiveSidebarItem 呼び出しも削除。

### C8. `encHTML` が `&` と `<` をエスケープしない

- [x] 修正済み（& < " ' をエスケープ、2026-06-11）

**場所**: `app.js:861-863`

`encHTML` は `"` `'` のみ。属性値（二重引用符）コンテキストでの引用符脱出は防げるが、
`&` を escape しないため正しい HTML エンティティにならない（`&amp;` 問題）。
C2 のスキーム検証と併せて、属性出力用は `&`→`&amp;` も含めるのが正しい。
（XSS 直結ではないが一貫性のため）

### C9. `loadItems` に catch がなく、失敗時に無言で空表示

- [x] 修正済み（catch で "Failed to load items" を表示、成功時は "No items" に戻す、2026-06-11）

**場所**: `app.js:235-262`

`try { ... } finally { state.loading = false }` のみで catch なし。401 は api.js が
リダイレクト処理するが、500 やネットワークエラーは未処理例外になり、リストは空のまま
「No items」表示でユーザーに区別がつかない。
**修正方針**: catch でエラーメッセージを表示（`itemListEmpty` を流用 or トースト）。

### C10. フォルダを開く操作と「ソースとして選択」が分離できない

- [x] 修正済み（三角トグル=開閉のみ（リロードなし）、フォルダ名クリック=選択。ブラウザで /api/items リクエスト 0 回（トグル時）を確認、2026-06-11）

**場所**: `app.js:141-150`

フォルダヘッダクリックで「開閉トグル」と「そのフォルダをソースに選択(loadItems)」が
同時に起きる。展開だけしたいユーザーも必ずソース切替＋再取得が走る。
**修正方針**: 三角トグル(`.folder-toggle`)クリックは開閉のみ、フォルダ名クリックは
選択、のように当たり判定を分ける。

### C11. アクセシビリティ — listbox ロールに option/選択状態がない

- [x] 修正済み（#item-list の li に role="option" + aria-selected を付与・選択時に切替。ツリー構造の #feed-list は listbox ロールを除去、2026-06-11）

**場所**: `index.html`（`role="listbox"` の ul）、`app.js` の li 生成

`#feed-list` / `#item-list` に `role="listbox"` を付けているが、子 li に
`role="option"` も `aria-selected` もなく、キーボードフォーカスも当たらない。
スクリーンリーダーには不完全なリストとして見える。
**修正方針**: li に `role="option"` と選択時 `aria-selected="true"` を付与、または
listbox ロール自体を外してプレーンなリストにする。

### C12. 未使用コードの除去

- [x] 修正済み（state.addFeedCandidates を削除。item.image はサーバー側 API 互換のため残置（UI で使う際は content.Link 経由にすること）、2026-06-11）

- `app.js:21` `state.addFeedCandidates` は宣言のみで未使用（実際は addFeedMode と
  DOM の radio で管理）
- `storage.Item.Image` / `item.image` はサーバーから返るが UI で未表示（将来機能なら
  コメントを、不要なら削除）

### C13. `relativeDate` / 描画で `item.title` 空のフォールバックは複数箇所に散在

- [x] 修正済み（itemTitle() ヘルパーに集約、2026-06-11）

`'(no title)'` が renderItems(292) と showDetail(358) に重複。ヘルパー化で十分。軽微。

---

## 修正の推奨順序

1. **C1, C2** — XSS。最優先。**C方式（サーバー側 bluemonday 浄化）で対応確定**。
   `item.Content` + `item.Link` + `/page` レスポンスをサーバーで浄化。クライアントは
   原則無修正（C2 の `safeURL()` のみ任意の保険）。サーバー #1（status を INSERT）と
   同じ `CreateItems` 周辺なのでまとめて実装する
2. **C3, C5** — 1〜数行で直る明確なバグ（テーマ追従、モーダル中ショートカット）
3. **C6, C7** — 二重発火と再描画の整理（パフォーマンス＋正しさ）
4. **C4** — レスポンシブ再設計（やや大きい）
5. **C8〜C13** — 品質・堅牢性・a11y

## 備考

- サーバー側レビューは `docs/code-review-2026-06-10.md` を参照
- **C1/C2 の方式は確定（2026-06-10）**: C方式（サーバー側サニタイズ）+ a方式
  （`/page` も同時に浄化）。ライブラリは bluemonday。既存データ移行は不要。
