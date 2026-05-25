// 定时器状态枚举
export type TimerStatus =
  | 'ACTIVE'    // 激活状态
  | 'INACTIVE'  // 未激活状态
  | 'DELETED';  // 已删除

// 执行记录状态枚举
export type RecordStatus =
  | 'PENDING'   // 等待执行
  | 'RUNNING'   // 执行中
  | 'SUCCESS'   // 执行成功
  | 'FAILED';   // 执行失败

// 定时器定义接口
export interface TimerDefinition {
  id: number;
  app: string;
  name: string;
  cron_expr: string;
  callback_url: string;
  callback_method: string;
  callback_body: string;
  callback_headers: string;
  status: TimerStatus;
  created_at: string;
  updated_at: string;
}

// 创建定时器请求
export interface CreateTimerRequest {
  app: string;
  name: string;
  cron_expr: string;
  callback_url: string;
  callback_method: 'GET' | 'POST' | 'PUT' | 'DELETE' | 'PATCH';
  callback_body?: string;
  callback_headers?: Record<string, string>;
}

// 定时器列表响应
export interface TimerListResponse {
  total: number;
  page: number;
  page_size: number;
  items: TimerDefinition[];
}

// 执行记录接口
export interface TimerRecord {
  id: number;
  timer_id: number;
  trigger_time: string;
  status: RecordStatus;
  request_url: string;
  request_method: string;
  request_body: string;
  response_code: number;
  response_body: string;
  error_message: string;
  started_at: string | null;
  finished_at: string | null;
  duration: number;
  created_at: string;
  updated_at: string;
}

// 执行记录列表响应
export interface RecordListResponse {
  total: number;
  page: number;
  page_size: number;
  items: TimerRecord[];
}

// API 响应通用格式
export interface ApiResponse<T> {
  code: number;
  message?: string;
  data?: T;
}

// 定时器列表查询参数
export interface TimerListParams {
  page?: number;
  page_size?: number;
  app?: string;
  status?: TimerStatus;
  keyword?: string;
}

// 执行记录查询参数
export interface RecordListParams {
  page?: number;
  page_size?: number;
  timer_id?: number;
  status?: RecordStatus;
}

export interface MonitoringSummary {
  timers: {
    total: number;
    active: number;
    inactive: number;
    deleted: number;
  };
  records: {
    total: number;
    pending: number;
    running: number;
    success: number;
    failed: number;
  };
  redis: {
    queue_keys: number;
    queue_items: number;
    lock_keys: number;
    bucket_keys: number;
  };
  runtime: {
    exec_total: number;
    exec_success: number;
    exec_failed: number;
    trigger_total: number;
    avg_duration_ms: number;
    success_rate: number;
    last_collected_msg: string;
  };
  exporter: string;
}
