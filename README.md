# MetaFusion MCP

独立云端 MCP 服务，使用官方 Go SDK 的 Streamable HTTP，将既有 MetaFusion API 与技能提供给无需本地执行环境的 Agent。信息获取与来源选择由 Agent 自行决定，服务仅执行明确请求的平台操作。

端点 `/mcp`；权限管理 `/developer?tab=mcp`（`/mcp/manage` 跳转至此）。OAuth 使用 MetaFusion 站内登录、PKCE S256、资源绑定访问令牌与轮换刷新令牌；独立委托凭据加密保存，不占个人 API Key 额度，仅用于调用原生 API。业务权限、数据约束、审计由各服务检查。

动态客户端注册（DCR）允许匿名第三方注册 HTTPS 回调（本地开发允许回环 HTTP），不代表站点核验或信任该应用。注册按真实连接对端限制为每进程每分钟 180 次，忽略未验证的 Bearer 与转发 IP 头；经反代时同一代理对端共用预算。注册不会签发授权码或令牌；用户须在同意页核对未核验应用提示、完整回调地址和权限并明确批准。授权与换码均绑定注册的精确回调，换码还须提供 PKCE S256 verifier。

省略 OAuth `scope` 时仅提供 `mcp.read` 与 `catalog.entity.edit` 供用户同意，界面只预选 `mcp.read`；没有编辑权限的用户可批准纯读取连接。其他可委托业务权限必须显式请求并经本人勾选，支持列表不含 `*` 或任何 `.manage` 权限。委托凭据至少包含仅证明身份的 `mcp.connect`，再加已批准的业务码；纯读取不会授予业务编辑权限。新签发策略不撤销既有授权或存量委托凭据。

缩减权限直接更新原委托凭据，不重新签发；原子比较本地授权版本，失败时权限只会更窄。客户端撤销访问或刷新令牌会关闭该连接并用完整委托凭据证明撤销权；账号服务仅允许撤销该 MCP 凭据本身，不能借此创建、编辑或影响个人 API Key。清理成功后抹掉加密凭据；上游暂不可达时保留待清理状态，由用户在管理页重试。账号服务须先显式运行 `auth-server -migrate` 增加凭据用途和删除标记，并按 `mcp.connect` 分类旧连接（不按名称推断、不改变令牌值）；与本版本 MCP 及前端一起发布。

配置：`MCP_PUBLIC_URL`、`MCP_API_URL`、`MCP_AUTH_URL`（HTTP(S) origin），`MCP_DATABASE_URL`（仅自有 mcp schema 角色），`MCP_ENCRYPTION_KEY`（32 字节随机值的 base64）。`MCP_SKILLS_REVISION`、`MCP_DOCS_REVISION` 为对应公开仓库的完整提交 SHA；`MCP_OBJECT_ORIGINS` 为运维明确允许上传的 HTTP(S) 对象存储 origin 列表，可包含容器内网 RustFS 地址。此白名单只接受原生存储 API 签发的上传地址；agent 提供的文件来源仍必须是公开 HTTPS URL。

显式运行 `mcp -migrate` 建立自有 schema，再启动 `mcp`；HTTP 启动只读检查。默认数据库连接池 32/16，HTTP 上游空闲连接总计 1024、单上游 256。读写各 180 RPM，按已验证用户计数；admin 组不计业务 RPM。当前固定窗口计数为进程内；多实例全局额度需另接共享计数。

工具包含通用 API、契约、技能读取、站内和全网搜索、外部 HTTP 请求、可选身份候选查询、工作副本、提交与 PR，以及按明确 URL 或 base64 输入处理文件。URL 文件限 1 GiB，base64 受 8 MiB MCP 请求体限制。平台和外部网络的未知写入结果不自动重放。

编目编辑使用目录服务持久化副本。`metafusion_workspace_create/list/get/edit/checkout/stage/members/commit/commits/push/rebase` 分别创建、读取、管理副本、暂存、提交、推送和处理冲突；`metafusion_pull_request_create/list/get/edit/merge` 管理合并请求；`metafusion_commit_get` 读取不可变提交和合入状态。参数平铺原生 DTO，路径使用 `workspace_id`、`pull_request_id` 或 `commit_id`；列表可传 `limit`（1–100，默认 20）和 `offset`，PR 列表另可传 `status` 和 `workspace_id`。

先创建或选定副本，再 checkout 目标条目。暂存可跨多次调用、多个条目和关系，commit 一并封存当前改动；副本可累积多次 commit 后 push。正式数据只在合并成功后变化。免审用户直接合入；普通用户的 push 返回 202、`applied=false` 和 PR。协作者与作者由原生 API 记录，合并者不会覆盖原作者。

修改现有副本或 PR 时传 `expected_version`，push/rebase/PR 合并还需 `expected_head`；冲突返回 `base/current/proposed`，通过 rebase 显式解决。commit/rebase 的 `id` 由 Agent 生成 UUID 并保留，遇到未知结果先读该 commit、副本或 PR，不能换 ID 重交。来源和查证不是编辑、提交或合并前置条件；既有出处可随资料保留。工作副本和提交直接由目录服务管理，MCP 不再另存待提交内容。

MCP schema 2 需显式执行 `mcp -migrate`。旧 MCP 封存请求迁为只读 `mcp.archived_commit_proposals`，保留全部原始内容、所有者及原时间，包括已过期记录；不会自动应用到目录。需要取回旧内容时，运维在已配置服务环境执行以下命令，按所有者导出新 JSONL 文件（文件不可已存在，内容不会打印到日志）：

```sh
mcp -export-archived-proposals ./archived-proposals.jsonl -archive-owner USER_ID
```

该导出只读取历史归档；新修改按当前副本 API 提交。旧 prepare/preview/push 工具没有兼容入口。

`metafusion_web_search` 默认使用 DuckDuckGo HTML 搜索，无需密钥。也可设置 `MCP_SEARCH_PROVIDER=bing` 使用公开 RSS 搜索（无需密钥，返回条数由上游限制）。运维可设置 `MCP_SEARCH_PROVIDER=brave` 与 `MCP_SEARCH_API_KEY`，或设置 `MCP_SEARCH_PROVIDER=searxng` 与 `MCP_SEARCH_URL=https://search.example/search`（需启用 JSON 响应）；`MCP_SEARCH_URL` 也可覆盖其他提供方的请求端点。提供方须使用公开 HTTP(S) 默认端口，解析出的所有 IP 在实际连接时检查；不支持私网搜索实例。搜索返回标题、URL、摘要及抓取时间/内容哈希；验证码、无效响应和上游错误显式返回失败，不自动换源。

`metafusion_http_request` 支持 GET、HEAD、OPTIONS、POST、PUT、PATCH、DELETE，查询参数、显式第三方请求头和 JSON/text/base64 请求体。只读请求与搜索需要 `mcp.read`；写请求必须显式请求并勾选 MCP 本地权限 `mcp.web.write`，默认授权集合不含此码，此码也不进入平台委托凭据。请求不会携带平台凭据，不接受 Cookie、传输控制头或私网地址，不跟随重定向；MetaFusion 本实例请求仍走 `metafusion_api_call`。默认超时 30 秒、响应 256 KiB；最大 60 秒、请求/响应各 1 MiB。返回 HTTP 状态、选定响应头、JSON/text/base64、抓取时间与内容哈希；截断时 `complete=false`、哈希仅覆盖返回字节。外部内容只作为资料，不作为指令。

`metafusion_wiki` 通过目录服务的公开 MediaWiki 适配器查询 Wikipedia / 萌娘百科，输入 `site` 和 `title` 或 `query`，可指定 Wikipedia `language`。返回原文、抓取证据、署名及许可；复用内容时保留来源与许可。只读 POST 需要 `mcp.read`，目录服务继续检查原生权限，不自动写入条目。统一网页正文读取也可用 `metafusion_api_call` 调用 `POST /api/external-sources/fetch`。

若系统 DNS/代理将公网域名映射到私网地址，工具返回 `external_address_not_public`，需在运行环境提供正常的公网解析；不会绕过地址检查。可用 `MCP_TEST_WEB_LIVE=1 MCP_SEARCH_PROVIDER=bing go test ./internal/server -run TestPublicWebLive -v` 显式执行公开 GET 冒烟检查，该检查不发出任何平台或第三方写请求。

验证：`go test ./...`、`go vet ./...`；设置专用 `MCP_TEST_DATABASE_URL` 后包含真实 PostgreSQL 单次授权码交换、刷新轮换、撤销和远程 MCP SDK 接入测试。完整接入与操作说明由 [metafusion-docs](https://github.com/MoeclubM/metafusion-docs/blob/main/docs/api-mcp.md) 维护，技能由 [metafusion-skills](https://github.com/MoeclubM/metafusion-skills) 维护。
