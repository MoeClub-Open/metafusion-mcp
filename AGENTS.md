# MetaFusion MCP

使用中文回复。独立云端 MCP 服务，只通过 HTTP 调用现有 MetaFusion API，不读取 catalog/auth/community/storage 的数据库表。保持 API 的权限、版本、来源、审计和 commit 语义；不得自动重放未知结果的写请求。

运行服务只使用自身 mcp schema 的数据库角色。迁移通过 `go run ./cmd/mcp -migrate` 显式执行，HTTP 启动只读检查。真实凭据不进 Git、日志或 MCP 工具结果。

检查：`go test ./...`、`go vet ./...`、`go build ./cmd/mcp`。数据库集成测试通过 `MCP_TEST_DATABASE_URL` 使用专用测试库。提交只暂存本任务明确路径。

外部搜索/HTTP 使用独立 publicClient，连接时校验并固定公开 IP，不携带平台凭据、不跟随重定向、不自动重放。GET/HEAD/OPTIONS 与搜索要求 `mcp.read`；POST/PUT/PATCH/DELETE 要求显式同意的本地 scope `mcp.web.write`。本地 scope 不进平台 PAT，也不能放行平台 API 写入。提供方由 MCP_SEARCH_PROVIDER/URL/API_KEY 配置，密钥不进工具结果或日志。

## 提交证据（破坏性）

CatalogCommit / `metafusion_commit_prepare`：每条 `sources[]` 必须有唯一 `key`；每个操作必须有非空 `source_refs`。旧格式无兼容——缺省返回结构化 `source_refs_required` / `source_key_required` / `source_unused`（及 MCP 侧 `invalid_source_refs`）。工具描述与 schema 已强制；示例见 metafusion-docs `api-commits` 与 metafusion-skills 模板。

