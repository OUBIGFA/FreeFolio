# 上游 ArcMD 核查记录

记录每次核查 `upstream`（[Ciao1019/ArcMD](https://github.com/Ciao1019/ArcMD)）时已看过的最后提交。下次核查从这里记录的提交之后开始，不重复对比已核查范围。已决定跟进的内容按实际落地范围登记到 [upstreams.json](../third_party/manifests/upstreams.json) 的 `adoptedUpdates`；本文件只记录“看到哪里”和结论。

## 增量核查方法

```sh
git fetch upstream --no-tags
# 主分支：从上次记录的 main 提交之后开始
git log --format='%h %ad %s' --date=short <上次 main 提交>..upstream/main
# 记录中列出的其他分支（分支可能已被上游删除或合并）
git log --format='%h %ad %s' --date=short <上次分支提交>..upstream/<分支>
# 新出现的分支
git branch -r --list 'upstream/*'
```

- 不运行 `git fetch upstream --tags`，版本号以上游提交信息与 `CHANGELOG.md` 为准。
- 上游 squash 发布时，发布提交可能与分支上的实现提交内容重复，按分支提交逐条核对，发布提交只看差异部分（`git diff <分支提交> <发布提交>`）。
- 核查完成后在下表末尾追加一行；未跟进的条目不必重复评估，除非上游后续又改动了相同文件。

## 记录

| 核查日期 | 起点（不含） | `upstream/main` 末次核查提交 | 其他分支末次核查提交 | 结论 |
| --- | --- | --- | --- | --- |
| 2026-11-18 | — | `27322d387268751932624ef83a7c580cb0b068d5`（v0.2.1） | — | 作为 FreeMD 基线 |
| — | — | `5b2a59ae6ac5bc06009a70ab2a26a1c625593d08`（v0.2.4） | — | 选择性跟进，见 `adoptedUpdates` |
| — | — | `3bb4a3342b3f5ffe8cf65898b6bbd2a8f3f85376`（v0.2.5） | — | 选择性跟进，见 `adoptedUpdates` |
| 2026-11-24 | `3bb4a3342b3f5ffe8cf65898b6bbd2a8f3f85376` | `3c94584def6aaa5e6406ae2e30dd76c2990cb165`（v0.2.8 发布，squash） | `0.2.8`：`f8fe7b23981f669377c79d14539670728e803676` | 已核查 v0.2.6–v0.2.8 共 17 + 4 个提交。维护者决定：修复类与功能类全量跟进（以 FreeMD 约定为准），PDF 阅读器在空间与文件浏览模式均支持；EPUB 阅读器（N6、F10 `f8fe7b2`）暂不跟进；HTML 阅读器不引入 WebView。落地范围见 `adoptedUpdates` |
