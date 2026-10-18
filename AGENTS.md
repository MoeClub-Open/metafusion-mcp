# MetaFusion MCP

使用中文回复。独立云端 MCP 服务，只通过 HTTP 调用现有 MetaFusion API，不读取 catalog/auth/community/storage 的数据库表。保持 API 的权限、版本、作者归属和副本合并语义；不得自动重放未知结果的写请求。

运行服务只使用自身 mcp schema 的数据库角色。迁移通过 `go run ./cmd/mcp -migrate` 显式执行，HTTP 启动只读检查。真实凭据不进 Git、日志或 MCP 工具结果。

检查：`go test ./...`、`go vet ./...`、`go build ./cmd/mcp`。数据库集成测试通过 `MCP_TEST_DATABASE_URL` 使用专用测试库。提交只暂存本任务明确路径。

外部搜索/HTTP 使用独立 publicClient，连接时校验并固定公开 IP，不携带平台凭据、不跟随重定向、不自动重放。GET/HEAD/OPTIONS 与搜索要求 `mcp.read`；POST/PUT/PATCH/DELETE 要求显式同意的本地 scope `mcp.web.write`。本地 scope 不进平台委托凭据，也不能放行平台 API 写入。提供方由 MCP_SEARCH_PROVIDER/URL/API_KEY 配置，密钥不进工具结果或日志。

MCP 使用专用委托凭据，与个人 API Key 分别管理和计额；缩减权限更新原凭据。客户端撤销后关闭连接并清理凭据，失败保留可重试状态，不重新签发、不自动重放未知写请求。凭完整委托凭据的撤销证明只允许撤销其自身，不允许编辑、签发或操作个人密钥。

## 编目协作

目录编辑统一走服务器工作副本：workspace create/checkout → changes 暂存 → commit → push；普通用户由 PR 合并，免审用户可直接合入。`expected_version` 与 `expected_head` 必须使用当前副本/PR 返回值；commit 与 rebase 的 UUID 由调用方生成并保留，结果不明先读 commit/workspace/PR，不能自动重放或换 ID 重交。来源、查证记录和候选检查都是可选资料，不设独立必经流程，不生成虚假出处。

工具参数平铺原生 DTO 字段，路径参数分别为 `workspace_id`、`pull_request_id`、`commit_id`。`metafusion_workspace_stage` 仅转发 `PUT /api/catalog/workspaces/:id/changes`；读取线上契约，不维护第二条提交或审核实现。旧 MCP 封存工具及 `mcp.drafts` 已移除；schema 2 将旧请求及其所有者原样归档，不自动执行。只允许通过显式 owner-scoped 导出命令读取归档，不对 MCP 暴露归档接口。
