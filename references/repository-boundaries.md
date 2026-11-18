# 仓库边界与交付约定

2026-11-18 确认创建公开仓库 `OUBIGFA/FreeMD`，默认分支 `main`，开源协议沿用上游 [ArcMD](https://github.com/Ciao1019/ArcMD) 的 MIT License（见 [LICENSE](../LICENSE)）。目标是完全基于 `Ciao1019/ArcMD` 进行深度优化、定制与功能增减。

## 历史与远端

- **上游基线**：`https://github.com/Ciao1019/ArcMD`，提交 `27322d387268751932624ef83a7c580cb0b068d5`（上游标签 `v0.2.1`）。
- **历史策略**：保留上游完整提交历史，不为清理当前工作树而改写历史提交。
- **远端约定**：`upstream` 仅用于读取 `Ciao1019/ArcMD` 并在本地禁用推送（`DISABLED`）；`origin` 的 fetch 与 push 均指向 `https://github.com/OUBIGFA/FreeMD.git`。仅推送 `main`，不自动迁移上游其他分支、标签或 Release 自动化。
- **子模块与 Git LFS**：上游基线与当前仓库均不使用 Git submodule 或 Git LFS；若未来引入，须先固定修订、核对许可并明确登记。

## 公开边界决策

| 路径或类别 | 处理方式 | 公开理由或风险 | 验证方式 |
| --- | --- | --- | --- |
| 应用源码、测试、`cmd/`、`internal/`、`tools/` | Git 跟踪 | 可审查、可构建的核心输入 | `tools/check_repo.py` 与 Go 测试 |
| `go.mod`、`go.sum`、`mygo.json`、`LICENSE`、`CHANGELOG.md` | Git 跟踪 | 依赖锁定、打包元数据与许可依据 | `tools/check_repo.py` 校验 `go-mod.json` |
| `assets/FreeMD-logo.png` | Git 跟踪并精确登记 | 维护者自供 FreeMD 品牌图，从 `dist/` 复制至受跟踪路径 | `references/repository-exceptions.json` SHA-256 |
| `resources/icon.*`、`internal/app/assets/ghost/*.png`、`internal/app/testdata/qabot/*.png`、`internal/editor/fonts/*.ttf` | Git 跟踪并精确登记 | 打包图标、原生幽灵动画素材、测试夹具与嵌入式 KaTeX 字体 | `references/repository-exceptions.json` SHA-256 |
| `docs/architecture.md`、`docs/plugins.md` | Git 跟踪 | 架构与插件契约技术文档 | `tools/check_repo.py` 相对链接检查 |
| `references/` 边界与例外登记 | Git 跟踪 | 公开边界规则与精确例外清单 | `tools/check_repo.py` 与 `tests/repository` |
| `third_party/` 来源、许可正文与 `manifests/` | Git 跟踪 | 第三方组件归属、上游基线与依赖校验 | `tools/check_repo.py` 校验 `upstreams.json` 与 `go-mod.json` |
| `.github/workflows/repository-check.yml`、`.python-version` | Git 跟踪 | 最小权限只读仓库卫生检查 CI | GitHub Actions |
| `/docs/images/`、`/docs/performance/`、`/docs/performance.md`、`/docs/building.md` | 移除跟踪并忽略 | 上游大型宣传图、GIF、性能采样、pprof 与本机路径记录 | `.gitignore` + `tools/check_repo.py` 禁止 |
| `/.github/workflows/build.yml` | 移除跟踪并忽略 | 上游 ArcMD 发布签名与发版工作流，不直接复用其发布密钥 | `.gitignore` + `tools/check_repo.py` 禁止 |
| `**/AGENTS.md`、`**/CLAUDE.md`、`**/.agents/`、`**/.claude/` | 移除跟踪并忽略 | 本地代理指令与会话状态，不进入公开主分支 | `.gitignore` + `tools/check_repo.py` 禁止 |
| `.local/`（按需创建） | 忽略，不提交 | 本地下载包、上游快照对比、工具链与私有测试输入 | `.gitignore` + `tools/check_repo.py` 探针 |
| `dist/`、`**/output/`、`bin/`、`build/` | 忽略，不提交 | 编译产物、打包输出与本地发布包（含原 `dist/FreeMD-logo.png`） | `.gitignore` + `tools/check_repo.py` 探针 |
| `**/_temp/` | 忽略，最终生效规则 | 一次性测试仓库与临时产物，必须位于 `.gitignore` 最后一条有效规则 | `.gitignore` + `tools/check_repo.py` |

## 本地资源位置

```text
.local/
  downloads/                 # 固定下载包
  upstream/<project>-<rev>/  # 上游源码快照与对比参考
  toolchains/                # 本地工具链缓存
  fixtures/                  # 不宜公开的本地测试输入
output/                      # 本地构建或验收输出
_temp/<task>/                # 单次临时文件与测试工作区
```

仅在实际存放内容时按需创建对应目录，不预先提交空目录占位。

## 验证入口

仓库边界检查与行为测试统一入口：

```sh
python -X utf8 tools/check_repo.py
python -X utf8 -m unittest discover -s tests/repository -v
```

当 `go.mod` 或 `go.sum` 变更时，需先运行 `python -X utf8 tools/update_go_inventory.py` 同步 [go-mod.json](../third_party/manifests/go-mod.json)。

仓库卫生检查仅验证 Git 暂存索引边界、忽略规则、编码/JSON、秘钥与本机路径模式、二进制登记及上游/依赖清单一致性；应用编译、全量 Go 测试与桌面界面验收须单独运行并如实报告覆盖范围。
