import axios, { AxiosError } from 'axios';
import type { ApiResponse } from '../types';

// 创建 axios 实例
const apiClient = axios.create({
  baseURL: '/api/v1',
  timeout: 10000,
  headers: {
    'Content-Type': 'application/json',
  },
});

apiClient.interceptors.response.use(
  (response) => response,
  (error: AxiosError<ApiResponse<never>>) => {
    const message = error.response?.data?.message || error.message || '请求失败';
    return Promise.reject(new Error(message));
  }
);

export const unwrapResponse = <T>(response: ApiResponse<T>): T => {
  if (response.data === undefined) {
    throw new Error(response.message || '响应中缺少数据');
  }
  return response.data;
};

export default apiClient;
