import apiClient from './client';
import type { ApiResponse, MonitoringSummary } from '../types';

export const getMonitoringSummary = async (): Promise<MonitoringSummary> => {
  const res = await apiClient.get<ApiResponse<MonitoringSummary>>('/monitoring/summary');
  return (res as unknown as ApiResponse<MonitoringSummary>).data as MonitoringSummary;
};
