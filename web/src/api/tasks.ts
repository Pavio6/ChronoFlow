import apiClient from './client';
import type {
  TimerDefinition,
  CreateTimerRequest,
  UpdateTimerRequest,
  TimerListResponse,
  TimerListParams,
  ApiResponse,
} from '../types';

// 获取定时器列表
export const getTimers = async (params?: TimerListParams): Promise<TimerListResponse> => {
  const res = await apiClient.get<any>('/timers', { params });
  return res.data;
};

// 获取定时器详情
export const getTimer = async (id: number): Promise<ApiResponse<TimerDefinition>> => {
  return apiClient.get(`/timers/${id}`);
};

// 创建定时器
export const createTimer = async (data: CreateTimerRequest): Promise<ApiResponse<TimerDefinition>> => {
  return apiClient.post('/timers', data);
};

// 更新定时器
export const updateTimer = async (id: number, data: UpdateTimerRequest): Promise<ApiResponse<TimerDefinition>> => {
  return apiClient.put(`/timers/${id}`, data);
};

// 删除定时器
export const deleteTimer = async (id: number): Promise<ApiResponse<void>> => {
  return apiClient.delete(`/timers/${id}`);
};

// 激活定时器
export const activateTimer = async (id: number): Promise<ApiResponse<void>> => {
  return apiClient.post(`/timers/${id}/activate`);
};

// 停用定时器
export const deactivateTimer = async (id: number): Promise<ApiResponse<void>> => {
  return apiClient.post(`/timers/${id}/deactivate`);
};
