# OneBot

OneBot 在原问答侧栏中提供对话、文档修改和方案审阅。文件工具原生移植自 MIT 的 [OpenCode](../third_party/opencode/README.md)，没有额外运行时。

拖动 OneBot 与编辑器之间的分隔线可以调整宽度，松开后自动保存。双击分隔线恢复默认宽度；分隔线获得焦点后，左右方向键可微调。窗口变窄时会暂时限制宽度，重新放大后恢复用户设置。

输入框始终固定在对话侧栏底部。新对话的标题、说明和示例问题在输入框上方的可用区域居中；窗口较矮时，可以滚动查看示例，输入框不会跟着移动。输入区从一行开始，随文字换行增长，超过六行后在框内滚动。按 Enter 发送，按 Shift+Enter 换行。只有添加附件或引用时才显示附件和引用列表；列表过长时可在列表内滚动，移除最后一项后不留空位。输入布局参考 [OpenCode 桌面输入框](https://github.com/anomalyco/opencode/blob/85951cb08614dfe6443905552cae49d59d2d6615/packages/app/src/composer/editor/editor.tsx)。

## 配置和切换模型

1. 打开「设置 → OneBot」，添加供应商。可以为同一服务添加多个独立连接。
2. 填写服务地址和 API Key，然后点击「获取模型」。
3. 搜索并勾选需要使用的模型。服务未列出的模型可以手动添加。
4. 在对话输入框底部的模型菜单中切换供应商和模型。

支持 OpenAI Chat Completions、OpenAI Responses、Anthropic Messages、Google Gemini、Ollama 和 OpenAI 兼容图像/视频协议。常用服务预设仅填写地址和协议，模型列表以实际接口返回为准。已有对话保留；正在运行的请求使用发送时的模型。

能力信息优先采用服务返回，再补充 models.dev 目录数据。能力详情保留来源，可手动校正。缺少数据时显示未知。输出上限、温度和推理参数在设置中调整，发送时校验模型能力。

API Key 单独保存在配置目录的 `credentials/ai-api-key`，不写入偏好或笔记。OneBot 从新配置开始，不复用旧单供应商设置和密钥。

## 图片和视频生成

OneBot 的连接也用于白板 AI 节点生成图片和视频。可以只为绘画添加独立供应商：

1. 在「设置 → OneBot → 常用服务」选择「OpenAI 绘画与视频」，或添加供应商后把接口协议设为「OpenAI 兼容图像/视频」。
2. 填写服务地址和 API Key。OpenAI 官方使用 `https://api.openai.com/v1`；new-api、one-api 等中转填写其 `/v1` 地址。
3. 点击「获取模型」，勾选绘画或视频模型，例如 `gpt-image-1`、`sora-2`。

使用 Gemini 生图时，选择「Google Gemini」常用服务，填写 API Key 后获取并勾选账号可用的图像模型，例如 `gemini-3-pro-image`。白板的「图像生成」节点和 OneBot 的生图工具共用该连接。白板可设置画幅比例、1K/2K/4K 分辨率和生成数量，也可连接图片输入节点作为参考图。具体分辨率支持以所选模型为准。若能力目录暂时不可用，可点击「更新模型能力」重试，或在模型能力中将输出模态设为 `text, image`。

Gemini 的能力信息复用 [models.dev](https://models.dev) 目录，包含其 `type: image` 记录。原生请求保留 `generateContent` 接口，对照 [Google Gen AI Go SDK v1.79.1](https://github.com/googleapis/go-genai/tree/c2b979819c3b57022841db6a1ac6057b38726268)（Apache-2.0）中的 `ImageConfig`、`responseModalities` 与 `inlineData` 标准字段，沿用现有 HTTP、密钥与附件保存实现。模型范围另核对 [Google 官方图像生成说明](https://ai.google.dev/gemini-api/docs/image-generation)（2026-11-25）；未引入 SDK 依赖或复制其源码。

| 协议 | 图片 | 视频 |
| --- | --- | --- |
| OpenAI 兼容图像/视频 | `/images/generations`；有图片输入时用 `/images/edits`（多图与遮罩） | `/videos` 提交任务、轮询并下载 |
| OpenAI Responses | 内置 `image_generation` 工具 | 输出模态含 video 时使用同地址 `/videos` |
| OpenAI Chat Completions | 模型输出模态含 image 时使用同地址 `/images/*`；OpenRouter 使用 `modalities` | 输出模态含 video 时使用 `/videos` |
| Google Gemini | 图像模型的原生图片输出；`imagen-*` 使用 `predict` | Veo 长任务轮询并下载 |

模型列表按能力筛选：图像/视频协议的模型默认可用；其他协议需要「输出模态」包含 `image` 或 `video`，可在模型能力中手动校正。图像/视频供应商不会出现在对话的模型菜单中。ChatGPT 套餐、Anthropic 和 Ollama 不提供图片或视频生成。

结果下载限制大小，只接受 https 地址（本机服务除外）；API Key 只发送给服务自身的主机。生成的文件保存为白板附件。

## 使用 ChatGPT 登录

1. 在「设置 → OneBot → 常用服务」选择「ChatGPT」。
2. 点击「使用 ChatGPT 继续」，在系统浏览器中完成登录和套餐授权。
3. 返回 FreeFolio，查看账号和套餐状态；登录成功后会自动获取当前账号的模型。
4. 勾选需要使用的模型，在 OneBot 输入框底部选择模型后发送消息。

可以添加多个 ChatGPT 连接，分别用于不同账号或工作区。给连接取不同名称后，通过原有供应商选择器切换。重新登录复用原有注册；切换账号请使用另一个连接。

只有允许套餐使用的账号才能通过此连接发送请求。仅完成登录时，点击「允许使用 ChatGPT 套餐」重新授权。「管理用量」打开 ChatGPT 用量设置，可查看套餐及此应用的限制。达到额度后不会自动改用其他供应商或 API Key。

ChatGPT 连接支持官方允许的流式对话、推理、图片和文件输入，以及 OneBot 本地文档工具；具体能力由所选模型决定。该通道不支持手动设置温度、输出上限或推理预算，也不提供音视频、图像生成、文件上传 API 或托管工具。模型以当前账号返回的列表为准。

「断开 ChatGPT 连接」停止该连接的请求，尝试撤销会话并清除本机令牌。账号注册保留，便于再次登录；远端撤销未确认时会提示你到 ChatGPT 设置中断开应用。「移除供应商」还会移除本机注册记录。

ChatGPT 凭据单独加密保存在配置目录的 `credentials/chatgpt-connections`，不会写入 API Key 文件、偏好或笔记。Windows 使用系统 DPAPI；macOS 使用 Keychain；Linux 需要可用且已解锁的 Secret Service 和 `secret-tool`。系统凭据服务不可用时会报错，不改用明文。技术边界与验证范围见 [ChatGPT 接入说明](onebot-chatgpt.md)。

## 只开放图片接口的供应商

在「设置 → OneBot」将接口协议设为「OpenAI 兼容图像/视频」，服务地址填供应商给出的 API 根地址（例如 `https://example.com/v1`），不要再加 `/responses` 或图片接口路径。若站点不开放模型列表，在「手动添加模型」输入站点提供的模型 ID。

- 没有参考图时使用 `/images/generations`；有参考图时使用 `/images/edits`。图片专用站点对 Responses 返回 403 不代表 Key 已失效，不会自动切换协议或账号绕过限制。
- 默认让服务决定返回格式；支持 URL 和 Base64。仅在服务明确以参数错误要求一种格式时，才用该格式再请求一次。要从第一次请求就指定 URL，在白板节点「全部设置 → 附加参数（JSON，可选）」中填 `{"response_format":"url"}`。
- 点击「测试生图（可能计费）」会用当前模型生成一张测试图，不保存结果。测试全部 Key 会分别请求；仅输出视频的模型只测试模型列表，不生成视频。
- 「清空选择」只取消勾选；「清空模型」确认后会删除该供应商的全部本地模型记录，包括搜索框未显示的记录。供应商、Key、其他供应商和已有笔记不变。

协议依据：[OpenAI Images API](https://developers.openai.com/api/reference/resources/images/methods/generate)；社区适配参考：[LiteLLM Images](https://github.com/BerriAI/litellm/tree/main/litellm/llms/openai/image_generation)。本地测试使用受控服务，不使用真实 Key。

## 修改内容

- 在编辑器选择文字，通过右键菜单「用 OneBot 修改」输入要求。原选区会保留，切换焦点不会改变应用位置。
- 在普通对话中明确要求修改当前文档，OneBot 使用文件工具准备草稿；单纯回答问题不会把回答内容写入正文。
- 输入框的「+」菜单可以选择当前章节、整篇文档、多个指定文档，或输入新文档路径。
- 要比较多个表达，选择「提供三个方案」，或在「更多编辑选项」中设置 1–5 个方案。

回答下方显示受影响文件和修改数量。点击摘要展开差异；可以应用或拒绝一处，也可以应用全部。多个方案之间切换不会修改原文。

「⋯ → 直接修改」启用后，单个方案完整生成且校验通过时自动应用。多个候选仍须选择。应用后可使用编辑器撤销，或在修改操作菜单中回退该方案。

「继续调整方案」基于当前候选继续改写。已经应用的方案会从最新文档重新捕获范围，保留之前的记录。

## 文件保护和恢复

`read / edit / write / patch` 先操作独立快照。工具支持精确替换、重复匹配检查、上下文补丁和多文件补丁。用户选择的范围决定可写内容；读取参考文档不授予修改权限。

应用时检查原文和外部文件变化，建立恢复点，再作为一次文档事务保存。生成被取消、被截断或补丁无效时，正文保持原样。原文冲突保留草稿，使用「基于最新原文重试」重新生成。

「已应用，等待保存」表示内存中的修改尚未保存成功；可以重试保存。多文件任务逐文件报告结果，失败后未开始的文件保持原样。编辑记录位于配置目录的 `onebot/tasks`，可通过「⋯ → 编辑记录」打开和恢复。

文件工具使用的路径仅在所选文档范围内生效。内容编辑不授权删除或移动整个文件。新建文件采用独占创建，已有同名文件会报错。

## 验证范围

自动测试使用本地受控模型响应和隔离笔记，覆盖协议、发现列表、切换连接、编辑工具、结构保留、候选应用、撤销、冲突和恢复。OpenCode 移植有 121 组上游对照用例。界面使用原生无窗口测试和截图检查。真实供应商的联网请求仍取决于用户账号、服务能力和具体模型，不以能力目录代替调用结果。
