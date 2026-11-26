# 白板图片历史与对比

## 行为

- 图片生成节点再次点击“运行”会重新生成，结果追加到历史；整图工作流仍保留未变化输入的缓存
- 图片历史随白板保存，不按固定数量丢弃；同一次返回多张图片时显示批次和张数
- 查看入口在节点图片区，可前后切换或直接选择历史
- 对比在同一图片区内进行，A、B 独立选择历史图片，拖动分隔线或用方向键调整位置
- 默认对比当前图片与前一张；不同尺寸图片按比例居中，不拉伸
- 查看历史沿用当前结果选择规则；对比的 A、B 和分隔位置只影响预览，不改变下游输入
- 只读和锁定节点允许临时查看与对比，不保存选择；生成失败、取消不移除历史

## 社区参考与取舍

参考 [rgthree Image Comparer](https://github.com/rgthree/rgthree-comfy/blob/bc48c6aaeca87bedbc14245d89f9bc6be37a630e/src_web/comfyui/image_comparer.ts)（MIT，2026-10-30 提交）的节点内 A/B 选择、等比显示与裁剪对比交互，使用现有 Go/MyGo 控件实现，不引入浏览器依赖或新的比较节点

[img-comparison-slider](https://github.com/sneas/img-comparison-slider/tree/c73939d61c8fb010342c9910f1de420edbf0b067)（MIT，2026-08-20 提交）提供拖动和键盘交互参考

滑杆保留较大的图片面积；并排会缩小图片，本次采用滑杆；单击切换不能同时观察两侧，不作为默认方式

## 实施与验证

1. 在 `internal/whiteboard/ai.go` 保留全部图片历史，测试多批次与保存重开
2. 在 `internal/editor/board_ai_card.go` 修正图片节点手动运行行为，使用真实节点按钮测试连续运行
3. 在 `internal/editor/board_ai_history.go` 集中历史控件、预览状态与裁剪显示，复用现有图片加载和节点布局
4. 使用 `ui.NewTester` 验证切换、任意 A/B、分隔线拖动与键盘、只读、缩放和失败状态
5. 补中英文文案，运行项目要求的检查、测试、跨平台构建与本地测试包构建
