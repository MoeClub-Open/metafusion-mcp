# MetaFusion MCP

使用中文回复。独立云端 MCP 服务，只通过 HTTP 调用现有 MetaFusion API，不读取 catalog/auth/community/storage 的数据库表。保持 API 的权限、版本、来源、审计和 commit 语义；不得自动重放未知结果的写请求。

运行服务只使用自身 mcp schema 的数据库角色。迁移通过 `go run ./cmd/mcp -migrate` 显式执行，HTTP 启动只读检查。真实凭据不进 Git、日志或 MCP 工具结果。

检查：`go test ./...`、`go vet ./...`、`go build ./cmd/mcp`。数据库集成测试通过 `MCP_TEST_DATABASE_URL` 使用专用测试库。提交只暂存本任务明确路径。
