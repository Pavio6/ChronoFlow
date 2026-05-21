import apiClient from './client';
import type {
  TaskExecution,
  ExecutionListResponse,
  ExecutionListParams,
  ApiResponse,
} from '../types';

// 获取执行记录列表
export const getExecutions = async (params?: ExecutionListParams): Promise<ExecutionListResponse> => {
  return apiClient.get('/executions', { params });
};

// 获取执行记录详情
export const getExecution = async (id: number): Promise<ApiResponse<TaskExecution>> => {
  return apiClient.get(`/executions/${id}`);
};
