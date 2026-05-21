import type { TaskStatus, ExecutionStatus } from '../types';

// 任务状态配置
export const TASK_STATUS_CONFIG: Record<TaskStatus, { label: string; color: string }> = {
  INIT: { label: '初始化', color: 'default' },
  ENABLED: { label: '已启用', color: 'success' },
  DISABLED: { label: '已禁用', color: 'warning' },
  RUNNING: { label: '运行中', color: 'processing' },
  SUCCESS: { label: '成功', color: 'success' },
  FAILED: { label: '失败', color: 'error' },
  TIMEOUT: { label: '超时', color: 'error' },
  DELETED: { label: '已删除', color: 'default' },
};

// 执行状态配置
export const EXECUTION_STATUS_CONFIG: Record<ExecutionStatus, { label: string; color: string }> = {
  PENDING: { label: '等待中', color: 'default' },
  RUNNING: { label: '执行中', color: 'processing' },
  SUCCESS: { label: '成功', color: 'success' },
  FAILED: { label: '失败', color: 'error' },
  RETRYING: { label: '重试中', color: 'warning' },
  TIMEOUT: { label: '超时', color: 'error' },
};

// HTTP 方法选项
export const HTTP_METHODS = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH'] as const;

// Cron 表达式预设
export const CRON_PRESETS = [
  { label: '每分钟', value: '0 * * * * *' },
  { label: '每5分钟', value: '0 */5 * * * *' },
  { label: '每小时', value: '0 0 * * * *' },
  { label: '每天凌晨2点', value: '0 0 2 * * *' },
  { label: '每天上午9点', value: '0 0 9 * * *' },
  { label: '工作日9点', value: '0 0 9 * * 1-5' },
  { label: '每周一9点', value: '0 0 9 * * 1' },
  { label: '每月1号0点', value: '0 0 0 1 * *' },
];
