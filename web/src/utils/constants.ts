import type { TimerStatus, RecordStatus } from '../types';

// 定时器状态配置
export const TIMER_STATUS_CONFIG: Record<TimerStatus, { label: string; color: string }> = {
  ACTIVE: { label: '已激活', color: 'success' },
  INACTIVE: { label: '未激活', color: 'default' },
  DELETED: { label: '已删除', color: 'error' },
};

// 执行记录状态配置
export const RECORD_STATUS_CONFIG: Record<RecordStatus, { label: string; color: string }> = {
  PENDING: { label: '等待中', color: 'default' },
  RUNNING: { label: '执行中', color: 'processing' },
  SUCCESS: { label: '成功', color: 'success' },
  FAILED: { label: '失败', color: 'error' },
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

// 应用名预设（示例）
export const APP_PRESETS = [
  'default',
  'order-service',
  'payment-service',
  'notification-service',
];
