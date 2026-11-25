# OneDrive、Google Drive 与 GitHub/Git 同步方案核查

核查日期：2026-11-25。范围是官方协议、成熟 Go 实现及 FreeFolio 的适配方式；没有注册应用、连接真实账号或实现这些新后端。

## 结论

建议继续复用现有同步引擎，分别接入官方账号授权与传输接口。普通用户使用“登录账号 → 选择空间 → 手动首次同步”；高级 Git 模式单独提供，避免把 Git 仓库配置变成所有用户的前提。

| 服务 | 建议方案 | 增量识别 | 必须先验证的边界 |
| --- | --- | --- | --- |
| OneDrive | 系统浏览器 OAuth 授权码 + PKCE，Microsoft Graph v1.0，优先应用专用文件夹 | `driveItem delta`，保留 `nextLink` / `deltaLink` | 个人与工作/学校账号的最小权限；上传最终提交时的版本保护；失效游标恢复 |
| Google Drive | Desktop OAuth + PKCE，Drive API v3，优先 `drive.file` 与应用创建的可见文件夹 | `changes.getStartPageToken` + `changes.list` | 同名文件与 ID 映射；条件写入是否覆盖媒体上传/删除；跨设备对同一授权文件集的可见性 |
| GitHub | GitHub App 设备码授权；Git Data REST 或 GraphQL 批量提交 | 分支 head 和文件 blob/tree 对比 | 私有仓库授权、分支保护、并发提交、API 限流和附件体积 |
| 通用 Git | 单独的高级模式；评估 go-git 或用户已有系统 Git | fetch 后比较共同祖先与远端提交 | 已有脏工作区、用户分支、SSH/凭据、LFS 和非快进冲突 |

以上是建议，不代表三个服务已经接入。现有 WebDAV/S3、自动调度规则、回收站、恢复副本及本次新增的段落合并应继续复用。

## OneDrive

官方支持原生桌面通过系统浏览器和回环地址完成授权，使用授权码与 PKCE。FreeFolio 应注册自己的 Microsoft Entra 公共客户端，支持所需账号类型；不把机密客户端密钥放进桌面程序。参考[桌面应用配置](https://learn.microsoft.com/en-us/entra/identity-platform/scenario-desktop-app-configuration)和[授权码流程](https://learn.microsoft.com/en-us/entra/identity-platform/v2-oauth2-auth-code-flow)。

Microsoft Graph 是访问个人 OneDrive、OneDrive for Business 和 SharePoint 文件的官方统一入口。建议默认使用 `Apps/FreeFolio` 对应的 `approot`，而不是要求用户手动填写 WebDAV 地址。[文件接口概览](https://learn.microsoft.com/en-us/graph/api/resources/onedrive?view=graph-rest-1.0)。

当前[应用文件夹概念文档](https://learn.microsoft.com/en-us/graph/onedrive-sharepoint-appfolder)说明 `Files.ReadWrite.AppFolder` 可用于个人和工作/学校 OneDrive，并支持委托及应用权限。但[特殊文件夹 API 权限表](https://learn.microsoft.com/en-us/graph/api/drive-get-specialfolder?view=graph-rest-1.0)和部分旧 OneDrive 权限页仍有不同表述。因此采用最小权限是建议，个人与组织账号都需要真实测试后才宣布支持；不能自动升级成整个网盘读写权限。

同步初次通过 `delta` 完整建立清单，之后使用返回的游标。删除作为事件处理，目录扫描失败或游标失效时先恢复完整清单，不把遗漏条目当删除。[delta 官方说明](https://learn.microsoft.com/en-us/graph/api/driveitem-delta?view=graph-rest-1.0)。

大附件使用可恢复上传会话，更新绑定读取时的 ETag，处理 `412` 后重新拉取并合并。官方上传会话文档说明条件请求及续传，但还需验证“创建会话后另一设备修改、最后一块才提交”的完整竞态；不能只测请求成功。[上传会话](https://learn.microsoft.com/en-us/graph/api/driveitem-createuploadsession?view=graph-rest-1.0)。

可借鉴 [rclone v1.76.0 的 OneDrive 后端](https://github.com/rclone/rclone/tree/v1.76.0/backend/onedrive)的路径编码、分页、重试、分块和账号类型处理。建议按所需接口复用，保留 FreeFolio 的冲突引擎；引入整个 rclone 程序不是账号登录的必要条件。

## Google Drive

官方 Desktop OAuth 推荐系统浏览器加回环地址，并强烈建议 PKCE。桌面应用应使用自己的 Google Cloud OAuth 客户端；不把网页登录塞进应用内 WebView。[桌面 OAuth](https://developers.google.com/identity/protocols/oauth2/native-app)、[最佳实践](https://developers.google.com/identity/protocols/oauth2/resources/best-practices)。

建议采用 `drive.file`，访问应用创建或用户明确交给应用的文件。它是官方推荐的非敏感权限。创建可见的 FreeFolio 文件夹便于用户直接备份和查看；`appDataFolder` 更适合隐藏配置，不作为普通笔记默认位置。选择任意已有文件夹时，不能假设授权一个文件夹就自动获得全部已有子文件权限。[权限说明](https://developers.google.com/workspace/drive/api/guides/api-specific-auth)。

初次建立文件清单与变更游标，后续分页读取 `changes.list`，只有全部分页应用成功后才保存 `newStartPageToken`。这是服务端变更记录，可减少反复扫描，但仍要处理移入、移出、删除和权限变化。[变更查询](https://developers.google.com/workspace/drive/api/guides/manage-changes)。

Drive 的文件 ID 才是身份，名称在同一目录内也不必唯一；不能直接把“路径字符串”当作远端唯一键。映射至少保存文件 ID、父目录 ID、名称和版本，并明确同名冲突处理。[文件模型](https://developers.google.com/workspace/drive/api/reference/rest/v3/files)。

**未决项：条件覆盖。** 本次核对的 Drive v3 `files.update` 和文件模型资料不足以确认对所有媒体上传及删除都提供与现有 WebDAV/S3 等价的 `If-Match` 保证。其他 Google API 的 ETag 文档不能作为 Drive 的证明。实施前需要独立双客户端实验；验证前只能设计“保留新副本、提示冲突”，不能声称上传前读一次版本即可防止并发覆盖。[更新接口](https://developers.google.com/workspace/drive/api/reference/rest/v3/files/update)。

OAuth 处于外部应用 Testing 状态时，涉及 Drive 权限的刷新令牌通常七天到期。长期使用需准备正式同意界面、生产发布状态及适用的验证流程；使用非敏感权限可以减少权限审查负担，但不等于所有品牌验证都自动免除。[令牌生命周期](https://developers.google.com/identity/protocols/oauth2)。

可复用 [Google 官方 Go 客户端的 Drive v3](https://github.com/googleapis/google-api-go-client/tree/v0.317.0/drive/v3)，并参考 [rclone Drive 后端](https://github.com/rclone/rclone/tree/v1.76.0/backend/drive)的重复名称、分页和限流处理。完整 rclone bisync 有自己的状态与冲突规则，不能不经适配就替换 FreeFolio 的恢复与回收站契约。[bisync 行为](https://rclone.org/bisync/)。

## GitHub 与通用 Git

建议面向普通用户的 GitHub 模式使用 GitHub App，启用设备码授权，用户在浏览器中确认并授权选定仓库。权限以 Contents 读写和必要 Metadata 为限；第一版可让用户选择已建的私有仓库，不申请管理所有仓库。官方设备码授权不需要在桌面程序中保存 App 私钥；设备码取得的用户令牌续期也有免 `client_secret` 的路径。[授权流程](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-user-access-token-for-a-github-app)、[续期规则](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/refreshing-user-access-tokens)。

两种官方写入方式都值得考虑：

| 方式 | 用法 | 取舍 |
| --- | --- | --- |
| GraphQL `createCommitOnBranch` | 用 `expectedHeadOid` 检查分支旧提交，一次新增/删除多文件并推进分支 | 小批量笔记改动请求少；要验证请求体大小、空仓库初始化和限流；作者由授权身份确定 |
| REST Git Data | 上传 blob、创建 tree、创建以旧 head 为父的 commit，再以 `force=false` 更新 ref | 便于分块组织内容；请求较多；并发失败后重新拉取和合并，不强推 |

依据：[批量创建提交](https://docs.github.com/en/graphql/reference/commits)、[更新 Git 引用](https://docs.github.com/en/rest/git/refs)。当前官方 REST 示例使用 `X-GitHub-Api-Version: 2026-03-10`；新适配应固定并验证该版本，不照抄旧示例日期。

远端仍是普通 Git 仓库，本地不一定要创建 `.git`。记录远端共同提交，先拉取、合并，再提交一次完整改动。所有更新只作用于用户选择的目录，保留其他文件。递归 tree 返回 `truncated=true` 时必须逐子树获取；不能把截断清单拿来判断删除。官方递归 tree 限制为 100,000 条或 7 MB。[Git trees](https://docs.github.com/en/rest/git/trees)。

GitHub 的主限流之外还有内容写入等次级限流，批量合并提交比每次自动保存生成一串 Contents API 写入更合适。桌面没有公网 webhook 接收端时按需轮询 head，利用条件请求和退避，不新建服务端只是为了通知。[API 最佳实践](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api)、[限流](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api)。

GitHub 适合文本笔记与适量附件。大量 AI 视频、频繁改写的大图片会使历史持续增大，不能把它作为通用大文件网盘。后续若支持 GitLab、自建 Git、SSH 或用户已有仓库，再做独立高级 Git 模式，评估 [go-git](https://github.com/go-git/go-git)或系统 Git。现有仓库的脏文件、分支和用户提交必须保护；LFS 需要另行验证，不能从普通 push 可用推断。

## 本次查询到的正式版本

通过各项目 GitHub Releases API 读取，均非预发布；只记录研究基线，没有升级 FreeFolio 依赖。

| 项目 | 版本 | 发布时间（UTC 日期） | 用途 |
| --- | --- | --- | --- |
| [rclone](https://github.com/rclone/rclone/releases/tag/v1.76.0) | v1.76.0 | 2026-11-06 | OneDrive/Drive 传输行为参考 |
| [go-git](https://github.com/go-git/go-git/releases/tag/v5.19.3) | v5.19.3 | 2026-10-04 | 通用 Git 的 Go 实现候选 |
| [Microsoft Graph Go SDK](https://github.com/microsoftgraph/msgraph-sdk-go/releases/tag/v1.107.0) | v1.107.0 | 2026-11-20 | 官方 Graph 类型与调用候选 |
| [Google API Go Client](https://github.com/googleapis/google-api-go-client/releases/tag/v0.317.0) | v0.317.0 | 2026-11-24 | 官方 Drive v3 客户端候选 |

## 对 FreeFolio 的落地建议

1. 优先验证 OneDrive 的浏览器登录、应用目录、delta 与条件覆盖。它与现有文件同步接口最接近。
2. GitHub 做独立账号连接，以私有笔记仓库、小批量原子提交和不强推为验收条件。
3. Google Drive 先验证最小权限下跨设备文件可见性和并发写入，再选择普通文件覆盖或保留版本副本方案。
4. 共用账号连接状态、令牌加密存储、刷新互斥、取消、重试和同步界面。凭据不进空间或同步数据；Windows 使用 DPAPI，其他平台沿用现有系统加密设施的方向。
5. 全部后端保留“开启不立即同步”“只同步明确选择的空间”、活动编辑保护、本地备份及系统回收站。服务端游标必须绑定账号、盘/仓库、根目录和权限范围。

正式实施还需要维护者注册各服务自己的应用标识，并提供隔离测试账号/仓库授权。本次研究没有使用真实账号验证，也没有替用户注册或发布这些外部应用。
