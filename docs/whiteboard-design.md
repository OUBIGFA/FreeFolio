# FreeFolio 原生白板设计

状态：设计提案，尚未实现。来源固定于 2026-11-20 核对的修订。

## 目标与采用范围

完整移植 Quiet.md 已实现的白板能力，用 FreeFolio 的纯 Go + MyGo 技术体系承载，并补充 Markdown 知识整理、双链、架构图素材和 Mermaid 转图。

“完整”指以下固定版本中实际存在的白板模型、工具、交互、文件交换、字体与导出能力。逐项完成并验证之前，不以基础图形可用、工具栏齐全或能够编译作为全量完成的依据。

| 来源 | 固定修订 | 用途 |
| --- | --- | --- |
| [Quiet.md](https://github.com/s1dashu/quiet-md/tree/e59c49adc3eb0bf8dabaec2df7e7d64f5a97e00a) | `e59c49adc3eb0bf8dabaec2df7e7d64f5a97e00a` | 白板功能、交互、格式、算法及测试的主要基线 |
| [Excalidraw](https://github.com/excalidraw/excalidraw/tree/3a317b55f508815d10a4c12acca99df30f026521) | `3a317b55f508815d10a4c12acca99df30f026521` | Quiet.md 标注的 0.18.0 移植基线；核对文件契约、算法及许可 |
| [ArcMD](https://github.com/Ciao1019/ArcMD/tree/70711e1694699207400f751c01d28b11bdaefac5) | `70711e1694699207400f751c01d28b11bdaefac5` | Markdown、双链、素材库、Mermaid 和问答定位的集成参考 |

现有 FreeFolio 尚未接入白板。文档、附件、搜索、双链、版本历史、分屏和 Mermaid 排版已经存在，应复用其公共入口。

## 方案取舍

| 方案 | 收益与限制 | 决定 |
| --- | --- | --- |
| Quiet.md 白板模型与行为移植到 Go，界面使用 MyGo，复用 FreeFolio 的文档能力 | 保留完整绘图范围与标准格式；需要移植算法、原生绘制和交互测试 | 采用，符合此次需求与现有技术约束 |
| 直接接入 ArcMD 的白板，再逐步增加绘图能力 | 集成较快，但其简化模型缺少图片资源，未知元素会降级为矩形 | 不作为白板数据模型和完成标准 |
| 直接嵌入 Quiet.md 的 Rust/GPUI 或网页 Excalidraw | 可直接复用另一套界面运行时 | 不符合纯 Go + MyGo、无 WebView/cgo 的约束 |

Quiet.md 的 GPUI、vello_cpu 和平台补丁不进入 FreeFolio。移植其绘图结果、缓存策略与交互行为，使用 MyGo 的窗口、文字、路径、图片和剪贴板能力实现。算法内部使用与来源一致的数值精度，仅在绘制边界转换到 MyGo 坐标类型。

本次不增加 Quiet.md 基线尚未提供、且需求未要求的实时多人协作、AI 生图、网页嵌入、激光笔、油漆桶、小地图和手绘转形状。素材库、元素链接和 Mermaid 转图属于明确要求的附加能力。

## 完整白板能力清单

下表每行均为交付要求。移植时在相应源码注释中保留来源模块、修订和必要差异，不以同名按钮代替真实行为。来源路径除特别标明外均相对于 Quiet.md 的 `crates/core/src/board/`。

| 编号 | 能力 | 可观察的验收结果 | 主要来源 |
| --- | --- | --- | --- |
| B01 | 场景模型与恢复 | 保留 ID、版本、层级顺序、分组、画框、绑定、图片和扩展字段；旧绑定按来源规则恢复 | `types.rs`、`restore.rs`、`json.rs`、`scene.rs`、`fractional_index.rs` |
| B02 | 基础工具 | 选择、套索、抓手、矩形、菱形、椭圆、箭头、直线、自由画笔、文字、图片、橡皮、画框和便签均可用 | `src/board/action.rs`、`src/board/tools/` |
| B03 | 选择和变换 | 点选、框选、套索、多选、组内编辑、缩放、旋转、水平/垂直翻转、复制、删除、锁定与解锁正确处理绑定关系 | `selection.rs`、`groups.rs`、`transform.rs`、`resize.rs`、`handles.rs`、`lasso.rs` |
| B04 | 排列和样式操作 | 层级升降、对齐、均匀分布、分组/解组、复制/粘贴样式；混合选区的属性修改只影响对应字段 | `actions/`、`align.rs`、`zindex.rs` |
| B05 | 连线 | 多点直线、曲线、直角箭头、端点绑定、箭头装饰、折点编辑、中点吸附、快速创建相连图形和标签随目标移动 | `linear.rs`、`binding.rs`、`elbow.rs`、`arrow_text.rs`、`flowchart.rs` |
| B06 | 手绘路径和填充 | 种子相同产生稳定路径；粗细、虚线、点线、透明度、粗糙度、圆角和来源支持的全部填充样式可读写及显示 | `rough/`、`shape.rs`、`freehand.rs`、`color.rs` |
| B07 | 文字、字体与便签 | 保留字体 ID、字号、水平/垂直对齐、自动换行、文字与容器绑定/解绑、便签自适应；中文输入法只提交确认文本 | `text.rs`、`text_edit.rs`、`text_wrapping.rs`、`text_measure.rs`、`sticky.rs` |
| B08 | 图片 | 插入、粘贴、拖入、缩放、旋转、翻转和无损裁剪；保留原图及裁剪参数；导出文件含所需图片资源 | `image.rs`、`crop.rs`、`src/board/tools/image.rs`、`src/board/tools/crop.rs` |
| B09 | 画框 | 创建、命名、由选区生成、加入/移出、选择框内元素、移动与裁切；屏幕与导出的裁切结果一致 | `frame.rs`、`src/board/frame_name.rs` |
| B10 | 视口和吸附 | 缩放、恢复缩放、适应全图、适应选区、网格、对象吸附、工具锁定和画布背景；平移/缩放不产生内容撤销记录 | `viewport.rs`、`snapping.rs`、`app_state.rs`、`src/board/canvas.rs` |
| B11 | 撤销与重做 | 一次拖动、一次画笔笔画和一次输入法提交各为一步；记录元素变化，撤销后绑定、资源及层级一致，选区有效 | `history.rs`、`mutate.rs`、`actions/` |
| B12 | 剪贴板 | 与 Excalidraw 交换 `excalidraw/clipboard`；接受文字、HTML、图片和 PNG/SVG 中的场景；复制 PNG/SVG 后粘回仍可编辑 | `clipboard.rs`、`encode.rs`、`src/board/run.rs` |
| B13 | 文件和导出 | 读写 `.excalidraw`，读取 PNG/SVG 嵌入场景，输出 SVG/PNG；文档 Markdown、HTML、PDF 和打印保留白板内容 | `crates/core/src/whiteboard.rs`、`json.rs`、`encode.rs`、`export_svg.rs`、`font_subset.rs` |
| B14 | 界面和键盘 | 工具栏、属性面板、颜色/字体选择、右键菜单、缩放/撤销、帮助、快捷键和清空确认均可用；浅色/深色下可读 | `src/board/chrome/`、`src/board/keys.rs`、`src/board/overlay.rs` |
| B15 | 绘制与生命周期 | 只绘制可见内容；复用未变元素的路径与文字；关闭白板释放缓存；缩略图在编辑区遮挡下不重复全图绘制 | `display.rs`、`src/board/render.rs`、`src/editor/board.rs` |

字体以来源的字体编号和度量为契约。纳入 Excalifont、Virgil、Nunito、Lilita One、Cascadia、Comic Shanns、Assistant、Xiaolai 所需字库；系统字体的选择、中文回退、字体子集和授权须单独验证。不能用统一系统字体替代全部手绘字体后宣称完整移植。

Quiet.md 已记录的缺口不能自动扩大为 FreeFolio 的承诺，例如画笔压感开关和未实现的网页元素交互。对已存在但不可编辑的外部对象，应保留数据并明确显示限制。

## 文件与 Markdown

### 文件选择

- 独立白板默认保存为标准 `.excalidraw`，文件树、打开文件、外部文件空间和分屏均识别该格式。
- 同时支持 `.excalidraw.svg`：SVG 提供普通阅读器可见的图像，标准嵌入场景提供重新编辑的数据。
- 在 Markdown 中新建白板时，默认在空间附件目录创建一个 `.excalidraw.svg`，正文写入普通图片引用。该文件同时拥有图像与编辑数据，无需维护第二份可编辑文件。
- 支持已有 `![[画板.excalidraw.svg]]`、普通 Markdown 图片引用，以及 `excalidraw` / `whiteboard` 围栏代码块。保留原来的存储方式，仅在明确执行转换时改变。
- `.arcmd` 复用图片引用和代码块表示，不引入新的未知块类型。未编辑的 Markdown 块继续由原有 `ImportSource` 路径保留。

独立白板的图片写入 Excalidraw `files` 资源表。读取 Quiet.md 的相对资源路径时使用既有空间路径校验；导出可携带的文件时包含图片内容。缺图应显示错误并保留原引用，不能保存成空资源。

### 一致性和恢复

场景 JSON 保留未知字段。无法安全理解的元素或未来格式不能被转换成矩形、空白板或新文档；保留原始内容并禁用会破坏数据的编辑。对标准默认值的恢复应有对应兼容测试。

沿用当前 700 ms 自动保存、原子写入、外部修改检测、版本历史和恢复入口。保存使用已提交的文字及一致的场景快照；失败保留脏状态并显示错误。白板的新建、改名、移动、附件目录迁移和删除沿用 `vault`、`fsutil`、`trashbin` 与系统回收站路径。

多个文档引用同一白板时共享同一可写场景会话，避免两个编辑器相互覆盖。未打开的引用只持有可失效的预览缓存。已有文件变化通过当前文件监听入口使预览、搜索和打开的白板同步更新；有未保存编辑时进入现有冲突处理流程。

## Markdown 知识整理和图形资料

| 编号 | 能力 | 可观察的验收结果 |
| --- | --- | --- |
| K01 | 文档内白板 | `/白板` 创建并进入编辑；双击预览进入编辑；返回文档保留滚动、光标及编辑结果；可在独立白板与文档分屏间切换 |
| K02 | 双链和元素链接 | 图形文字中的 `[[文档名]]` 与显式元素链接可补全、打开或按既有行为创建文档；普通 URL 使用现有打开链接入口 |
| K03 | 反向链接和改名 | 文档与白板互相发现引用；改名/移动后重写对应链接；跨空间名称解析遵循当前文档所在空间 |
| K04 | 搜索、标签和问答 | 索引文字、便签、画框名称、链接和标签，包括已提交但未保存的内容；搜索和问答引用定位到具体元素；不把 JSON 或图片编码送入正文索引 |
| K05 | 架构图素材 | 提供数据库、消息队列、云服务、三层架构、浏览器/手机线框、泳道、象限、思维图和参与者等组合；可保存选区、分类、检索和插入 |
| K06 | 素材交换 | 导入和导出 `.excalidrawlib`；插入分配新 ID，重建组内箭头/文字/画框关系，保留关联图片；导入失败不改现有素材库 |
| K07 | Mermaid 转图 | 将当前解析器支持的流程图和时序图转成可编辑图形、文字和连线；保留节点对应关系、标签、方向和分组；转换一次可撤销 |
| K08 | 导出和历史 | Markdown 图片在普通阅读器中可见；HTML/PDF/打印使用同一场景的绘制结果；版本历史可预览并恢复白板及其图片 |

架构图素材使用标准 Excalidraw 图形、线段与分组组合。数据库、云朵等复杂外形不能只写一个 FreeFolio 私有类型并要求其他阅读器自行识别。额外的应用元数据放入命名空间化的 `customData`，不覆盖原有内容。

Mermaid 转图复用 `internal/mermaid.ParseAndLayout`。它当前支持 `flowchart` / `graph` 和 `sequenceDiagram`；不支持的语法明确报错并保留源文本。转换是一次性生成可编辑白板，不承诺任意手工修改后的图形反向生成 Mermaid。源 Mermaid 在原文档中保留；替换原代码块是显式操作。

## 界面组织

白板占用当前文档区域，保留 FreeFolio 的空间侧栏、文档标题和分屏入口。文档内预览显示编辑入口；编辑时可扩展画布并通过返回入口回到原阅读位置。

基本布局保留 Quiet.md / Excalidraw 的顶部工具栏和左下缩放、撤销。画布变窄时，当前绘图工具保持可见，其余放入「更多工具」。普通图形的属性面板在宽画布上靠选区另一侧显示；窄画布点「图形样式」才展开。AI 节点操作随选区出现并避开可见画布边缘，完整参数从「全部设置」打开。选区工具参考 [tldraw 的上下文样式面板](https://tldraw.dev/examples/contextual-style-panel)，不改变白板文件、手绘图形或连线规则。

空白板提供可直接开始的画布与工具提示。加载过程显示正在加载；无效文件、缺图、保存失败和不支持的对象显示具体原因。键盘可以访问工具、属性、素材、链接和返回入口；白板快捷键只在白板获得焦点时生效。遵守减弱动态效果，不加入无关弹跳动画。

## 实现边界

| 位置 | 职责 |
| --- | --- |
| 新增 `internal/whiteboard/` | 场景与文件编解码、资源、几何、绑定、变换、路径生成、增量历史、素材和平台无关显示数据；不依赖 MyGo |
| `internal/editor/board*.go` 及所需资源子目录 | MyGo 绘制、输入/IME、工具状态、属性面板、剪贴板、缩略图、文档嵌入和 Mermaid 转换 |
| `internal/app/whiteboard*.go` 与现有文档入口 | 场景会话、异步加载/保存、文件对话框、图片资源、素材持久化、分屏及界面更新 |
| `internal/vault/`、`internal/history/`、`internal/trashbin/` | 复用文件边界、原子写入、快照和删除流程；只扩充新格式需要的识别与处理 |
| `internal/app/search.go`、`links.go`、`qacitations.go` 与 `internal/spacefs/` | 提供白板语义文字、双链和元素定位；复用现有索引与问答接口 |
| `internal/editor/markdown.go`、`htmlexport.go`、`vectorpdf.go` | Markdown 保留与嵌入，HTML/PDF/打印输出 |
| `third_party/`、`references/repository-exceptions.json` | 来源修订、许可正文、移植范围、字库/测试夹具等二进制的逐文件登记 |

白板功能以独立控制点接入现有代码，复用 FreeFolio 的当前接口。问答接口只扩充白板的读取和定位，不额外引入 AI 编辑白板。

阻塞的磁盘操作、图片解码、SVG/PNG 输出和大型场景处理放到后台；结果经 `win.Update` 更新界面。手势时间使用 `c.Now()` / `c.After()`；回归使用 `Tester.Advance`。不更改 module cache，不使用 MyGo 的富文本编辑器组件。

鼠标命中检查和橡皮擦复用按元素版本缓存的边界，先排除远处元素，再执行精确命中检查。没有选中元素时，不扫描场景生成空选择列表。

交互白板只解码视口及其外围 240 屏幕单位内的图片，每个白板的普通图片与 AI 预览合计最多同时解码两张。移出范围或删除后释放解码缓存；异步结果返回时，重新核对资源是否仍需要。原始附件与图片历史引用保留。导出和图片标注快照不使用交互视口筛选，仍加载全部有效图片。缩小到全图可见时，需要显示的图片仍可能较多，此限制不是整板内存上限。

## 分组背景与名称

组合后的元素显示低饱和度半透明圆角背景，亮色模式不透明度为 12%，深色模式为 14%。背景从成员的实际范围计算，四周保留 24 个画布单位。顶部为每层组合保留 28 像素的标题栏，名称使用 15 像素字号，任何缩放比例下都保持这一屏幕尺寸；画布单位下的标题栏高度随缩放换算。导出 SVG 没有缩放，仍按 28 个画布单位留白。嵌套组合逐层增加留白。背景跟随成员移动和调整大小，不新增独立图形，不挡住节点与连线操作。

单击标题或背景空白区域选择整组，拖动背景空白区域移动整组，一次拖动只记一次撤销。双击标题，或在右键菜单的“组合与锁定”中选择“重命名分组”，直接在标题原位置输入，不另开面板。标题获得键盘焦点后按 Enter 也可重命名；输入后按 Enter 或点击外部保存，Escape 取消。输入法候选文字不提前写入名称，一次重命名只记一次撤销。空名称显示“分组”。只读白板和包含锁定成员的组合不能重命名或整体拖动。

在“组合与锁定”中选择“编辑组内节点”，可分别选择、移动和操作成员，组合关系保留。点击“完成组内编辑”、按 Escape 或点击组外可退出。节点原有的锁定状态不会被组内编辑解除。

节点右键菜单按编辑、组合与锁定、排列与对齐、图层与翻转、图形与文字、复制与导出、AI 操作、画布与视图收纳。子菜单沿用系统的悬停展开和键盘导航。

名称写入场景的 `freefolioGroupNames` 扩展字段，以组合 ID 为键；旧白板无需迁移。重命名支持撤销和重做，复制粘贴时名称随新组合 ID 保留。取消组合移除名称及背景，SVG/PNG 导出保留背景和已命名标题。

## 许可和仓库边界

Quiet.md 和 Excalidraw 为 MIT。移植时一并核对并保留 rough.js、hachure-fill、points-on-curve、points-on-path、path-data-parser、perfect-freehand、fractional-indexing、nanoid 和图标的必要声明。字体分别遵守 OFL 1.1 或 MIT，不能把项目 MIT 当成全部资源的统一许可。

现有 Quiet.md 许可副本不能代替本次新增来源记录。每个实际采用的版本、文件和适配范围登记到第三方清单。字库与可复现二进制夹具在纳入前核对来源、许可、尺寸及 SHA-256，并逐文件加入例外清单。

一次性上游对照、截图、性能记录和下载放入已忽略的 `_temp/<任务>/`；可复用来源快照放 `.local/`。长期源码、测试、许可和设计文档进入仓库。本设计不授权自动安装、创建标签或发布安装包。

## 验证与完成条件

1. **文件往返。** 使用来源的全部元素夹具及 FreeFolio 自有夹具，验证载入、编辑、保存、重载后图形、图片、绑定、字体、画框和未知字段保留。检查 Excalidraw 标准 JSON、SVG/PNG 内嵌场景、剪贴板和素材库。不能仅验证 FreeFolio 能读取自己写出的文件。
2. **算法对照。** 移植 Quiet.md 已有的几何、绑定、文字、变换、历史和手绘路径测试。rough.js 对照采用其真实输出夹具及原有 `1e-6` 容差；只在 UI 绘制边界处理单精度差异。
3. **交互回归。** 用 `ui.NewTester` 覆盖 B02–B14 和 K01–K08 的真实操作，包含中文 IME、拖动与撤销、不同缩放、旋转图片、画框裁剪、跨白板粘贴、双链改名、附件迁移和关闭后重开。
4. **文件保护。** 覆盖未来版本、未知元素、畸形资源、缺图、路径越界、外部修改、保存失败和撤销后资源恢复；验证原文件没有被空文档或降级内容覆盖。
5. **跨应用交换。** 对同一标准夹具比较本实现与来源解析/序列化结果，并在可用的隔离环境验证 Excalidraw 打开、编辑和再次导入。未完成真实跨应用检查时单独标注，不把自测往返说成完全兼容。
6. **性能。** 复用 Quiet.md 的 200、1000、3000、10000 个基础图形场景及生成参数，记录实际元素数、可见数、窗口、缩放、平台和硬件。测量打开、概览/原比例平移、缩放、单元素/全选拖动、撤销和内存释放的 P50/P95。区分 CPU 构建与屏幕呈现；不引用另一平台的数据作为 Windows 结果。
7. **源码与仓库检查。** `gofmt -l .` 无输出；`CGO_ENABLED=0` 下运行 `go vet ./...`、`go test ./...` 和 Windows/Linux 构建。运行 `python -X utf8 tools/check_repo.py` 与 `python -X utf8 -m unittest discover -s tests/repository -v`。依赖变更同步第三方清单。
8. **本地交付。** 验证后运行 `python -X utf8 tools/build_test_installer.py`，核对安装包、同名 `.sha256` 和 `build-info.json`。截图及私有桌面检查遵循项目既有约定，未完成的目检如实注明。

实施顺序为：格式与兼容夹具 → 几何/绘图/增量历史 → 原生工具与完整交互 → Markdown/链接/素材/Mermaid → 交互和文件保护回归 → 性能与本地测试包。各阶段为实现依赖顺序，不缩减最终清单；B01–B15、K01–K08 全部完成并有对应证据后，才可标记全量采用完成。

## 依据

- [Quiet.md 白板设计、模块对应与已知缺口](https://github.com/s1dashu/quiet-md/blob/e59c49adc3eb0bf8dabaec2df7e7d64f5a97e00a/docs/design/whiteboard-excalidraw.md)
- [Quiet.md 白板工具与操作全集](https://github.com/s1dashu/quiet-md/blob/e59c49adc3eb0bf8dabaec2df7e7d64f5a97e00a/src/board/action.rs)
- [Quiet.md 字体和算法许可](https://github.com/s1dashu/quiet-md/blob/e59c49adc3eb0bf8dabaec2df7e7d64f5a97e00a/THIRD_PARTY.md)
- [Quiet.md 性能方法与测量边界](https://github.com/s1dashu/quiet-md/blob/e59c49adc3eb0bf8dabaec2df7e7d64f5a97e00a/tools/perf/board-results.md)
- [ArcMD 白板与 Markdown 集成](https://github.com/Ciao1019/ArcMD/blob/70711e1694699207400f751c01d28b11bdaefac5/internal/app/whiteboard.go)
- [ArcMD Mermaid 转换](https://github.com/Ciao1019/ArcMD/blob/70711e1694699207400f751c01d28b11bdaefac5/internal/editor/board_mermaid.go)
- [FreeFolio 架构](architecture.md)与[仓库边界](../references/repository-boundaries.md)
