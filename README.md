# MetaFusion MCP

独立云端 MCP 服务，使用官方 Go SDK 的 Streamable HTTP，将既有 MetaFusion API 与技能提供给无需本地执行环境的 Agent。信息获取与来源选择由 Agent 自行决定，服务仅执行明确请求的平台操作。

端点 `/mcp`；权限管理 `/mcp/manage`。OAuth 使用 MetaFusion 站内登录、PKCE S256、资源绑定访问令牌与轮换刷新令牌；受限 PAT 加密保存，仅用于调用原生 API。业务权限、数据约束、审计由各服务检查。

省略 OAuth `scope` 时仅提供 `mcp.read` 与 `catalog.entity.edit` 供用户同意，界面只预选 `mcp.read`；没有编辑权限的用户可批准纯读取连接。其他可委托业务权限必须显式请求并经本人勾选，支持列表不含 `*` 或任何 `.manage` 权限。底层 PAT 至少包含仅证明身份的 `mcp.connect`，再加已批准的业务码；纯读取不会授予业务编辑权限。新签发策略不撤销既有授权或存量 PAT。

配置：`MCP_PUBLIC_URL`、`MCP_API_URL`、`MCP_AUTH_URL`（HTTP(S) origin），`MCP_DATABASE_URL`（仅自有 mcp schema 角色），`MCP_ENCRYPTION_KEY`（32 字节随机值的 base64）。`MCP_SKILLS_REVISION`、`MCP_DOCS_REVISION` 为对应公开仓库的完整提交 SHA；`MCP_OBJECT_ORIGINS` 为运维明确允许上传的 HTTP(S) 对象存储 origin 列表，可包含容器内网 RustFS 地址。此白名单只接受原生存储 API 签发的上传地址；agent 提供的文件来源仍必须是公开 HTTPS URL。

显式运行 `mcp -migrate` 建立自有 schema，再启动 `mcp`；HTTP 启动只读检查。默认数据库连接池 32/16，HTTP 上游空闲连接总计 1024、单上游 256。读写各 180 RPM，按已验证用户计数；admin 组不计业务 RPM。当前固定窗口计数为进程内；多实例全局额度需另接共享计数。

工具包含通用 API、契约、技能读取、查询、候选、checkout、云端提交封存/预览/推送/恢复，以及按明确 URL 或 base64 输入处理文件。提交封存 30 天，URL 文件限 1 GiB，base64 受 8 MiB MCP 请求体限制。平台未知写入结果不自动重放。

验证：`go test ./...`、`go vet ./...`；设置专用 `MCP_TEST_DATABASE_URL` 后包含真实 PostgreSQL 单次授权码交换、刷新轮换、撤销和远程 MCP SDK 接入测试。完整接入与操作说明由 [metafusion-docs](https://github.com/MoeclubM/metafusion-docs/blob/main/docs/api-mcp.md) 维护，技能由 [metafusion-skills](https://github.com/MoeclubM/metafusion-skills) 维护。
