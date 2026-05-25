import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Button, Empty, Select, Space, Tag } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';
import { getMonitoringHistory } from '../api/monitoring';
import type { MonitoringHistory, MonitoringPoint } from '../types';

const rangeOptions = [
  { label: '最近 15 分钟', value: 15 },
  { label: '最近 1 小时', value: 60 },
  { label: '最近 6 小时', value: 360 },
  { label: '最近 24 小时', value: 1440 },
];

const Monitoring: React.FC = () => {
  const [historyLoading, setHistoryLoading] = useState(false);
  const [history, setHistory] = useState<MonitoringHistory | null>(null);
  const [historyUnavailable, setHistoryUnavailable] = useState(false);
  const [rangeMinutes, setRangeMinutes] = useState(60);
  const historyRequestID = useRef(0);

  const loadHistory = useCallback(async () => {
    const requestID = ++historyRequestID.current;
    setHistoryLoading(true);
    try {
      const data = await getMonitoringHistory(rangeMinutes);
      if (requestID !== historyRequestID.current) {
        return;
      }
      setHistory(data);
      setHistoryUnavailable(false);
    } catch {
      if (requestID === historyRequestID.current) {
        setHistoryUnavailable(true);
      }
    } finally {
      if (requestID === historyRequestID.current) {
        setHistoryLoading(false);
      }
    }
  }, [rangeMinutes]);

  useEffect(() => {
    loadHistory();
    const timer = window.setInterval(loadHistory, 10000);
    return () => window.clearInterval(timer);
  }, [loadHistory]);

  const historySeries = history?.series;
  const availability = latestValue(historySeries?.availability);

  return (
    <div className="page-stack monitor-page">
      <div className="monitor-toolbar">
        <p>趋势数据由 Prometheus 持久化保存，每 10 秒自动刷新</p>
        <Space>
          <Tag color={historyUnavailable ? 'error' : history === null ? 'processing' : 'success'}>
            {historyUnavailable ? '历史数据不可用' : history === null ? '历史数据加载中' : 'Prometheus connected'}
          </Tag>
          <Select value={rangeMinutes} options={rangeOptions} onChange={setRangeMinutes} className="history-range" />
          <Button icon={<ReloadOutlined />} loading={historyLoading} onClick={loadHistory}>刷新</Button>
        </Space>
      </div>

      <div className="monitor-grid">
        <section className="surface monitor-panel">
          <div className="panel-heading">
            <h3>服务可用性</h3>
            <HealthStat
              label="采集状态"
              value={availability === undefined ? '--' : availability === 1 ? 'UP' : 'DOWN'}
              healthy={availability === undefined ? undefined : availability === 1}
            />
          </div>
          <LineChart
            title="Prometheus scrape"
            points={values(historySeries?.availability)}
            labels={labels(historySeries?.availability)}
            color="#16a34a"
            displayValue={(value) => value === 1 ? 'UP' : 'DOWN'}
          />
        </section>

        <section className="surface monitor-panel">
          <div className="panel-heading">
            <h3>成功率趋势</h3>
            <span>{historySeries?.success_rate.length || 0} samples</span>
          </div>
          <LineChart
            points={values(historySeries?.success_rate)}
            labels={labels(historySeries?.success_rate)}
            suffix="%"
            color="#16a34a"
          />
        </section>

        <section className="surface monitor-panel">
          <div className="panel-heading">
            <h3>回调延迟</h3>
            <span>P95 latency</span>
          </div>
          <LineChart
            title="P95 延迟"
            points={values(historySeries?.callback_p95_ms)}
            labels={labels(historySeries?.callback_p95_ms)}
            suffix="ms"
            color="#2563eb"
          />
        </section>

        <section className="surface monitor-panel">
          <div className="panel-heading">
            <h3>异常任务</h3>
            <span>overdue or stale</span>
          </div>
          <LineChart
            title="超期或卡住记录"
            points={values(historySeries?.abnormal_records)}
            labels={labels(historySeries?.abnormal_records)}
            color="#d97706"
          />
        </section>
      </div>
    </div>
  );
};

const HealthStat: React.FC<{ label: string; value: string; healthy?: boolean }> = ({ label, value, healthy }) => (
  <div className="health-chip">
    <span>{label}</span>
    <strong className={healthy === undefined ? undefined : healthy ? 'success-text' : 'danger-text'}>{value}</strong>
  </div>
);

interface LineChartProps {
  title?: string;
  points: number[];
  labels: string[];
  color: string;
  suffix?: string;
  precision?: number;
  displayValue?: (value: number) => string;
}

const LineChart: React.FC<LineChartProps> = ({ title, points, labels, color, suffix = '', precision, displayValue }) => {
  if (points.length === 0) {
    return (
      <div className="line-card chart-empty">
        <div className="line-card-head">
          <span>{title || '趋势'}</span>
          <strong>--</strong>
        </div>
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无历史数据" />
      </div>
    );
  }
  const normalized = points;
  const max = Math.max(...normalized, 1);
  const min = Math.min(...normalized, 0);
  const range = Math.max(max - min, 1);
  const width = 420;
  const height = 140;
  const padding = 14;
  const step = normalized.length > 1 ? (width - padding * 2) / (normalized.length - 1) : 0;
  const coords = normalized.map((value, index) => {
    const x = padding + index * step;
    const y = height - padding - ((value - min) / range) * (height - padding * 2);
    return { x, y };
  });
  const line = coords.map((point) => `${point.x},${point.y}`).join(' ');
  const area = `${padding},${height - padding} ${line} ${width - padding},${height - padding}`;
  const latest = normalized[normalized.length - 1] || 0;

  return (
    <div className="line-card">
      <div className="line-card-head">
        <span>{title || '趋势'}</span>
        <strong>{displayValue ? displayValue(latest) : `${latest.toFixed(precision ?? (suffix === '%' || suffix === 'ms' ? 1 : 0))}${suffix}`}</strong>
      </div>
      <svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label={title || '趋势图'}>
        <defs>
          <linearGradient id={`area-${color.replace('#', '')}`} x1="0" x2="0" y1="0" y2="1">
            <stop offset="0%" stopColor={color} stopOpacity="0.18" />
            <stop offset="100%" stopColor={color} stopOpacity="0" />
          </linearGradient>
        </defs>
        <polyline className="grid-line" points={`${padding},${height - padding} ${width - padding},${height - padding}`} />
        <polygon points={area} fill={`url(#area-${color.replace('#', '')})`} />
        <polyline points={line} fill="none" stroke={color} strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" />
        {coords.map((point, index) => (
          <circle key={`${point.x}-${index}`} cx={point.x} cy={point.y} r="3.5" fill="#ffffff" stroke={color} strokeWidth="2" />
        ))}
      </svg>
      <div className="chart-labels">
        <span>{labels[0] || '--'}</span>
        <span>{labels[labels.length - 1] || '--'}</span>
      </div>
    </div>
  );
};

const values = (points?: MonitoringPoint[]): number[] => points?.map((item) => item.value) || [];

const labels = (points?: MonitoringPoint[]): string[] => points?.map((item) => (
  new Date(item.timestamp * 1000).toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit' })
)) || [];

const latestValue = (points?: MonitoringPoint[]): number | undefined => points && points.length > 0 ? points[points.length - 1].value : undefined;

export default Monitoring;
