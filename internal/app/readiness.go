package app

import "context"

// checkMySQLReadiness 检查 MySQL 是否可用并返回失败原因。
func (a *Application) checkMySQLReadiness(ctx context.Context) map[string]string {
	failures := make(map[string]string)
	if err := a.sqlDB.PingContext(ctx); err != nil {
		failures["mysql"] = err.Error()
	}
	return failures
}
