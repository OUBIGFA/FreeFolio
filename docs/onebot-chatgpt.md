# OneBot 的 ChatGPT 连接

## 接入边界

通过 OpenAI 公布的 Sign in with ChatGPT 协议，为 OneBot 增加独立的 ChatGPT 供应商。继续使用原生 Go + MyGo 界面、现有对话及文档草稿流程。登录在系统浏览器中完成。

实现依据是公开协议，不复制或翻译 DevKit 的非商业许可代码，不引入其 Node.js、React 或 Electron 运行时。直接嵌入 SDK 会改变运行时和许可边界；原生协议实现可以复用 OneBot 现有 Responses 客户端。FreeFolio 的 MIT 代码许可不因此改变。登录图标使用官方原样 SVG，单独遵守 OpenAI 品牌规则，来源见 [品牌与协议记录](../third_party/chatgpt/README.md)。服务使用资格仍由 OpenAI 决定。

每个 ChatGPT 供应商连接对应一个独立的账号或工作区注册。已有供应商选择器负责切换，名称可自定义，已验证的账号信息显示在设置中。新登录未验证前不能替换现有凭据。

## 登录与存储

- 首次注册使用 `dynamic_agent_client`，固定应用名 `FreeFolio`；以后复用服务器签发的 client ID。
- 每次登录使用新的 state、nonce 和 PKCE；回调仅监听 IPv4 本机地址，固定 `/auth/callback`，端口由系统选择。
- 每次授权都发送持久化的、不含个人信息的 host ID。
- 使用 OpenID Connect 库验证签名、issuer、audience、有效期，再验证 nonce、subject 和授权方。
- 登录、取消、重新授权套餐使用、续期、断开与重新连接均保留独立状态。
- 凭据单独保存在配置目录的 `credentials/chatgpt-connections`，操作系统加密，无明文后备方案。Windows 使用 DPAPI；macOS 使用 Keychain 保存加密密钥；Linux 使用 Secret Service 的 `secret-tool` 保存加密密钥。
- 文件原子替换；进程间锁保护注册及旋转令牌。收到新令牌后先加密保存待验证状态，再验证身份，防止签名服务暂时故障时丢失已旋转令牌。
- 退出停止该连接的请求，尝试撤销刷新令牌，清除本地令牌并保留注册。远端撤销未确认时明确提示。

## 模型与生成

- 模型列表来自当前账号的 `GET https://api.openai.com/v1/models`，使用 `models`、`slug`、`display_name`、`visibility` 字段，保留服务顺序。
- 请求只发往官方 Responses 端点，固定 `store:false`、`stream:true`，发送完整所需上下文。
- OneBot 本地文件工具按官方要求放入 namespace；保留工具结果、推理内容、附件及文档应用前的检查。
- 不发送该通道禁止的温度、输出上限等参数；这些设置在 ChatGPT 连接中隐藏。模型能力未提供时保持未知。
- 只有 `response.completed` 才视为成功；截断、未完成、流内错误和额度错误均保留失败状态。
- 无套餐使用权限时保留登录，但禁止推理，提供明确的重新授权操作。
- 设置与输入区显示套餐使用状态和「管理用量」，链接到 ChatGPT 用量页。首次启用套餐使用时显示一次说明。
- 不读取 ChatGPT 历史聊天，不实现官方当前不支持的音视频、图像生成、托管工具或后台推理。

## 实施与核查

1. 在 `internal/chatgpt` 实现并测试登录、身份验证、加密存储、续期、取消和撤销。测试使用本地签名服务与合成令牌。
2. 在 `internal/agent` 增加 ChatGPT 通道、账号令牌来源、专用模型发现及请求约束。用本地 HTTP 服务验证工具往返、错误详情和流完成条件。
3. 在 `internal/app` 接入账号状态、供应商设置、输入区用量入口和首次提示。用 `ui.NewTester` 验证交互；偏好格式升级并保留原连接。
4. 更新用户文档、依赖许可清单和项目本地约定。运行仓库检查、格式检查、全量 Go 测试与 vet、Windows/Linux 构建，并生成本地 Windows 测试安装包。

重点检查：取消后迟到的回调；切换账号后迟到的模型列表；刷新轮换期间签名服务不可用；撤销失败后的本地退出；流开始后的额度错误不能应用文档草稿。

真实账号登录、套餐资格、真实额度和 macOS/Linux 密钥服务需在相应用户环境中验收。自动测试不代替这些检查。

## 官方依据

核对日期：2026-11-23。

- [DevKit 与许可证](https://github.com/openai/sign-in-with-chatgpt-devkit)
- [注册与登录](https://developers.openai.com/siwc/token-sharing-open-source/sign-in)
- [账号与会话](https://developers.openai.com/siwc/token-sharing-open-source/profiles-and-sessions)
- [模型与推理](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference)
- [当前限制](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations)
- [错误恢复](https://developers.openai.com/siwc/token-sharing-open-source/errors-and-recovery)
- [界面指引](https://developers.openai.com/siwc/ui-ux-guidelines)
