import type { TimerStatus, ExecutionStatus } from '../types';

// 定时器状态配置
export const TIMER_STATUS_CONFIG: Record<TimerStatus, { label: string }> = {
  ACTIVE: { label: '已激活' },
  INACTIVE: { label: '未激活' },
  DELETED: { label: '已删除' },
};

export const EXECUTION_STATUS_CONFIG: Record<ExecutionStatus, { label: string }> = {
  PENDING: { label: '等待中' },
  RUNNING: { label: '执行中' },
  RETRY_WAIT: { label: '等待重试' },
  SUCCESS: { label: '成功' },
  FAILED: { label: '失败' },
  CANCELLED: { label: '已取消' },
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
