# Changelog

## FreeFolio 0.1.1

FreeFolio 0.1.1 增加白板图片对比与标注、笔记导入和快速笔记，并完善同步恢复与文档编辑。

- **白板图片工作流**：导入的图片可转为图片输入节点，保留原附件、裁剪和翻转。生成节点保存图片历史，支持切换结果和 A/B 滑杆对比；对比预览不改变下游输入。
- **图片查看与标注**：可在独立窗口中缩放、查看和标注图片。保存标注会创建新的参考图，保留原图；选择“用于生图”后，仍需手动运行生成节点。
- **白板绘图与操作**：新增手绘图形和箭头样式、自动排列、大纲转思维导图及白板搜索。修正操作指针与节点布局，支持点击连线中点后删除连线。
- **笔记导入与快速记录**：可导入 Markdown 文件夹和思源笔记，保留原文件，并提供转换报告。快速笔记使用独立窗口，可从托盘打开，也可自行启用全局快捷键。
- **字体、历史与附件**：界面和正文可分别选择字体，支持搜索、预览及按需下载。历史版本可直接预览排版和改动。清理未使用附件前会重新核查引用并备份，再移入回收站。
- **云同步**：不同段落的修改可自动合并，无法合并时保留冲突副本。版本校验失败时可重新核对；反复失败时，可明确选择“以云端为准”或“以本地为准”恢复该空间，覆盖前保留备份。
- **Gemini 生图**：完善图像模型识别、参考图输入和分辨率设置。可选 1K、2K 或 4K，具体支持以所选模型为准。
- **表格与搜索**：可拖动表格边线调整宽度，并恢复自适应宽度；调整后保持阅读位置。文档搜索显示全部匹配结果，面板内可滚动浏览。
- **白板性能**：减少笔迹命中检查的重复计算，按可见区域加载普通图片和 AI 图片，并限制同时解码的数量。

升级后保留原有偏好。云同步仍默认关闭，开启后不立即同步；快速笔记的全局快捷键默认关闭，启用后需保持 FreeFolio 运行。

思源的动态查询等不能完整对应的内容会在导入报告中说明。新增白板图形在其他编辑器中可能显示为基础形状；需要保持外观时，请导出为 SVG、PNG 或 PDF。

下载：Windows x64 安装器 `FreeFolio-0.1.1-Setup.exe`。发布后可用 GitHub 附件提供的 SHA-256 摘要核对文件。

## FreeFolio 0.1.0

FreeFolio's first public release: a local-first native writing and knowledge studio built with Go and MyGo.

### Highlights

- **Documents** — Native Markdown and legacy `.arcmd` editing with full-width page mode, wide tables that can extend to the page margins (optional), drag-and-drop import, HTML export with syntax highlighting and Mermaid diagrams, vector PDF export, and document mind maps.
- **More formats** — Open source-code and plain-text files with a text tools menu, view converted formats read-only, and edit PDF pages. Loose files open in separate tabs and reopen at the position where you stopped reading.
- **Find and replace** — Match case, whole word, and regular expressions; PDF search supports the same options. The selection, table, and whiteboard toolbars have more office actions.
- **Whiteboards** — Native whiteboards with notes, shapes, arrows, and split panes. AI node workflows use a LiteGraph-based node graph, and node links stay separate from whiteboard lines.
- **OneBot AI assistant** — Conversation history, file and image attachments, multiple providers and API keys with key testing, ChatGPT sign-in, PDF full-text search with citations, and image and video generation through OpenAI-compatible connections.
- **Spaces** — Fast space creation from large folders, drag a folder onto + to create a space, pinned and collapsible spaces, and close a document to return to the home page.
- **Cloud sync (opt-in)** — WebDAV and S3 sync for selected spaces, with conflict copies, recovery backups, optional content encryption, system proxy detection, and sidebar alerts for sync failures and conflicts.
- **Updates and performance** — Verified Windows in-app updates from Settings → Update, faster startup, and idle memory release.

### Download

- Windows x64: `FreeFolio-0.1.0-Setup.exe` (verify it with the SHA-256 digest shown for this asset on GitHub)

## Upstream provenance

ArcMD's historical releases are not FreeFolio releases. See the [third-party credits](third_party/README.md) and [pinned source records](third_party/manifests/upstreams.json) for the upstream baseline, adaptations, and licenses.
