import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Button, Empty, Select, Space, Tag } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';
import { getMonitoringHistory, getMonitoringSummary } from '../api/monitoring';
import type { MonitoringHistory, MonitoringPoint, MonitoringSummary } from '../types';

const rangeOptions = [
  { label: '最近 15 分钟', value: 15 },
  { label: '最近 1 小时', value: 60 },
  { label: '最近 6 小时', value: 360 },
  { label: '最近 24 小时', value: 1440 },
];

const Monitoring: React.FC = () => {
  const [historyLoading, setHistoryLoading] = useState(false);
  const [history, setHistory] = useState<MonitoringHistory | null>(null);
  const [summary, setSummary] = useState<MonitoringSummary | null>(null);
  const [historyUnavailable, setHistoryUnavailable] = useState(false);
  const [rangeMinutes, setRangeMinutes] = useState(60);
  const historyRequestID = useRef(0);

  const loadHistory = useCallback(async () => {
    const requestID = ++historyRequestID.current;
    setHistoryLoading(true);
    try {
      const [data, current] = await Promise.all([
        getMonitoringHistory(rangeMinutes),
        getMonitoringSummary(),
      ]);
      if (requestID !== historyRequestID.current) {
        return;
      }
      setHistory(data);
      setSummary(current);
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
    const initialLoad = window.setTimeout(loadHistory, 0);
    const timer = window.setInterval(loadHistory, 10000);
    return () => {
      window.clearTimeout(initialLoad);
      window.clearInterval(timer);
    };
  }, [loadHistory]);

  const historySeries = history?.series;

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
            <h3>执行状态分布</h3>
            <span>当前记录</span>
          </div>
          <StatusDonut records={summary?.records} />
        </section>

        <section className="surface monitor-panel">
          <div className="panel-heading">
            <h3>执行耗时</h3>
            <span>P95 duration</span>
          </div>
          <TrendLineChart
            title="P95 耗时"
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
          <BarTrendChart
            title="超期或卡住记录"
            points={values(historySeries?.abnormal_records)}
            labels={labels(historySeries?.abnormal_records)}
            color="#d97706"
          />
        </section>

        <section className="surface monitor-panel">
          <div className="panel-heading">
            <h3>Redis 队列</h3>
            <span>queue status</span>
          </div>
          <QueueStat redis={summary?.redis} />
        </section>

      </div>
    </div>
  );
};

interface StatusDonutProps {
  records?: MonitoringSummary['records'];
}

const StatusDonut: React.FC<StatusDonutProps> = ({ records }) => {
  if (!records) {
    return <ChartEmpty />;
  }
  const segments = [
    { key: 'success', label: '成功', value: records.success, color: '#16a34a' },
    { key: 'failed', label: '失败', value: records.failed, color: '#dc2626' },
    { key: 'running', label: '运行中', value: records.running, color: '#2563eb' },
    { key: 'pending', label: '等待中', value: records.pending, color: '#a1a1aa' },
  ];
  const total = records.total || 1;
  const radius = 52;
  const circumference = 2 * Math.PI * radius;
  let accumulated = 0;

  return (
    <div className="donut-card">
      <svg className="donut-svg" viewBox="0 0 160 160" role="img" aria-label="执行状态分布">
        <circle className="donut-track" cx="80" cy="80" r={radius} />
        {segments.map((segment) => {
          const length = (segment.value / total) * circumference;
          const offset = -accumulated;
          accumulated += length;
          return (
            <circle
              key={segment.key}
              className="donut-segment"
              cx="80"
              cy="80"
              r={radius}
              stroke={segment.color}
              strokeDasharray={`${length} ${circumference - length}`}
              strokeDashoffset={offset}
            />
          );
        })}
        <text className="donut-total" x="80" y="77">{records.total}</text>
        <text className="donut-label" x="80" y="96">总执行数</text>
      </svg>
      <div className="donut-legend">
        {segments.map((segment) => (
          <div key={segment.key}>
            <span style={{ backgroundColor: segment.color }} />
            <label>{segment.label}</label>
            <strong>{segment.value}</strong>
          </div>
        ))}
      </div>
    </div>
  );
};

const QueueStat: React.FC<{ redis?: MonitoringSummary['redis'] }> = ({ redis }) => {
  if (!redis) {
    return <ChartEmpty />;
  }
  const isClear = redis.queue_items === 0;
  return (
    <div className="queue-stat-card">
      <div className={`queue-status ${isClear ? 'healthy' : 'busy'}`}>
        <span />
        {isClear ? '队列畅通' : '处理中'}
      </div>
      <strong>{redis.queue_items}</strong>
      <label>待触发队列任务</label>
      <div className="queue-meta">
        <div><span>队列键</span><b>{redis.queue_keys}</b></div>
        <div><span>分桶键</span><b>{redis.bucket_keys}</b></div>
        <div><span>锁数量</span><b>{redis.lock_keys}</b></div>
      </div>
    </div>
  );
};

interface TrendChartProps {
  title?: string;
  points: number[];
  labels: string[];
  color: string;
  suffix?: string;
  precision?: number;
}

const TrendLineChart: React.FC<TrendChartProps> = ({ title, points, labels, color, suffix = '', precision }) => {
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null);
  if (points.length === 0) {
    return <ChartEmpty title={title} />;
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
  const latestValue = normalized[normalized.length - 1] || 0;
  const activeIndex = hoveredIndex ?? normalized.length - 1;
  const activeValue = normalized[activeIndex] || 0;
  const activePoint = coords[activeIndex];
  const displayPrecision = precision ?? (suffix === '%' || suffix === 'ms' ? 1 : 0);

  const handlePointerMove = (event: React.PointerEvent<SVGSVGElement>) => {
    const bounds = event.currentTarget.getBoundingClientRect();
    const x = ((event.clientX - bounds.left) / bounds.width) * width;
    const index = step === 0 ? 0 : Math.round((x - padding) / step);
    setHoveredIndex(clampIndex(index, normalized.length));
  };

  return (
    <div className="line-card">
      <div className="line-card-head">
        <span>{title || '趋势'}</span>
        <strong>{`${latestValue.toFixed(displayPrecision)}${suffix}`}</strong>
      </div>
      <svg
        className="interactive-chart"
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-label={title || '趋势图'}
        onPointerMove={handlePointerMove}
        onPointerLeave={() => setHoveredIndex(null)}
      >
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
        {hoveredIndex !== null && (
          <>
            <line className="chart-crosshair" x1={activePoint.x} y1={padding} x2={activePoint.x} y2={height - padding} />
            <circle className="chart-active-point" cx={activePoint.x} cy={activePoint.y} r="6" fill="#ffffff" stroke={color} strokeWidth="3" />
          </>
        )}
      </svg>
      <div className="chart-labels">
        <span>{labels[0] || '--'}</span>
        {hoveredIndex !== null && <strong>{`${labels[activeIndex]} | ${activeValue.toFixed(displayPrecision)}${suffix}`}</strong>}
        <span>{labels[labels.length - 1] || '--'}</span>
      </div>
    </div>
  );
};

const BarTrendChart: React.FC<TrendChartProps> = ({ title, points, labels, color, suffix = '', precision }) => {
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null);
  if (points.length === 0) {
    return <ChartEmpty title={title} />;
  }
  const max = Math.max(...points, 1);
  const width = 420;
  const height = 140;
  const padding = 14;
  const gap = 7;
  const barWidth = Math.max(5, Math.min(34, (width - padding * 2 - gap * (points.length - 1)) / points.length));
  const usedWidth = barWidth * points.length + gap * (points.length - 1);
  const startX = width - padding - usedWidth;
  const latestValue = points[points.length - 1] || 0;
  const activeIndex = hoveredIndex ?? points.length - 1;
  const activeValue = points[activeIndex] || 0;

  const handlePointerMove = (event: React.PointerEvent<SVGSVGElement>) => {
    const bounds = event.currentTarget.getBoundingClientRect();
    const x = ((event.clientX - bounds.left) / bounds.width) * width;
    const index = Math.round((x - startX - barWidth / 2) / (barWidth + gap));
    setHoveredIndex(clampIndex(index, points.length));
  };

  return (
    <div className="line-card bar-card">
      <div className="line-card-head">
        <span>{title || '趋势'}</span>
        <strong>{`${latestValue.toFixed(precision ?? 0)}${suffix}`}</strong>
      </div>
      <svg
        className="interactive-chart"
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-label={title || '柱状趋势图'}
        onPointerMove={handlePointerMove}
        onPointerLeave={() => setHoveredIndex(null)}
      >
        <polyline className="grid-line" points={`${padding},${height - padding} ${width - padding},${height - padding}`} />
        {points.map((value, index) => {
          const barHeight = Math.max(value === 0 ? 3 : 8, (value / max) * (height - padding * 2));
          return (
            <rect
              key={`${value}-${index}`}
              x={startX + index * (barWidth + gap)}
              y={height - padding - barHeight}
              width={barWidth}
              height={barHeight}
              rx="3"
              fill={color}
              opacity={0.38 + (index / Math.max(points.length, 1)) * 0.52}
              className={hoveredIndex === index ? 'is-active' : undefined}
            />
          );
        })}
      </svg>
      <div className="chart-labels">
        <span>{labels[0] || '--'}</span>
        {hoveredIndex !== null && <strong>{`${labels[activeIndex]} | ${activeValue.toFixed(precision ?? 0)}${suffix}`}</strong>}
        <span>{labels[labels.length - 1] || '--'}</span>
      </div>
    </div>
  );
};

const ChartEmpty: React.FC<{ title?: string }> = ({ title }) => (
  <div className="line-card chart-empty">
    <div className="line-card-head">
      <span>{title || '当前分布'}</span>
      <strong>--</strong>
    </div>
    <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无监控数据" />
  </div>
);

const values = (points?: MonitoringPoint[]): number[] => points?.map((item) => item.value) || [];

const labels = (points?: MonitoringPoint[]): string[] => points?.map((item) => (
  new Date(item.timestamp * 1000).toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit' })
)) || [];

const clampIndex = (index: number, count: number): number => Math.max(0, Math.min(index, count - 1));

export default Monitoring;
