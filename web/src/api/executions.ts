import apiClient from './client';
import type {
  TimerRecord,
  RecordListResponse,
  RecordListParams,
  ApiResponse,
} from '../types';

// 获取执行记录列表
export const getRecords = async (params?: RecordListParams): Promise<RecordListResponse> => {
  const res = await apiClient.get<any>('/records', { params });
  return res.data;
};

// 获取指定定时器的执行记录
export const getTimerRecords = async (timerId: number, limit?: number): Promise<ApiResponse<TimerRecord[]>> => {
  return apiClient.get(`/timers/${timerId}/records`, { params: { limit } });
};

// 获取执行记录详情
export const getRecord = async (id: number): Promise<ApiResponse<TimerRecord>> => {
  return apiClient.get(`/records/${id}`);
};
