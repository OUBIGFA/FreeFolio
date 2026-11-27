# 可选资源与白板图片处理

在「设置 → 可选资源」选保存位置，再点资源旁的「下载」。抠图模型会附带共用运行库；Real-ESRGAN Vulkan 包含引擎和两款模型。这些资源无需另装 Python；ComfyUI 资源需先配置本机引擎。安装和打开 FreeFolio 不会自动下载资源。

「硬件与保存位置」位于页面顶部，由六个标签页共用：

- **本地模型**：无需 ComfyUI 的抠图模型、运行库和 Real-ESRGAN Vulkan。
- **ComfyUI 模型**：引擎连接、生图底模和 ComfyUI 抠图模型。
- **ComfyUI 增强**：SeedVR2 7B Sharp / 3B INT8 与 ComfyUI 原生 Real-ESRGAN Anime 6B。
- **ComfyUI LoRA**：按 Z-Image Turbo、Qwen 2.1 Turbo、Qwen 2.1 普通版分组的附加权重。下载会补齐底模，已有且校验一致的文件会复用。
- **音频模型**：ACE-Step 1.5 音乐、Stable Audio 3 音效、Qwen3-TTS 语音。
- **视频模型**：Wan 2.2 TI2V 5B，支持文字生视频和首帧图生视频

下载行显示进度条、百分比和已下载大小。点击「暂停」保留当前任务，点击「继续下载」恢复；点击「停止」结束当前任务，已下载内容仍保留。下次点击「下载」时继续，包括重新打开应用后。服务端需支持 HTTP Range；若不支持，将重下当前文件。所有权重在完整下载并通过 SHA-256 校验后才可使用。断点文件保存在资源目录内，不会被当作已安装模型。

LoRA 的体积只计算自身权重，不包含底模；下载时分别显示 LoRA 和底模的进度。SeedVR2 的模型权重与共用 VAE 也分别显示，避免把依赖体积计入模型本身。

选择资源后，在白板 AI 菜单的「LoRA 流程（自动连线）」中选择 LoRA。FreeFolio 会以当前视图中央为放置位置，添加并连接「加载底模 → 加载 LoRA → 生成图像／图片增强」，不改变缩放和视角。未下载的资源标为「未下载」。新增流程使用 ComfyUI 原生节点，不需要第三方插件。

例如下载 **Z-Image Detailer** 后，在侧边栏搜索并点击它。流程已连接，只需在右侧生成节点填写提示词，然后点击「运行」。也可手动添加加载底模、加载 LoRA、生成图像节点，将紫色「模型」输出依次连接；文字节点连到生成节点的「提示词」，而不是 LoRA。多个兼容 LoRA 可以串联，执行顺序与连线相同。

Qwen 图片修复流程还需将图片连到最后一个节点的「输入图片」。SeedVR2 和 Real-ESRGAN 是图片处理模型，图片直接连到放大节点，不接 LoRA 的模型线。模型下拉项用绿点表示非 ComfyUI 本地模型、紫点表示 ComfyUI 模型、蓝点表示供应商模型，同时保留文字来源。

## 增强与 LoRA 节点

| 用途 | 资源 | 对应节点 |
| --- | --- | --- |
| 细节增强 | Z-Image Detailer | 图像生成，Z-Image Turbo |
| 人物与写实 | V1 Realism | 图像生成，Z-Image Turbo |
| 图片修复 | Qwen 2.1 Restore | 图片增强，接入一张图片 |
| 3D | 3D Render Style Redmond | 图像生成，Z-Image Turbo |
| 电商样机 | Mockups Redmond | 图像生成，Z-Image Turbo |
| 插画 | Elusarca Anime | 图像生成，Z-Image Turbo |
| 手绘 | Pencil Sketch | 图像生成，Z-Image Turbo |
| 线稿 | ColoringBook Redmond | 图像生成，Z-Image Turbo |
| 绘本 | Linex Storybook | 图像生成，Z-Image Turbo |
| 漫画 | LineartAnime Redmond | 图像生成，Z-Image Turbo |
| 简笔 | Doodle Redmond | 图像生成，Z-Image Turbo |
| 图标 | Icons Redmond | 图像生成，Z-Image Turbo |
| 像素画 | PixelArt Redmond | 图像生成，Z-Image Turbo |
| 等距插画 | Isometric Redmond | 图像生成，Z-Image Turbo |

- 每个 LoRA 节点使用一款附加权重，卡片与属性面板均可调整「LoRA 强度」。0 表示关闭；切换模型时重置为该模型的初始强度。触发词在运行时自动加入，不改写用户提示词。底模与 LoRA 不匹配会报错，不静默跳过。以前保存的单节点预设仍可运行。
- 「图片增强」按原图比例生成约 2 MP 的新图片，长宽对齐至 32 像素。提示词可编辑或连接文字节点；未填写时使用该资源的默认增强说明。结果保留历史，可进行 A/B 对比、撤销和下游连接。
- 放大节点选择 3B 或 7B 后使用对应 SeedVR2 权重，不自动换用另一款。删除其中一款时，保留另一款仍在使用的共用 VAE。
- 供应商生图结果和导入图片可接到「图片增强」或「图片放大」。LoRA 只加载到匹配的本地生图模型，不会发送给供应商接口。
- 这些模型会生成新细节，人物身份、商品文字和线条应通过原图对比核查。像素画和简笔画可将细节增强关闭，保留原有轮廓。

固定下载地址、大小、SHA-256 与许可来源见[增强资源清单](../third_party/manifests/enhancements.json)。下载不需要 Hugging Face Token；模型不随安装包分发。网络与本机 ComfyUI 环境仍须可用。

验证范围：自动测试覆盖原生工作流提交、模型与 LoRA 选择、强度、缺失资源拒绝、资源下载复用与回收、节点交互与界面布局。每款权重在不同显卡上的画质、耗时和显存峰值需本机运行确认。

## 本地抠图

在白板的 AI 节点菜单添加「本地抠图」，选择已下载的模型，将一张图片接到「输入图片」，再点「运行」。结果是保留原尺寸和原透明度的 PNG 附件，不覆盖原图。可以另存或接到下游图像或对比节点。模型推理在本机独立进程内执行，可以停止。

| 可选模型 | 权重下载 | 推理输入 | 建议用途 |
| --- | ---: | ---: | --- |
| [BEN2](https://huggingface.co/onnx-community/BEN2-ONNX/tree/c552aa82688edce09f0ac9d2e31ad53d9d629010) | 209.0 MiB | 1024×1024 | 通用物体与人像发丝均衡，Confidence Guided Matting 两阶段精修边缘，推荐优先试用 |
| [BiRefNet HR Matting](https://huggingface.co/PinkPixel/birefnet-hr-matting-onnx/tree/792518b9d4dfe9793891712677de25e554f948c6) | 449.5 MiB | 2048×2048 | 2K 高清精细 Matting 权重，发丝、毛发与半透明边缘细节更强，占用更多时间与内存 |

两者均采用 MIT 许可与 FP16 权重。它们是通用前景分离与精细 Alpha 抠图模型，不是基于点击或文字描述的任意对象选择器。

**处理管线。** 输入图片先经 Catmull-Rom 缩放到模型输入分辨率并按 ImageNet 均值方差归一化；模型输出根据权重类型转为 `[0, 1]` 透明度（`BiRefNet HR Matting` 为已激活的 Alpha 概率，`BEN2` 自动区分 logits 与概率，避免重复 sigmoid 导致灰底）并双线性缩回原尺寸；随后在半透明过渡区域执行前景色去色溢（估算邻近纯前景像素颜色替换背景残留色边），最后乘以原图透明度输出 PNG。单张输入最多 64 MiB、32 兆像素。

**运行库。** 两款抠图模型共用 [Microsoft ONNX Runtime 1.28.0](https://pypi.org/project/onnxruntime/1.28.0/#files) 官方 CPU 包。Windows x64 下载 13.1 MiB；Linux x64（glibc 2.27+）和 macOS 14+ Apple Silicon 各约 18.3 MiB。保留完整的官方包和其中的 LICENSE、ThirdPartyNotices；仅加载核对后的本平台运行库，不安装 Python 包。其他平台不提供该下载。

## 本地图片放大

在白板的 AI 节点菜单添加「图片放大」，选择模型与放大倍数（`2×` 或 `4×`），将一张图片接到「输入图片」，再点「运行」。底层通过本机独立进程调用 [Real-ESRGAN ncnn-vulkan v0.2.5.0](https://github.com/xinntao/Real-ESRGAN/releases/tag/v0.2.5.0)（支持 NVIDIA / AMD / Intel 显卡及 Apple Silicon Vulkan/MoltenVK 加速），输出无损 PNG 附件，可接到下游「图片对比」或「图像生成」节点。

| 模型 | 适用场景 | 许可 |
| --- | --- | --- |
| `realesrgan-x4plus` | 通用照片、自然画面、商品与复杂纹理 | BSD-3-Clause |
| `realesrgan-x4plus-anime` | 动漫、二次元插画、线稿与扁平色块 | BSD-3-Clause |

引擎包在 Windows x64 为 43.4 MiB，Linux x64 为 44.8 MiB，macOS 为 49.4 MiB；选择 `2×` 时先以 4× 推理再用 Catmull-Rom 超采样缩回 2× 尺寸。单张放大输出上限为 64 兆像素、长边不超过 16384 像素。

## ComfyUI 引擎与专业 GPU 模型（BRIA RMBG-3.0 FP8 / Sa2VA）

如果本机已安装独立显卡（例如 NVIDIA 8 GB+ / 12 GB 显存）和 ComfyUI（便携版、桌面版或源码虚拟环境），可在「设置 → 可选资源 → ComfyUI 引擎（GPU 加速）」按两步开启：

1. **连接 ComfyUI**：指定本地 ComfyUI 目录（便携版、源码虚拟环境、ComfyUI Desktop 安装目录或数据目录均可，或填写已运行的服务地址），点「检测并启动」。FreeFolio 不内置也不复制 PyTorch/CUDA，仅在需要时于本机回环地址静默拉起，并在退出时自动结束子进程；同时通过 `--extra-model-paths-config` 挂载资源目录下的 `comfyui/`（包含 `diffusion_models`、`text_encoders`、`vae`、`upscale_models`、`checkpoints`、`rmbg`、`sa2va`、`vitmatte`）并保留原有桌面端模型配置，不污染原 ComfyUI 目录。
2. **下载 GPU 模型（支持断点下载，完成后校验）**：

| 模型包 | 适用显卡 | 下载位置（资源目录内） | 用途与说明 | 许可 |
| --- | --- | --- | --- | --- |
| **BRIA RMBG-3.0 (FP8)** | 8 GB+ 显存（12 GB 推荐） | `comfyui/diffusion_models/RMBG-3.0-fp8.safetensors`、`text_encoders/qwen3vl_4b_int8_convrot.safetensors`、`vae/flux2-vae.safetensors` | 生成级柔和透明抠图，发丝、玻璃、婚纱与半透明物体效果最佳；通过 ComfyUI ≥ 0.4.60 原生节点运行，无需额外插件 | CC-BY-NC-4.0 |
| **Sa2VA-InternVL3-2B** | 6 GB+ 显存（推荐） | `comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B/` | 按自然语言指代抠出特定对象（如「左边穿白衣服的人」），轻量快速；需安装 `ComfyUI-Sa2VA-XJ` 或 `ComfyUI-Sa2VA` 插件 | Apache-2.0 |
| **Sa2VA-Qwen3-VL-4B** | 10 GB+ 显存（≤16 GB 自动 8-bit 量化） | `comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B/` | 复杂场景指代理解精度更高，12 GB 显卡自动启用 8-bit 量化避免爆显存；需安装 `ComfyUI-Sa2VA-XJ` 或 `ComfyUI-Sa2VA` 插件 | Apache-2.0 |
| **SeedVR2 7B Sharp (INT8)** | 10 GB+ 显存（12 GB + 大内存推荐） | `comfyui/diffusion_models/seedvr2_7b_sharp_int8_convrot.safetensors`、`vae/seedvr2_ema_vae_fp16.safetensors` | 一步扩散重绘级 2×/4× 图片放大，重建毛发、皮肤、织物与文字细节并保留 Alpha 透明通道，分块 VAE 防爆显存；ComfyUI 原生节点无需插件 | Apache-2.0 |
| **Z-Image-Turbo (INT8)** | 8 GB+ 显存（推荐） | `comfyui/diffusion_models/z_image_turbo_int8_convrot.safetensors`、`text_encoders/qwen_3_4b_fp8_mixed.safetensors`、`vae/ae.safetensors` | 8 步极速文生图，中英文语义准确、轻量全驻显存；ComfyUI 原生节点无需插件，供白板「图像生成」使用 | Apache-2.0 |
| **Qwen-Image 2.1 Turbo (INT8)** | 10 GB+ 显存 | `comfyui/diffusion_models/qwen_image_2.1_turbo_int8_convrot.safetensors`、`text_encoders/qwen3vl_8b_int8_convrot.safetensors`、`vae/qwen_image_2.1_vae_bf16.safetensors` | 文生图 + 多参考图/红圈标注改图一体，中文文字排版与复杂指令理解极强；ComfyUI 原生节点无需插件 | Qwen |

白板 AI 工作流节点支持：

- 节点和属性面板的模型选项以「本地模型」或「供应商模型」区分来源，ComfyUI 属于本地模型。
- 「**本地抠图**」节点可选 **BRIA RMBG-3.0 (FP8)** 与 **Sa2VA**，并支持通过「抠图说明」输入框或文字连线提示主体材质与指代对象。
- **BEN2、BiRefNet 与通用 RMBG** 不接收提示词，抠图说明和对应输入槽位置灰并禁止连接。切换模型时保留原文字和连线；禁用期间连线不参与运行，也不触发上游节点，切回支持提示词的模型后恢复。
- 「**图片放大**」节点可选 **SeedVR2 7B Sharp (INT8)**（`2×` / `4×` 重绘超分），与极速的 `Real-ESRGAN Vulkan` 互补。
- 「**生成图像 / 编辑图像**」节点可在模型列表直接选用本机 ComfyUI 的 **Z-Image-Turbo (INT8)**（文生图）与 **Qwen-Image 2.1 Turbo (INT8)**（自动切换文生图或参考图/红圈标注改图），保留完整的批量出图（`×1..×4`）、历史对比与 A/B 挑选体验。

此外，探测 `/object_info` 后也会继续列出 ComfyUI 中已有的通用放大模型（`UpscaleModelLoader`）与 `RMBG` 自定义节点模型。

## 依据、位置与删除

- **依据与限制。** 参照 [BEN2](https://github.com/PramaLLC/BEN2)、[BiRefNet](https://github.com/ZhengPeng7/BiRefNet) 与 [Real-ESRGAN](https://github.com/xinntao/Real-ESRGAN) 原项目及对应模型卡。[BRIA RMBG 2.0/3.0](https://huggingface.co/briaai/RMBG-3.0) 与 `4x-UltraSharp` 为非商业许可，`upscayl-ncnn` 为 AGPL-3.0，均不作为默认本地包分发。
- **位置和删除。** 更改保存位置时会自动将当前目录中已下载的本地模型、运行库和 `comfyui/` 模型包（含重建挂载配置）转移到新目录，不影响其他文件或旧笔记。下载和使用前核对 SHA-256。删除时核对文件内容并移到系统回收站；不递归删除目录，不动旧笔记、已生成图片或其他文件。同名文件内容不符时会报错，不覆盖或删除。单独移除抠图模型不影响另一款模型或图片放大资源；删除共用 ONNX 运行库后，两款抠图模型都需要重新下载运行库才能使用。

## 音频与视频

在资源页下载对应模型后，在白板添加「音频生成」或「视频生成」节点。音频节点模型下拉框显示三款本地模型，视频节点同时保留已有供应商模型。

- 音频节点填写提示词。ACE-Step 还可在「歌词」输入框填写歌词，或将文字节点连接到歌词口。连接歌词后，输入框置灰，行为与 ComfyUI 的已连接控件一致。
- Qwen3-TTS 使用「音色」和「语言」设置。先选择本机 ComfyUI 目录并清空服务地址，由 FreeFolio 启动引擎。首次下载会安装固定版本的 ComfyUI-QwenTTS 节点和依赖。安装使用所选 ComfyUI 的独立 Python 环境，不替换 CUDA PyTorch。
- 视频节点默认生成 832×480、24 帧/秒、4 秒视频。可将一张图片连接到「首帧」。
- 运行结果保存为白板附件。音频使用 FLAC，视频使用 MP4。节点内可播放或打开结果文件。

这些设置按当前硬件给出保守默认值。模型下载和本机推理的显存峰值、速度与画质仍需在本机实际运行确认。

## Qwen 2.1 底模与 LoRA

普通版使用 25 步采样，Turbo 使用 8 步采样，模型与 LoRA 不跨版本混用。当前接入 Turbo 的 Restore、Consistency、Consistency 2000、FUSAL Anime Turbo E6、Character Swap、MosaicMate；普通版接入 FUSAL Anime V3、WarmBlood Anime 与 WarmBlood Real。旧 Detail Enhancer 已从菜单移除，旧白板选择它时会提示重新选择。

Character Swap 将原图连到参考图 1，目标角色图连到参考图 2。Consistency、MosaicMate 至少需要一张参考图。FUSAL 会额外下载对应文本编码器，显示体积不计入 LoRA 本身；FUSAL 必须单独使用，Restore 与 Consistency 也不能叠加。Turbo E6 与 WarmBlood 项目标为实验版。

模型的可调参数、默认值和运行范围见[白板模型参数](ai-model-parameters.md)。选中节点，点击「全部设置」后调整。

本机 ComfyUI 服务地址无法连接时，FreeFolio 会尝试从已选目录或 ComfyUI Desktop 登记信息启动引擎；没有可用安装时提示选择目录。远程服务地址需由远程主机启动。
