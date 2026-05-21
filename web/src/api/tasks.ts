import apiClient from './client';
import type {
  Task,
  CreateTaskRequest,
  UpdateTaskRequest,
  TaskListResponse,
  TaskListParams,
  ApiResponse,
} from '../types';

// 获取任务列表
export const getTasks = async (params?: TaskListParams): Promise<TaskListResponse> => {
  return apiClient.get('/tasks', { params });
};

// 获取任务详情
export const getTask = async (id: number): Promise<ApiResponse<Task>> => {
  return apiClient.get(`/tasks/${id}`);
};

// 创建任务
export const createTask = async (data: CreateTaskRequest): Promise<ApiResponse<Task>> => {
  return apiClient.post('/tasks', data);
};

// 更新任务
export const updateTask = async (id: number, data: UpdateTaskRequest): Promise<ApiResponse<Task>> => {
  return apiClient.put(`/tasks/${id}`, data);
};

// 删除任务
export const deleteTask = async (id: number): Promise<ApiResponse<void>> => {
  return apiClient.delete(`/tasks/${id}`);
};

// 启用任务
export const enableTask = async (id: number): Promise<ApiResponse<void>> => {
  return apiClient.post(`/tasks/${id}/enable`);
};

// 禁用任务
export const disableTask = async (id: number): Promise<ApiResponse<void>> => {
  return apiClient.post(`/tasks/${id}/disable`);
};

// 手动触发任务
export const triggerTask = async (id: number): Promise<ApiResponse<void>> => {
  return apiClient.post(`/tasks/${id}/trigger`);
};
