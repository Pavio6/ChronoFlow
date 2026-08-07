export type TimerStatus = 'ACTIVE' | 'INACTIVE' | 'DELETED';

export type MisfirePolicy = 'SKIP' | 'FIRE_ONCE' | 'CATCH_UP';

export type ExecutionStatus =
  | 'PENDING'
  | 'RUNNING'
  | 'RETRY_WAIT'
  | 'SUCCESS'
  | 'FAILED'
  | 'CANCELLED';

export interface TimerDefinition {
  id: number;
  app: string;
  name: string;
  cron_expr: string;
  callback_url: string;
  callback_method: string;
  callback_body: string;
  status: TimerStatus;
  next_fire_at: string | null;
  timezone: string;
  misfire_policy: MisfirePolicy;
  max_catch_up: number;
  version: number;
  created_at: string;
  updated_at: string;
}

export interface CreateTimerRequest {
  app: string;
  name: string;
  cron_expr: string;
  callback_url: string;
  callback_method: 'GET' | 'POST' | 'PUT' | 'DELETE' | 'PATCH';
  callback_body?: string;
  callback_headers?: Record<string, string>;
  timezone?: string;
  misfire_policy?: MisfirePolicy;
  max_catch_up?: number;
}

export interface TimerListResponse {
  total: number;
  page: number;
  page_size: number;
  items: TimerDefinition[];
  stats: {
    total: number;
    active: number;
    inactive: number;
  };
}

export interface TimerExecution {
  id: number;
  timer_id: number;
  timer_name?: string;
  scheduled_at: string;
  status: ExecutionStatus;
  attempt: number;
  max_attempts: number;
  next_attempt_at: string | null;
  response_code: number;
  response_body: string;
  error_message: string;
  started_at: string | null;
  finished_at: string | null;
  duration_ms: number;
  created_at: string;
  updated_at: string;
}

export interface ExecutionListResponse {
  total: number;
  page: number;
  page_size: number;
  items: TimerExecution[];
  stats: Partial<Record<ExecutionStatus, number>>;
}

export interface ApiResponse<T> {
  code: number;
  message?: string;
  data?: T;
}

export interface TimerListParams {
  page?: number;
  page_size?: number;
  app?: string;
  status?: TimerStatus;
  keyword?: string;
}

export interface ExecutionListParams {
  page?: number;
  page_size?: number;
  timer_name?: string;
  status?: ExecutionStatus;
}
