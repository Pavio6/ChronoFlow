// 任务状态枚举
export type TaskStatus = 
  | 'INIT'      // 初始化
  | 'ENABLED'   // 已启用
  | 'DISABLED'  // 已禁用
  | 'RUNNING'   // 运行中
  | 'SUCCESS'   // 执行成功
  | 'FAILED'    // 执行失败
  | 'TIMEOUT'   // 执行超时
  | 'DELETED';  // 已删除

// 执行状态枚举
export type ExecutionStatus =
  | 'PENDING'   // 等待执行
  | 'RUNNING'   // 执行中
  | 'SUCCESS'   // 执行成功
  | 'FAILED'    // 执行失败
  | 'RETRYING'  // 重试中
  | 'TIMEOUT';  // 执行超时

// 任务接口
export interface Task {
  id: number;
  name: string;
  description: string;
  cron_expr: string;
  callback_url: string;
  callback_method: string;
  callback_body: string;
  callback_headers: string;
  status: TaskStatus;
  timeout: number;
  max_retries: number;
  next_trigger_time: string | null;
  last_trigger_time: string | null;
  created_at: string;
  updated_at: string;
}

// 创建任务请求
export interface CreateTaskRequest {
  name: string;
  description?: string;
  cron_expr: string;
  callback_url: string;
  callback_method: 'GET' | 'POST' | 'PUT' | 'DELETE' | 'PATCH';
  callback_body?: string;
  callback_headers?: Record<string, string>;
  timeout?: number;
  max_retries?: number;
}

// 更新任务请求
export interface UpdateTaskRequest {
  name?: string;
  description?: string;
  cron_expr?: string;
  callback_url?: string;
  callback_method?: 'GET' | 'POST' | 'PUT' | 'DELETE' | 'PATCH';
  callback_body?: string;
  callback_headers?: Record<string, string>;
  timeout?: number;
  max_retries?: number;
}

// 任务列表响应
export interface TaskListResponse {
  total: number;
  page: number;
  page_size: number;
  tasks: Task[];
}

// 执行记录接口
export interface TaskExecution {
  id: number;
  task_id: number;
  trigger_time: string;
  status: ExecutionStatus;
  retry_count: number;
  request_url: string;
  request_method: string;
  request_body: string;
  response_code: number;
  response_body: string;
  error_message: string;
  started_at: string | null;
  finished_at: string | null;
  duration: number;
  next_retry_time: string | null;
  created_at: string;
  updated_at: string;
}

// 执行记录列表响应
export interface ExecutionListResponse {
  total: number;
  page: number;
  page_size: number;
  executions: TaskExecution[];
}

// API 响应通用格式
export interface ApiResponse<T> {
  code: number;
  message?: string;
  data?: T;
}

// 任务列表查询参数
export interface TaskListParams {
  page?: number;
  page_size?: number;
  status?: TaskStatus;
  keyword?: string;
}

// 执行记录查询参数
export interface ExecutionListParams {
  page?: number;
  page_size?: number;
  task_id?: number;
  status?: ExecutionStatus;
}
