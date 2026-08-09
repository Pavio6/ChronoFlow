package app

// Role 表示一个可部署的 ChronoFlow 运行角色；RoleAll 仅用于本地开发组合。
type Role string

const (
	RoleAPI        Role = "api"
	RoleScheduler  Role = "scheduler"
	RoleDispatcher Role = "dispatcher"
	RoleWorker     Role = "worker"
	RoleAll        Role = "all"
)

var allRoles = []Role{
	RoleAPI,
	RoleScheduler,
	RoleDispatcher,
	RoleWorker,
	RoleAll,
}

// RuntimeMode 返回健康检查接口展示的角色运行模式。
func (r Role) RuntimeMode() string {
	switch r {
	case RoleAPI:
		return "control-plane"
	case RoleScheduler:
		return "scheduler"
	case RoleDispatcher:
		return "outbox-dispatcher"
	case RoleWorker:
		return "stream-worker"
	case RoleAll:
		return "combined"
	default:
		return "unknown"
	}
}

// Components 返回该角色拥有的运行组件名称。
func (r Role) Components() []string {
	switch r {
	case RoleAPI:
		return []string{"api"}
	case RoleScheduler:
		return []string{"scheduler", "execution-reconciler"}
	case RoleDispatcher:
		return []string{"outbox-dispatcher"}
	case RoleWorker:
		return []string{"stream-worker", "stream-retention-cleaner"}
	case RoleAll:
		return []string{
			"api",
			"scheduler",
			"execution-reconciler",
			"outbox-dispatcher",
			"stream-worker",
			"stream-retention-cleaner",
		}
	default:
		return nil
	}
}
