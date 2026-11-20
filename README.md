<div align="center">

<img src="assets/FreeMD-logo.png" width="160" alt="FreeMD logo">

# FreeMD

**基于 ArcMD 深度定制与优化的本地优先原生 Markdown 编辑器（Go + MyGo）**

简体中文 · [English](README.en.md)

<a href="mygo.json"><img src="https://img.shields.io/badge/base-ArcMD%20v0.2.1-F21158" alt="基于 ArcMD v0.2.1"></a>
<a href="https://github.com/egoist/mygo"><img src="https://img.shields.io/badge/built%20with-Go%20%2B%20MyGo-00ADD8?logo=go&amp;logoColor=white" alt="使用 Go 与 MyGo 原生开发"></a>
<a href=".github/workflows/repository-check.yml"><img src="https://img.shields.io/badge/CI-Repository%20Check-2088ff?logo=githubactions&amp;logoColor=white" alt="仓库边界只读检查工作流"></a>
<a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-C912D6" alt="MIT 许可证"></a>

</div>

---

## 项目说明

**FreeMD** 是基于 [Ciao1019/ArcMD](https://github.com/Ciao1019/ArcMD)（MIT License，基线提交 `27322d387268751932624ef83a7c580cb0b068d5` / `v0.2.1`）开展的二开项目，面向本地优先的 Markdown 写作、多空间组织与原生桌面交互进行深度优化、定制与功能演进。

FreeMD 使用 MyGo v0.10.0 原生界面。打开已登记文件夹中的文档会进入所属空间。打开其他 Markdown 文件会进入文件浏览模式，可编辑、保存和另存为。临时打开的路径只保留在当前会话中。

空间支持独立附件目录、自动更新图片引用、侧边栏多选与批量整理。文档支持可导航的思维导图，以及从当前块逐步扩展到整篇文档的全选。设置中的云端同步可将选定空间的笔记和附件同步到 WebDAV 或 S3，支持手动、定时、冲突副本和可选内容加密，默认关闭。

## 从源码运行与验证

### 仓库边界检查

```sh
python -X utf8 tools/check_repo.py
python -X utf8 -m unittest discover -s tests/repository -v
```

### 运行应用与单元测试

```sh
CGO_ENABLED=0 go run ./cmd/freemd
CGO_ENABLED=0 go test ./internal/document
```

使用隔离开发目录启动（不写入个人日常文稿目录）：

```sh
FREEMD_HOME=/tmp/freemd-dev CGO_ENABLED=0 go run ./cmd/freemd
```

## 文档与仓库边界

- [架构说明（英文）](docs/architecture.md)
- [云端同步、冲突处理与恢复（英文）](docs/cloud-sync.md)
- [插件契约与路线图（英文）](docs/plugins.md)
- [仓库边界与交付约定](references/repository-boundaries.md)
- [第三方来源、上游基线与许可清单](third_party/README.md)

## 许可证

FreeMD 沿用上游 [MIT License](LICENSE)。所有第三方组件、字体、上游基线与依赖校验记录均保留在 [third_party](third_party/README.md) 与 [依赖清单](third_party/manifests/go-mod.json)。
