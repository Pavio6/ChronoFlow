import apiClient, { unwrapResponse } from './client';
import type {
  TimerDefinition,
  CreateTimerRequest,
  TimerListResponse,
  TimerListParams,
  ApiResponse,
} from '../types';

// 获取定时器列表
export const getTimers = async (
  params?: TimerListParams,
  signal?: AbortSignal,
): Promise<TimerListResponse> => {
  const response = await apiClient.get<ApiResponse<TimerListResponse>>('/timers', {
    params,
    signal,
  });
  return unwrapResponse(response.data);
};

// 获取定时器详情
export const getTimer = async (
  id: number,
  signal?: AbortSignal,
): Promise<TimerDefinition> => {
  const response = await apiClient.get<ApiResponse<TimerDefinition>>(`/timers/${id}`, {
    signal,
  });
  return unwrapResponse(response.data);
};

// 创建定时器
export const createTimer = async (data: CreateTimerRequest): Promise<TimerDefinition> => {
  const response = await apiClient.post<ApiResponse<TimerDefinition>>('/timers', data);
  return unwrapResponse(response.data);
};

// 删除定时器
export const deleteTimer = async (id: number): Promise<void> => {
  await apiClient.delete(`/timers/${id}`);
};

// 激活定时器
export const activateTimer = async (id: number): Promise<void> => {
  await apiClient.post(`/timers/${id}/activate`);
};

// 停用定时器
export const deactivateTimer = async (id: number): Promise<void> => {
  await apiClient.post(`/timers/${id}/deactivate`);
};
