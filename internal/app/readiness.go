package app

import "context"

func (a *Application) checkMySQLReadiness(ctx context.Context) map[string]string {
	failures := make(map[string]string)
	if err := a.sqlDB.PingContext(ctx); err != nil {
		failures["mysql"] = err.Error()
	}
	return failures
}
