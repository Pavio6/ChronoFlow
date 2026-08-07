import apiClient from './client';
import type {
  TimerExecution,
  ExecutionListResponse,
  ExecutionListParams,
  ApiResponse,
} from '../types';

// 获取执行记录列表
export const getExecutions = async (params?: ExecutionListParams): Promise<ExecutionListResponse> => {
  const res = await apiClient.get<ExecutionListResponse>('/executions', { params });
  return res.data;
};

// 获取指定定时器的执行记录
export const getTimerExecutions = async (timerId: number, limit?: number): Promise<ApiResponse<TimerExecution[]>> => {
  return apiClient.get(`/timers/${timerId}/executions`, { params: { limit } });
};

// 获取执行记录详情
export const getExecution = async (id: number): Promise<ApiResponse<TimerExecution>> => {
  return apiClient.get(`/executions/${id}`);
};
