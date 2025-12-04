---
name: AI 向けタスクテンプレート
about: AIに作業を進めていただく時に、本issueによりタスクを管理します
title: AI 向けタスクテンプレート
labels: ''
assignees: ''

---

CodeX の環境をセットアップするタスクを Issue で進めます。作業ルールは以下の通りです。

- 1つのタスクにつき、1つの PR を作成する
- `update` ブランチをベースにしてブランチを切る
- PR を作成したら私へレビューを依頼する
- レビューが OK なら、私が PR をマージしてタスク完了

```mermaid
flowchart LR
    start[タスクを開始する]
    create_environment[Environmentを作成する]
    create_issue_contents[]

    start --> create_environment
```

---

## タスク一覧

- [x] (task) 空の AGENTS.md を追加する — レポジトリのルートに追加 ([PR #2](https://github.com/suzuito/playground2-go/pull/2))
- [x] (task) README.md 更新: プロジェクトのソフトウェアアーキテクチャ（各ディレクトリに何を書くか） — [PR #5](https://github.com/suzuito/playground2-go/pull/5)
  - `internal/cmd/{command}/*.go` などの説明
- [x] (task) README.md 更新: テストの実行方法 / コマンド一覧の追記
  - テストの実行方法（単体テスト）
  - コマンド一覧（各コマンドの概要）
- [ ] (task) AGENTS.md 作成: AI 向けのルールを追記
  - 変更前に必ず `make test`, `make lint`, `make fmt` を実行する
  - 既存の Makefile ターゲットを優先して使う
  - 大きな設計変更やパッケージ追加は README の方針に従う
