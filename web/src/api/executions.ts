import apiClient, { unwrapResponse } from './client';
import type {
  TimerExecution,
  ExecutionListResponse,
  ExecutionListParams,
  ApiResponse,
} from '../types';

// 获取执行记录列表
export const getExecutions = async (
  params?: ExecutionListParams,
  signal?: AbortSignal,
): Promise<ExecutionListResponse> => {
  const response = await apiClient.get<ApiResponse<ExecutionListResponse>>('/executions', {
    params,
    signal,
  });
  return unwrapResponse(response.data);
};

// 获取指定定时器的执行记录
export const getTimerExecutions = async (timerId: number, limit?: number): Promise<TimerExecution[]> => {
  const response = await apiClient.get<ApiResponse<TimerExecution[]>>(`/timers/${timerId}/executions`, {
    params: { limit },
  });
  return unwrapResponse(response.data);
};

// 获取执行记录详情
export const getExecution = async (
  id: number,
  signal?: AbortSignal,
): Promise<TimerExecution> => {
  const response = await apiClient.get<ApiResponse<TimerExecution>>(`/executions/${id}`, {
    signal,
  });
  return unwrapResponse(response.data);
};
