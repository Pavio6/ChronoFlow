import React, { useCallback, useEffect, useState } from 'react';
import { Button, Empty, message, Progress, Space, Spin, Tag } from 'antd';
import {
  ClockCircleOutlined,
  CloudServerOutlined,
  DatabaseOutlined,
  ReloadOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons';
import { getMonitoringSummary } from '../api/monitoring';
import type { MonitoringSummary } from '../types';

const numberFormat = new Intl.NumberFormat('zh-CN');
const HISTORY_LIMIT = 12;

interface SamplePoint {
  label: string;
  execTotal: number;
  successRate: number;
  avgDuration: number;
  queueItems: number;
  lockKeys: number;
}

const Monitoring: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const [summary, setSummary] = useState<MonitoringSummary | null>(null);
  const [history, setHistory] = useState<SamplePoint[]>([]);

  const loadSummary = useCallback(async () => {
    setLoading(true);
    try {
      const data = await getMonitoringSummary();
      setSummary(data);
      setHistory((items) => {
        const next = [
          ...items,
          {
            label: new Date().toLocaleTimeString('zh-CN', { hour12: false, minute: '2-digit', second: '2-digit' }),
            execTotal: data.runtime.exec_total,
            successRate: data.runtime.success_rate * 100,
            avgDuration: data.runtime.avg_duration_ms,
            queueItems: data.redis.queue_items,
            lockKeys: data.redis.lock_keys,
          },
        ];
        return next.slice(-HISTORY_LIMIT);
      });
    } catch (error: unknown) {
      message.error((error as Error).message || '加载监控数据失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    const run = async () => { await loadSummary(); };
    run();
    const timer = window.setInterval(loadSummary, 10000);
    return () => window.clearInterval(timer);
  }, [loadSummary]);

  if (!summary && loading) {
    return <div className="surface monitor-loading"><Spin /></div>;
  }

  if (!summary) {
    return (
      <div className="surface monitor-empty">
        <Empty description="暂无监控数据" />
        <Button icon={<ReloadOutlined />} onClick={loadSummary}>刷新</Button>
      </div>
    );
  }

  const successPercent = Math.round(summary.runtime.success_rate * 100);
  const recordTotal = Math.max(summary.records.total, 1);
  const recordSegments = [
    { label: '成功', value: summary.records.success, color: '#16a34a' },
    { label: '失败', value: summary.records.failed, color: '#dc2626' },
    { label: '执行中', value: summary.records.running, color: '#2563eb' },
    { label: '等待中', value: summary.records.pending, color: '#a1a1aa' },
    { label: '超时', value: summary.records.timeout, color: '#d97706' },
  ];

  return (
    <div className="page-stack monitor-page">
      <div className="monitor-hero">
        <div>
          <span className="monitor-kicker">Prometheus exporter</span>
          <h2>{summary.exporter}</h2>
          <p>当前页面展示后端聚合后的运行快照，每 10 秒自动刷新。</p>
        </div>
        <Space>
          <Tag color="default">{summary.runtime.last_collected_msg}</Tag>
          <Button icon={<ReloadOutlined />} loading={loading} onClick={loadSummary}>刷新</Button>
        </Space>
      </div>

      <div className="metric-grid four">
        <Metric title="活跃任务" value={summary.timers.active} icon={<ClockCircleOutlined />} />
        <Metric title="执行总数" value={summary.runtime.exec_total} icon={<DatabaseOutlined />} />
        <Metric title="触发次数" value={summary.runtime.trigger_total} icon={<ThunderboltOutlined />} />
        <Metric title="队列任务" value={summary.redis.queue_items} icon={<CloudServerOutlined />} />
      </div>

      <div className="monitor-grid">
        <section className="surface monitor-panel">
          <div className="panel-heading">
            <h3>执行质量</h3>
            <span>{summary.runtime.exec_total} total</span>
          </div>
          <div className="success-ring">
            <Progress type="circle" percent={successPercent} size={132} strokeColor="#16a34a" />
            <div>
              <strong>{summary.runtime.avg_duration_ms.toFixed(1)} ms</strong>
              <span>平均执行耗时</span>
            </div>
          </div>
          <div className="split-stats">
            <SmallStat label="成功" value={summary.runtime.exec_success} tone="success" />
            <SmallStat label="失败" value={summary.runtime.exec_failed} tone="danger" />
          </div>
        </section>

        <section className="surface monitor-panel">
          <div className="panel-heading">
            <h3>成功率趋势</h3>
            <span>{history.length} samples</span>
          </div>
          <LineChart
            points={history.map((item) => item.successRate)}
            labels={history.map((item) => item.label)}
            suffix="%"
            color="#16a34a"
          />
        </section>

        <section className="surface monitor-panel wide">
          <div className="panel-heading">
            <h3>吞吐趋势</h3>
            <span>exec total / avg duration</span>
          </div>
          <div className="dual-chart">
            <LineChart
              title="累计执行"
              points={history.map((item) => item.execTotal)}
              labels={history.map((item) => item.label)}
              color="#18181b"
            />
            <LineChart
              title="平均耗时"
              points={history.map((item) => item.avgDuration)}
              labels={history.map((item) => item.label)}
              suffix="ms"
              color="#2563eb"
            />
          </div>
        </section>

        <section className="surface monitor-panel wide">
          <div className="panel-heading">
            <h3>Redis 调度面趋势</h3>
            <span>queue items / lock keys</span>
          </div>
          <div className="dual-chart">
            <LineChart
              title="队列任务"
              points={history.map((item) => item.queueItems)}
              labels={history.map((item) => item.label)}
              color="#d97706"
            />
            <LineChart
              title="锁数量"
              points={history.map((item) => item.lockKeys)}
              labels={history.map((item) => item.label)}
              color="#71717a"
            />
          </div>
          <div className="redis-grid">
            <SmallStat label="队列 Key" value={summary.redis.queue_keys} />
            <SmallStat label="队列成员" value={summary.redis.queue_items} />
            <SmallStat label="锁 Key" value={summary.redis.lock_keys} />
            <SmallStat label="分桶 Key" value={summary.redis.bucket_keys} />
          </div>
        </section>

        <section className="surface monitor-panel wide">
          <div className="panel-heading">
            <h3>执行结果分布</h3>
            <span>{summary.records.total} records</span>
          </div>
          <div className="distribution-bar">
            {recordSegments.map((item) => (
              <span
                key={item.label}
                style={{
                  width: `${Math.max((item.value / recordTotal) * 100, item.value > 0 ? 2 : 0)}%`,
                  background: item.color,
                }}
              />
            ))}
          </div>
          <div className="legend-grid">
            {recordSegments.map((item) => (
              <div key={item.label} className="legend-item">
                <i style={{ background: item.color }} />
                <span>{item.label}</span>
                <strong>{numberFormat.format(item.value)}</strong>
              </div>
            ))}
          </div>
        </section>
      </div>
    </div>
  );
};

interface MetricProps {
  title: string;
  value: number;
  icon: React.ReactNode;
}

const Metric: React.FC<MetricProps> = ({ title, value, icon }) => (
  <div className="metric-tile monitor-metric">
    <span>{title}</span>
    <strong>{numberFormat.format(value)}</strong>
    <i>{icon}</i>
  </div>
);

const SmallStat: React.FC<{ label: string; value: number; tone?: 'success' | 'danger' }> = ({ label, value, tone }) => (
  <div className="small-stat">
    <span>{label}</span>
    <strong className={tone === 'success' ? 'success-text' : tone === 'danger' ? 'danger-text' : undefined}>
      {numberFormat.format(value)}
    </strong>
  </div>
);

interface LineChartProps {
  title?: string;
  points: number[];
  labels: string[];
  color: string;
  suffix?: string;
}

const LineChart: React.FC<LineChartProps> = ({ title, points, labels, color, suffix = '' }) => {
  const normalized = points.length > 0 ? points : [0];
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
        <strong>{latest.toFixed(suffix === '%' || suffix === 'ms' ? 1 : 0)}{suffix}</strong>
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

export default Monitoring;
