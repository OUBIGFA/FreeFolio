# ArcMD 增量核查：2026-11-28

结论：优先跟进长文档跳转、行内公式和电子表格粘贴三组修复。本次已在 FreeFolio 当前代码上重现这三类问题。页面缩放、源码分栏和 Mermaid 状态图值得随后跟进；数据库与外部 Agent 接入应单独规划。

## 范围与来源

- 本地基准：`9c18e0400eebbce164abf595a5a16c223ecfa13d`，开始时工作区干净
- 上次主分支核查终点：`3c94584def6aaa5e6406ae2e30dd76c2990cb165`（v0.2.8）
- 本次主分支终点：`e62e05a30d8b58512a2b5a41973d38fb0637e8e0`（v0.5.1），范围内 13 个提交，覆盖 v0.3.0、v0.4.0、v0.5.0、v0.5.1
- `0.2.8` 分支仍为 `f8fe7b23981f669377c79d14539670728e803676`，没有新增提交
- 新发现的 `feat/database-engine` 分支终点为 `1aa21aedfee621c23d205136954ef16a419c390f`，有 4 个不在主分支历史中的提交；其源码树与主分支 v0.3.0 发布提交 `3509072` 完全相同，树哈希均为 `df80feacd8e7ac9861bb5a86a88349e6ab95401c`，不重复计算功能
- 主分支总差异为 299 个文件、75,420 行新增、1,347 行删除；这是功能筛选与重点代码抽查，不是逐行审计全部上游代码
- GitHub API 确认最新正式发布为 [v0.5.1](https://github.com/Ciao1019/ArcMD/releases/tag/v0.5.1)，发布时间为北京时间 2026-11-27 21:51:50
- MyGo 最新正式版仍为 [v0.10.0](https://github.com/egoist/mygo/releases/tag/v0.10.0)，与当前 `go.mod` 一致，无需升级

主要来源：[主分支增量](https://github.com/Ciao1019/ArcMD/compare/3c94584def6aaa5e6406ae2e30dd76c2990cb165...e62e05a30d8b58512a2b5a41973d38fb0637e8e0)、[固定版本更新说明](https://github.com/Ciao1019/ArcMD/blob/e62e05a30d8b58512a2b5a41973d38fb0637e8e0/CHANGELOG.md)。以下成本是依据代码范围给出的相对判断，不是工时承诺。

## 优先修复

| 编号 | 优先级 / 成本 | 用户可见问题与本地现状 | 跟进建议及依据 |
| --- | --- | --- | --- |
| F1 | 高 / 中 | 大文档跳到尚未排版的章节时可能崩溃；本地 900 块、含行内公式的用例已触发空指针 | 移植多轮可见区域排版收敛和目录跳转目标跟随；当前 `materializeViewport`、`ensureVisibleShaped` 仍只排版一轮。依据：本地 [virtualize.go](../internal/editor/virtualize.go)、[outline.go](../internal/editor/outline.go)，上游 v0.5.0、[#76](https://github.com/Ciao1019/ArcMD/issues/76) |
| F2 | 高 / 中 | 数字开头公式、字面 `$` 或行内代码前后的公式不渲染；本地已重现 | 移植按文本样式隔离公式扫描、数字开头公式导入和空白归一化，连同保存后重开测试一起跟进；不能只改显示。依据：本地 [markdown.go](../internal/editor/markdown.go)、[inline.go](../internal/editor/inline.go)，上游 v0.5.1、[#91](https://github.com/Ciao1019/ArcMD/issues/91) |
| F3 | 高 / 中 | 从 Excel/WPS 等复制表格时，如果剪贴板同时有图片，当前先贴图片；Windows Excel HTML 片段及单行表格也有缺口 | 优先识别真实 HTML/TSV 表格，处理 Windows 的裸 `tr` 片段、合并单元格、单元格文本与样式，同时保留浏览器复制图片行为。依据：本地 [clipboard.go](../internal/editor/clipboard.go)、[csvpaste.go](../internal/editor/csvpaste.go)、[html.go](../internal/editor/html.go)，上游 v0.5.1；本地已重现 |
| F4 | 高 / 中 | Windows 阅读中的 PDF 会保留系统文件流，移动、重命名或回收前未见上游新增的集中释放步骤 | 适配 PDF 句柄释放及失败后的重新打开，覆盖缓存、预览、分屏和独立窗口。依据：本地 [pdfviewer.go](../internal/app/pdfviewer.go)、[relocate.go](../internal/app/relocate.go)、[render_windows.go](../internal/pdfdoc/render_windows.go)，上游 v0.4.0。代码路径已核对，具体文件操作失败尚未实机复现 |
| F5 | 中 / 小至中 | 原子新建仍依赖硬链接，不支持硬链接的 exFAT/FAT32/部分 SMB 位置可能无法创建文件；文件名过滤尚未覆盖 `CON`、`NUL` 等 Windows 保留名 | 跟进文件系统兼容和保留名测试。上游的排他新建回退避免覆盖，但写入过程不再具备同等的原子可见性，需要结合 FreeFolio 写入约定适配。依据：本地 [fsutil.go](../internal/fsutil/fsutil.go)、[vault.go](../internal/vault/vault.go)，上游 v0.4.0；未在上述卷类型测试 |
| F6 | 中 / 中 | 外部空间引用的文件被移走或删除后，当前缺少统一失效标记与批量清理入口 | 连同后续保护修复一起移植：权限错误不视作失效、保护未保存及写入中的文档、同步资料库缓存、处理分屏与独立窗口；清理对象只是失效引用。依据：[新增入口 bfe0b3f](https://github.com/Ciao1019/ArcMD/commit/bfe0b3f)、[后续修复 b50e191](https://github.com/Ciao1019/ArcMD/commit/b50e191)，本地 [external.go](../internal/app/external.go)、[files.go](../internal/vault/files.go) |
| F7 | 中 / 小 | OneBot 引用正则排除了文件名中的 `#`；工具结果按字节硬截断可能截断中文字；Windows 反斜杠路径处理不统一 | 选择性移植引用解析、UTF-8 截断和路径归一化。依据：本地 [agent/space.go](../internal/agent/space.go)、[agent.go](../internal/agent/agent.go)、[spacefs/tools.go](../internal/spacefs/tools.go)，上游 v0.4.0；本次为静态核对 |
| F8 | 中 / 小至中 | 浮层菜单上悬停时，底层编辑器仍仅按指针几何位置更新段落控件 | 借鉴实际遮挡命中判断，保留 FreeFolio 的只读指针、表格控件和滚动条避让。依据：本地 [editor/view.go](../internal/editor/view.go)，上游 v0.5.0、[#77](https://github.com/Ciao1019/ArcMD/issues/77)；本次未做目检 |
| F9 | 中 / 小 | 插件 ZIP 安装主要按声明的解压大小累计，每个文件单独限流 | 借鉴整个归档的实际解压字节累计、超限报错和失败文件清理；保留当前 HTTPS、校验和、路径、权限检查及独立 WASM 协议。依据：本地 [registry.go](../internal/plugin/registry.go)，上游 v0.4.0 |
| F10 | 中 / 中 | LaTeX 对换行间距、独立竖线、否定关系及部分长箭头支持较少 | 跟进 `\\[8pt]` 等换行参数、`\lvert` / `\lVert`、`\not` 和可伸缩箭头，并补标题目录中的公式显示。依据：上游 [tex 测试](https://github.com/Ciao1019/ArcMD/blob/e62e05a30d8b58512a2b5a41973d38fb0637e8e0/internal/tex/tex_test.go)、v0.5.0、[#78](https://github.com/Ciao1019/ArcMD/issues/78) |

F1–F3 是已复现的问题；F4–F10 是代码对照后值得跟进的候选项，不代表已完成所有实际场景验收。

## 值得跟进的功能

| 编号 | 建议顺序 / 成本 | 功能与价值 | FreeFolio 适配范围 |
| --- | --- | --- | --- |
| N1 | 第二批 / 中至高 | 整页 50%–200% 等比缩放，支持快捷键、Ctrl+滚轮及比例菜单；比单独调字号更适合图片、表格、公式混排 | 当前没有对应页面缩放设置；需兼容已有手动表格宽度、目录避让、阅读锚点和白板/PDF 自己的缩放，打印保持 100%。来源：[f3599fc](https://github.com/Ciao1019/ArcMD/commit/f3599fc)、v0.4.0 |
| N2 | 第二批 / 中 | 源码语法高亮、源码查找替换、缩进、可调比例的源码与预览分栏 | FreeFolio 已能编辑源码并保存原文，本次新增价值是编辑辅助与同时预览；保留现有未编辑块、输入法提交和外部修改保护。来源：v0.5.0 [source.go](https://github.com/Ciao1019/ArcMD/blob/e62e05a30d8b58512a2b5a41973d38fb0637e8e0/internal/app/source.go)，本地 [view.go](../internal/app/view.go)、[document.go](../internal/app/document.go) |
| N3 | 第二批 / 中 | 原生 Mermaid `stateDiagram` / `stateDiagram-v2`，包含嵌套状态、分支、备注和转移边，可转白板并导出 HTML/PDF | 符合纯 Go 约束；当前 Mermaid 分派尚无状态图，需要保留现有 gantt、pie、chart 等扩展并接入 FreeFolio 白板。来源：v0.5.1 [state.go](https://github.com/Ciao1019/ArcMD/blob/e62e05a30d8b58512a2b5a41973d38fb0637e8e0/internal/mermaid/state.go)、[#89](https://github.com/Ciao1019/ArcMD/issues/89) |
| N4 | 第三批 / 中至高 | MDZip `.mdz` 把 Markdown 和附件打成单文件，支持原地只读阅读、页内跳转和解包，适合分享含图片的笔记 | 纯 Go 编解码可复用；必须覆盖 FreeFolio 自定义附件目录、白板 AI 媒体、无覆盖解包、只读和错误提示。EPUB 与 WebView 不随此功能引入。来源：[e7eb4be](https://github.com/Ciao1019/ArcMD/commit/e7eb4be)、[b506821](https://github.com/Ciao1019/ArcMD/commit/b506821)；已抽查归档路径、符号链接和体积上限 |
| N5 | 单独规划 / 高 | 多维数据库：表格、看板、日历、时间轴、列表、画廊、图表、动态流、表单，支持关联、汇总、公式与 CSV/Notion 导入 | 仅 `internal/database`、`internal/dbview` 和两处主入口已新增约 4.08 万行（含测试，尚未包含全部接线）。涉及 `.db.md`、元数据、跨库引用、同步与撤销；上游独立 `internal/dbview` 还需适配本项目 UI 依赖边界。来源：v0.3.0，后续 v0.4.0 补充排序、拖拽和一致性修复 |
| N6 | 单独规划 / 高 | 本地 MCP/CLI 接入，让 Codex 等外部 Agent 读写笔记、白板，窗口打开时同步到内存并保留撤销 | 当前 OneBot 是应用内部能力，不等同于外部 Agent 接口。需明确工具范围、连接鉴权、只读、备份、窗口关闭和并发写入规则；上游的数据库工具依赖 N5。来源：v0.4.0 [agentmcp](https://github.com/Ciao1019/ArcMD/tree/e62e05a30d8b58512a2b5a41973d38fb0637e8e0/internal/agentmcp) |

## 已有能力与不宜直接照搬的部分

- **清除格式**：本地已有入口和撤销，且 [TestClearFormatKeepsLinks](../internal/editor/find_index_test.go) 明确要求保留超链接。上游会删除链接，可单独借鉴快捷键和表格选区支持，不能直接替换语义
- **文档属性 / Frontmatter**：本地已有 [properties.go](../internal/editor/properties.go) 可视化属性编辑；上游新增的原始 YAML 高亮卡片属于补充呈现，不是填补完全缺失的能力
- **启动优化**：本地已有 [launch.go](../internal/app/launch.go) 延后工作、[wasm.go](../internal/plugin/wasm.go) 首次调用初始化，以及初始化前的单实例锁。上游减少目录统计和延迟非活动空间恢复可继续评估，但需先测当前瓶颈；不照搬 ArcMD 的默认目录迁移或宣称未实测提速
- **插件运行保护**：本地已有内存限制、调用超时、取消终止和标准输出上限，并使用不同插件协议；只补确实缺少的边界。不能把上游 256 MiB 上限机械覆盖本地 1 GiB 上限
- **资料库整理**：本地已有点击切换空间与文件移动能力；上游卡片内拖动、自动展开等属于交互增强，优先级低于稳定性修复
- **侧边栏密度**：紧凑 / 标准 / 舒适三档可以按需要增加；沿用 FreeFolio 图标和样式，不整体替换为上游主题
- **边距点击失焦**：可作为交互候选，需保留当前左侧框选、锁定后指针和表格滚动约定，不作为已确认缺陷直接改行为
- **AI 流式协议**：上游 Responses 断流分类值得参考，但本地已使用多供应商的 `protocols.go` / ChatGPT 实现，不能直接移植已不存在的 `responses.go`
- **既有排除项**：EPUB 暂不跟进、HTML 不引入 WebView、上游品牌/发布流程不迁移；本次没有改变这些决定

## 本次验证

环境为 Windows、Go 1.27.4、MyGo v0.10.0，设置 `CGO_ENABLED=0`，使用 `ui.NewTester` 无窗口测试。通过 Go 的临时文件覆盖机制把固定上游测试附加到当前测试文件，未修改受跟踪源码；配置目录指向本次独立临时目录。

| 上游测试 | 本地结果 | 可确认的结论 |
| --- | --- | --- |
| `TestVirtualizedOutlineJumpInlineLaTeXNoCrashAndLands` | 失败，`Editor.paint` 在 `internal/editor/view.go:1738` 发生空指针 | 900 块含公式文档的深处跳转能触发当前代码崩溃 |
| `TestInlineMathDigitStartAndAdjacentLiteralDollarRoundTrip` | 10 个子场景中 8 个失败、2 个通过，额外链接场景也失败 | 数字开头公式、字面 `$`、行内代码及含空白公式的显示问题存在；失败场景在初次显示断言即停止，因此未单独确认其保存后结果 |
| `TestSpreadsheetPasteOverPNGAndImagePastePreserved` | 16 个末级场景中 9 个失败、7 个通过 | Windows/Excel HTML、TSV、多格式剪贴板及部分表格样式存在缺口；普通截图、浏览器复制图片等 7 个场景通过 |

测试来源为上游固定提交 `e62e05a` 的 `internal/editor/virtualize_test.go`、`clipboard_test.go`、`reading_features_test.go`。公式测试仅省略末尾直接调用上游新增私有函数 `inlineTokensInBlock` 的 GFM 单元格检查，未改变前述场景断言。测试运行参数均为 `go test -overlay <临时映射> ./internal/editor -run '^<测试名>$' -count=1 -v -timeout=90s`。

本次没有移植修复或功能，没有运行全量测试、跨平台构建或生成安装器。测试失败是本次核查发现的问题，不是本次修改引入的回归。只保存本报告并追加核查记录，`adoptedUpdates` 保持不变。

建议实施顺序：F1–F3 → F4–F10 的适用部分 → N1–N3 → N4；N5、N6 分别确认范围后再做。
