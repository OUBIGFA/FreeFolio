# 可选资源与白板抠图 / 图片放大

在「设置 → 可选资源」选保存位置，再点对应资源旁的「下载」。第一次下载抠图模型时会同时下载共用的 CPU 运行库；图片放大包已内置 Vulkan 引擎与两款模型，一次下载即可使用。不需要 Python 或 OneBot Key；安装和打开 FreeFolio 不会自动下载。

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

1. **连接 ComfyUI**：指定本地 ComfyUI 目录（或填写已运行的服务地址），点「检测并启动」。FreeFolio 不内置也不复制 PyTorch/CUDA，仅在需要时于本机回环地址静默拉起，并在退出时自动结束子进程；同时通过 `--extra-model-paths-config` 挂载资源目录下的 `comfyui/`（包含 `diffusion_models`、`text_encoders`、`vae`、`upscale_models`、`checkpoints`、`rmbg`、`sa2va`、`vitmatte`），不污染原 ComfyUI 目录。
2. **下载 GPU 专业模型（断点可续、SHA-256 校验）**：

| 模型包 | 适用显卡 | 下载位置（资源目录内） | 用途与说明 | 许可 |
| --- | --- | --- | --- | --- |
| **BRIA RMBG-3.0 (FP8)** | 8 GB+ 显存（12 GB 推荐） | `comfyui/diffusion_models/RMBG-3.0-fp8.safetensors`、`text_encoders/qwen3vl_4b_int8_convrot.safetensors`、`vae/flux2-vae.safetensors` | 生成级柔和透明抠图，发丝、玻璃、婚纱与半透明物体效果最佳；通过 ComfyUI ≥ 0.4.60 原生节点运行，无需额外插件 | CC-BY-NC-4.0 |
| **Sa2VA-InternVL3-2B** | 6 GB+ 显存（推荐） | `comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B/` | 按自然语言指代抠出特定对象（如「左边穿白衣服的人」），轻量快速；需安装 `ComfyUI-Sa2VA-XJ` 或 `ComfyUI-Sa2VA` 插件 | Apache-2.0 |
| **Sa2VA-Qwen3-VL-4B** | 10 GB+ 显存（≤16 GB 自动 8-bit 量化） | `comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B/` | 复杂场景指代理解精度更高，12 GB 显卡自动启用 8-bit 量化避免爆显存；需安装 `ComfyUI-Sa2VA-XJ` 或 `ComfyUI-Sa2VA` 插件 | Apache-2.0 |

白板「本地抠图」节点新增了可选的「抠图说明」输入口与文本栏：使用 **BRIA RMBG-3.0 (FP8)** 时可提示主体材质（如 `透明玻璃杯`），使用 **Sa2VA** 时可直接描述要抠出的具体对象；留空时均默认自动识别主体。

此外，探测 `/object_info` 后也会继续列出 ComfyUI 中已有的通用放大模型（`UpscaleModelLoader`）与 `RMBG` 自定义节点模型。

## 依据、位置与删除

- **依据与限制。** 参照 [BEN2](https://github.com/PramaLLC/BEN2)、[BiRefNet](https://github.com/ZhengPeng7/BiRefNet) 与 [Real-ESRGAN](https://github.com/xinntao/Real-ESRGAN) 原项目及对应模型卡。[BRIA RMBG 2.0/3.0](https://huggingface.co/briaai/RMBG-3.0) 与 `4x-UltraSharp` 为非商业许可，`upscayl-ncnn` 为 AGPL-3.0，均不作为默认本地包分发。
- **位置和删除。** 切换目录只更改当前资源位置，旧文件不移动。要管理旧文件，选回原目录。下载和使用前核对 SHA-256。删除时核对文件内容并移到系统回收站；不递归删除目录，不动旧笔记、已生成图片或其他文件。同名文件内容不符时会报错，不覆盖或删除。单独移除抠图模型不影响另一款模型或图片放大资源；删除共用 ONNX 运行库后，两款抠图模型都需要重新下载运行库才能使用。
