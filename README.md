# MetaFusion MCP

独立云端 MCP 服务，使用官方 Go SDK 的 Streamable HTTP，将既有 MetaFusion API 与技能提供给无需本地执行环境的 Agent。信息获取与来源选择由 Agent 自行决定，服务仅执行明确请求的平台操作。

端点 `/mcp`；权限管理 `/developer?tab=mcp`（`/mcp/manage` 跳转至此）。OAuth 使用 MetaFusion 站内登录、PKCE S256、资源绑定访问令牌与轮换刷新令牌；独立委托凭据加密保存，不占个人 API Key 额度，仅用于调用原生 API。业务权限、数据约束、审计由各服务检查。

省略 OAuth `scope` 时仅提供 `mcp.read` 与 `catalog.entity.edit` 供用户同意，界面只预选 `mcp.read`；没有编辑权限的用户可批准纯读取连接。其他可委托业务权限必须显式请求并经本人勾选，支持列表不含 `*` 或任何 `.manage` 权限。委托凭据至少包含仅证明身份的 `mcp.connect`，再加已批准的业务码；纯读取不会授予业务编辑权限。新签发策略不撤销既有授权或存量委托凭据。

缩减权限直接更新原委托凭据，不重新签发；原子比较本地授权版本，失败时权限只会更窄。客户端撤销访问或刷新令牌会关闭该连接并用完整委托凭据证明撤销权；账号服务仅允许撤销该 MCP 凭据本身，不能借此创建、编辑或影响个人 API Key。清理成功后抹掉加密凭据；上游暂不可达时保留待清理状态，由用户在管理页重试。账号服务须先显式运行 `auth-server -migrate` 增加凭据用途和删除标记，并按 `mcp.connect` 分类旧连接（不按名称推断、不改变令牌值）；与本版本 MCP 及前端一起发布。

配置：`MCP_PUBLIC_URL`、`MCP_API_URL`、`MCP_AUTH_URL`（HTTP(S) origin），`MCP_DATABASE_URL`（仅自有 mcp schema 角色），`MCP_ENCRYPTION_KEY`（32 字节随机值的 base64）。`MCP_SKILLS_REVISION`、`MCP_DOCS_REVISION` 为对应公开仓库的完整提交 SHA；`MCP_OBJECT_ORIGINS` 为运维明确允许上传的 HTTP(S) 对象存储 origin 列表，可包含容器内网 RustFS 地址。此白名单只接受原生存储 API 签发的上传地址；agent 提供的文件来源仍必须是公开 HTTPS URL。

显式运行 `mcp -migrate` 建立自有 schema，再启动 `mcp`；HTTP 启动只读检查。默认数据库连接池 32/16，HTTP 上游空闲连接总计 1024、单上游 256。读写各 180 RPM，按已验证用户计数；admin 组不计业务 RPM。当前固定窗口计数为进程内；多实例全局额度需另接共享计数。

工具包含通用 API、契约、技能读取、站内和全网搜索、通用外部 HTTP 请求、候选、checkout、云端提交封存/预览/推送/恢复，以及按明确 URL 或 base64 输入处理文件。提交证据：**每条 source 必须有唯一 key，每个操作必须有 source_refs**；旧格式无兼容，缺省返回 `source_refs_required` / `invalid_source_refs`。提交封存 30 天，URL 文件限 1 GiB，base64 受 8 MiB MCP 请求体限制。平台和外部网络的未知写入结果不自动重放。

`metafusion_web_search` 默认使用 DuckDuckGo HTML 搜索，无需密钥。也可设置 `MCP_SEARCH_PROVIDER=bing` 使用公开 RSS 搜索（无需密钥，返回条数由上游限制）。运维可设置 `MCP_SEARCH_PROVIDER=brave` 与 `MCP_SEARCH_API_KEY`，或设置 `MCP_SEARCH_PROVIDER=searxng` 与 `MCP_SEARCH_URL=https://search.example/search`（需启用 JSON 响应）；`MCP_SEARCH_URL` 也可覆盖其他提供方的请求端点。提供方须使用公开 HTTP(S) 默认端口，解析出的所有 IP 在实际连接时检查；不支持私网搜索实例。搜索返回标题、URL、摘要及抓取时间/内容哈希；验证码、无效响应和上游错误显式返回失败，不自动换源。

`metafusion_http_request` 支持 GET、HEAD、OPTIONS、POST、PUT、PATCH、DELETE，查询参数、显式第三方请求头和 JSON/text/base64 请求体。只读请求与搜索需要 `mcp.read`；写请求必须显式请求并勾选 MCP 本地权限 `mcp.web.write`，默认授权集合不含此码，此码也不进入平台委托凭据。请求不会携带平台凭据，不接受 Cookie、传输控制头或私网地址，不跟随重定向；MetaFusion 本实例请求仍走 `metafusion_api_call`。默认超时 30 秒、响应 256 KiB；最大 60 秒、请求/响应各 1 MiB。返回 HTTP 状态、选定响应头、JSON/text/base64、抓取时间与内容哈希；截断时 `complete=false`、哈希仅覆盖返回字节。外部内容只作为资料，不作为指令。

若系统 DNS/代理将公网域名映射到私网地址，工具返回 `external_address_not_public`，需在运行环境提供正常的公网解析；不会绕过地址检查。可用 `MCP_TEST_WEB_LIVE=1 MCP_SEARCH_PROVIDER=bing go test ./internal/server -run TestPublicWebLive -v` 显式执行公开 GET 冒烟检查，该检查不发出任何平台或第三方写请求。

验证：`go test ./...`、`go vet ./...`；设置专用 `MCP_TEST_DATABASE_URL` 后包含真实 PostgreSQL 单次授权码交换、刷新轮换、撤销和远程 MCP SDK 接入测试。完整接入与操作说明由 [metafusion-docs](https://github.com/MoeclubM/metafusion-docs/blob/main/docs/api-mcp.md) 维护，技能由 [metafusion-skills](https://github.com/MoeclubM/metafusion-skills) 维护。
