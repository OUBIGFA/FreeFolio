# 本地模型选择与验证

核查日期：2026-11-27。选择条件是免 Token 下载、可由本机 ComfyUI 调用、适合 12 GB 显存级别的设备。语音允许自动安装固定版本插件。模型和插件均不随 FreeFolio 安装包分发。

| 类别 | 已接入 | 默认设置 | 选择依据 |
| --- | --- | --- | --- |
| 音乐与歌曲 | ACE-Step 1.5 Turbo 2B | 30 秒，8 步，支持歌词 | 官方硬件表支持 12–16 GB 设备使用 1.7B 语言模型；ComfyUI 提供原生分体工作流 |
| 音效与环境声 | Stable Audio 3 Medium | 10 秒，8 步 | 当前公开权重中的通用音频模型，ComfyUI 原生支持 |
| 语音 | Qwen3-TTS 1.7B CustomVoice | 中文、Vivian，可选九种音色 | 官方多语言语音权重，使用固定版本 ComfyUI-QwenTTS 插件 |
| 视频 | Wan 2.2 TI2V 5B | 832×480、24 帧/秒、4 秒、20 步 | 同时支持文字与首帧图片，ComfyUI 官方文档说明 8 GB 显存可配合原生卸载运行 |

这些是按运行条件筛选的选择，不是跨模型实测排名。显存建议不是实测峰值，也不保证在其他应用占用显存时能够运行。

来源：[ACE-Step 硬件说明](https://github.com/ace-step/ACE-Step-1.5/blob/main/docs/en/GPU_COMPATIBILITY.md)、[Stable Audio 模型比较](https://stability.ai/guides/stable-audio-3-model-comparison-guide)、[Qwen3-TTS 模型卡](https://huggingface.co/Qwen/Qwen3-TTS-12Hz-1.7B-CustomVoice)、[Wan 官方 ComfyUI 教程](https://docs.comfy.org/tutorials/video/wan/wan2_2)、[语音插件源码](https://github.com/1038lab/ComfyUI-QwenTTS/tree/d8122a8ba835b65fd65c113d2b273b1ad1579293)。精确文件、修订、大小、摘要和许可见[音视频资源清单](../third_party/manifests/media-resources.json)。

ACE-Step XL 4B 的显存需求更高，未作为此次默认包。LTX 2.5 的所查权重需要登录，不能满足免 Token 下载。FastH3 V2 的模型、32B 编码器及视频 VAE 体积较大，尚无本次 12 GB 设备的稳定运行证据，未进入下载菜单。Stable Audio Large 没有同等公开权重下载入口。以后增加这些候选需重新验证下载条件、运行路径和硬件需求。

## Qwen 2.1 LoRA

本次检索核对了 345 个去重的 Hugging Face 搜索结果及相关模型卡，不代表全站穷尽。只有有明确对应底模和可调用权重的候选进入菜单。

| 底模 | 已接入 LoRA | 约束 |
| --- | --- | --- |
| Turbo | Restore | 一张图片，不与 Consistency 同用 |
| Turbo | Consistency、Consistency 2000 | 至少一张图片，分别保留作者两个权重版本 |
| Turbo | FUSAL Anime Turbo E6 | 实验版，独立使用，另需 Heretic 编码器 |
| Turbo | Character Swap | 两张参考图，第一张原图，第二张目标角色 |
| Turbo | MosaicMate | 至少一张图片，用于半透明马赛克修复 |
| 普通版 | FUSAL Anime V3 | 独立使用，另需 Heretic 编码器 |
| 普通版 | WarmBlood Anime、WarmBlood Real | 实验版，未将普通版权重标为 Turbo |

Outfit Swap 的示例使用 Viggle Turbo；Next Scene、Multiple Angles、Natural Exposure、Outpaint 和 QMA 等候选缺少所选官方 Turbo 的直接兼容证据，因此未当作 Turbo LoRA 接入。旧 Qwen Detail Enhancer 按需求移除。对应来源在[增强资源清单](../third_party/manifests/enhancements.json)中逐项记录。

## 验证边界

已核对匿名下载状态、文件大小和发布方摘要，检查固定插件 ZIP 路径与许可。原生图按 ComfyUI 0.46.2 的实际节点输入核对，缺失模型文件单独记录。自动测试覆盖生成图、输入限制、首帧上传、结果附件、歌词连线、音频播放、按底模分组、断点下载和插件安装失败。

语音插件已在隔离 Python 环境中完成依赖安装与实际加载器导入检查。该检查复用当前 ComfyUI 已有依赖，未修改用户运行中的服务。未下载本次新增的大型权重，也未在实际 GPU 上完成各模型生成；画质、耗时和显存峰值未实测。

语音安装会固定现有 Python 包版本；发生版本冲突时显示失败，不自动替换 PyTorch。移除语音资源会校验并回收插件源码和模型，保留可能被其他节点使用的 Python 依赖与缓存。更改资源目录后从已验证 ZIP 重建插件，无需再次安装依赖。
