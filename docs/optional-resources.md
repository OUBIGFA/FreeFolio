# 可选资源与白板抠图

在「设置 → 可选资源」选保存位置，再点模型旁的「下载」。第一次下载模型时会同时下载共用的 CPU 运行库。不需要 Python、显卡或 OneBot Key；安装和打开 FreeFolio 不会自动下载。

在白板的 AI 节点菜单添加「本地抠图」，选择已下载的模型，将一张图片接到「输入图片」，再点「运行」。结果是保留原尺寸和原透明度的 PNG 附件，不覆盖原图。可以另存或接到下游图像节点。模型推理在本机独立进程内执行，可以停止。

| 可选模型 | 权重下载 | 建议用途 |
| --- | ---: | --- |
| [BiRefNet Lite](https://huggingface.co/onnx-community/BiRefNet_lite-ONNX/tree/de15b22ba131738a16dff04aab8bdf8dc32e3ac1) | 109.2 MiB | 希望减少下载量和内存占用时先试用 |
| [BiRefNet General](https://huggingface.co/onnx-community/BiRefNet-ONNX/tree/534d3c82d3bb8b2f0867db6dfbc3a525b8e42f67) | 467.0 MiB | 可以接受更高内存占用时，用同一张图片比较轮廓细节 |

两者均采用 MIT 许可、FP16 权重和 1024×1024 推理输入。它们是通用前景分离模型，不是基于点击的任意对象选择器。模型输出经 sigmoid 转为透明度，缩回原尺寸，再乘以原图透明度。单张输入最多 64 MiB、32 兆像素。

**依据与限制。** 参照 [BiRefNet 原项目](https://github.com/ZhengPeng7/BiRefNet)、[rembg 的轻量版适配](https://github.com/danielgatis/rembg/blob/v2.0.88/rembg/sessions/birefnet_general_lite.py) 和上表两份精确版本的 ONNX 模型卡。U²-NetP 权重约 4.4 MiB，较小但不是较新的通用方案；[BRIA RMBG 2.0](https://huggingface.co/briaai/RMBG-2.0) 的权重使用条件不同，不作为 MIT 模型分发。没有用统一真实图片集验证哪一款效果最佳。

**运行库。** 两款模型共用 [Microsoft ONNX Runtime 1.28.0](https://pypi.org/project/onnxruntime/1.28.0/#files) 官方 CPU 包。Windows x64 下载 13.1 MiB；Linux x64（glibc 2.27+）和 macOS 14+ Apple Silicon 各约 18.3 MiB。保留完整的官方包和其中的 LICENSE、ThirdPartyNotices；仅加载核对后的本平台运行库，不安装 Python 包。其他平台不提供该下载。

**位置和删除。** 切换目录只更改当前资源位置，旧文件不移动。要管理旧文件，选回原目录。下载和使用前核对 SHA-256。删除时核对文件内容并移到系统回收站；不递归删除目录，不动旧笔记、已生成图片或其他文件。同名文件内容不符时会报错，不覆盖或删除。单独移除模型不影响另一款模型；删除共用运行库后，两款模型都要重新下载运行库才能使用。
