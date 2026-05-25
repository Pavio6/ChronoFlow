import apiClient from './client';
import type { ApiResponse, MonitoringHistory, MonitoringSummary } from '../types';

export const getMonitoringSummary = async (): Promise<MonitoringSummary> => {
  const res = await apiClient.get<ApiResponse<MonitoringSummary>>('/monitoring/summary');
  return (res as unknown as ApiResponse<MonitoringSummary>).data as MonitoringSummary;
};

export const getMonitoringHistory = async (rangeMinutes: number): Promise<MonitoringHistory> => {
  const res = await apiClient.get<ApiResponse<MonitoringHistory>>('/monitoring/history', {
    params: { range_minutes: rangeMinutes },
  });
  return (res as unknown as ApiResponse<MonitoringHistory>).data as MonitoringHistory;
};
