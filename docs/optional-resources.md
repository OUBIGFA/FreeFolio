# 可选资源与白板抠图

在「设置 → 可选资源」选保存位置，再点模型旁的「下载」。第一次下载模型时会同时下载共用的 CPU 运行库。不需要 Python、显卡或 OneBot Key；安装和打开 FreeFolio 不会自动下载。

在白板的 AI 节点菜单添加「本地抠图」，选择已下载的模型，将一张图片接到「输入图片」，再点「运行」。结果是保留原尺寸和原透明度的 PNG 附件，不覆盖原图。可以另存或接到下游图像节点。模型推理在本机独立进程内执行，可以停止。

| 可选模型 | 权重下载 | 推理输入 | 建议用途 |
| --- | ---: | ---: | --- |
| [BEN2](https://huggingface.co/onnx-community/BEN2-ONNX/tree/c552aa82688edce09f0ac9d2e31ad53d9d629010) | 209.0 MiB | 1024×1024 | 通用物体与人像发丝均衡，Confidence Guided Matting 两阶段精修边缘，推荐优先试用 |
| [BiRefNet HR Matting](https://huggingface.co/PinkPixel/birefnet-hr-matting-onnx/tree/792518b9d4dfe9793891712677de25e554f948c6) | 449.5 MiB | 2048×2048 | 2K 高清精细 Matting 权重，发丝、毛发与半透明边缘细节更强，占用更多时间与内存 |

两者均采用 MIT 许可与 FP16 权重。它们是通用前景分离与精细 Alpha 抠图模型，不是基于点击或文字描述的任意对象选择器。

**处理管线。** 输入图片先经 Catmull-Rom 缩放到模型输入分辨率并按 ImageNet 均值方差归一化；模型输出根据权重类型转为 `[0, 1]` 透明度（`BiRefNet HR Matting` 为已激活的 Alpha 概率，`BEN2` 自动区分 logits 与概率，避免重复 sigmoid 导致灰底）并双线性缩回原尺寸；随后在半透明过渡区域执行前景色去色溢（估算邻近纯前景像素颜色替换背景残留色边），最后乘以原图透明度输出 PNG。单张输入最多 64 MiB、32 兆像素。

**依据与限制。** 参照 [BEN2 原项目](https://github.com/PramaLLC/BEN2)、[BiRefNet 原项目](https://github.com/ZhengPeng7/BiRefNet) 与上表两份精确修订的 ONNX 模型卡。[BRIA RMBG 2.0](https://huggingface.co/briaai/RMBG-2.0) 与 [BRIA RMBG 3.0](https://huggingface.co/briaai/RMBG-3.0) 为非商业许可（且 RMBG 3.0 依赖 PyTorch/CUDA 与受限访问仓库），不作为纯 Go 本地 MIT 可选资源分发。

**运行库。** 两款模型共用 [Microsoft ONNX Runtime 1.28.0](https://pypi.org/project/onnxruntime/1.28.0/#files) 官方 CPU 包。Windows x64 下载 13.1 MiB；Linux x64（glibc 2.27+）和 macOS 14+ Apple Silicon 各约 18.3 MiB。保留完整的官方包和其中的 LICENSE、ThirdPartyNotices；仅加载核对后的本平台运行库，不安装 Python 包。其他平台不提供该下载。

**位置和删除。** 切换目录只更改当前资源位置，旧文件不移动。要管理旧文件，选回原目录。下载和使用前核对 SHA-256。删除时核对文件内容并移到系统回收站；不递归删除目录，不动旧笔记、已生成图片或其他文件。同名文件内容不符时会报错，不覆盖或删除。单独移除模型不影响另一款模型；删除共用运行库后，两款模型都要重新下载运行库才能使用。
