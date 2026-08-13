import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Button,
  Descriptions,
  Drawer,
  Empty,
  Input,
  message,
  Select,
  Skeleton,
  Table,
  Tag,
  Tooltip,
} from 'antd';
import { CopyOutlined, EyeOutlined, ReloadOutlined, SearchOutlined } from '@ant-design/icons';
import type { TableProps } from 'antd';
import LoadError from '../components/LoadError';
import { getExecution, getExecutions } from '../api/executions';
import type { ExecutionListParams, ExecutionStatus, TimerExecution } from '../types';
import { EXECUTION_STATUS_CONFIG } from '../utils/constants';
import { formatDateTime } from '../utils/date';
import {
  getExecutionListCache,
  hasFreshExecutionListCache,
  saveExecutionListCache,
} from '../utils/pageCache';

const ExecutionList = () => {
  const cachedList = getExecutionListCache();
  const [executions, setExecutions] = useState<TimerExecution[]>(() => cachedList?.data.items ?? []);
  const [total, setTotal] = useState(() => cachedList?.data.total ?? 0);
  const [loading, setLoading] = useState(() => cachedList === null);
  const [error, setError] = useState('');
  const [params, setParams] = useState<ExecutionListParams>(
    () => cachedList?.params ?? { page: 1, page_size: 20 },
  );
  const [timerName, setTimerName] = useState(() => cachedList?.params.timer_name ?? '');
  const [reloadKey, setReloadKey] = useState(0);
  const [refreshing, setRefreshing] = useState(false);
  const [detailOpen, setDetailOpen] = useState(false);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detail, setDetail] = useState<TimerExecution | null>(null);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      const nextName = timerName.trim();
      if ((params.timer_name ?? '') === nextName) return;
      setLoading(true);
      setError('');
      setParams((current) => ({ ...current, timer_name: nextName || undefined, page: 1 }));
    }, 300);
    return () => window.clearTimeout(timer);
  }, [params.timer_name, timerName]);

  useEffect(() => {
    if (reloadKey === 0 && hasFreshExecutionListCache(params)) {
      return;
    }

    const controller = new AbortController();
    getExecutions(params, controller.signal)
      .then((result) => {
        setExecutions(result.items ?? []);
        setTotal(result.total);
        saveExecutionListCache(result, params);
      })
      .catch((requestError: Error) => {
        if (!controller.signal.aborted) setError(requestError.message || '无法加载执行记录');
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setLoading(false);
          setRefreshing(false);
        }
      });
    return () => controller.abort();
  }, [params, reloadKey]);

  const refresh = useCallback(() => {
    setError('');
    setRefreshing(true);
    setReloadKey((current) => current + 1);
  }, []);

  const openDetail = useCallback(async (id: number) => {
    setDetailOpen(true);
    setDetail(null);
    setDetailLoading(true);
    try {
      setDetail(await getExecution(id));
    } catch (requestError) {
      message.error((requestError as Error).message || '无法加载执行详情');
      setDetailOpen(false);
    } finally {
      setDetailLoading(false);
    }
  }, []);

  const copyText = useCallback(async (value: string) => {
    try {
      await navigator.clipboard.writeText(value);
      message.success('已复制');
    } catch {
      message.error('复制失败，请手动选择文本');
    }
  }, []);

  const columns = useMemo<TableProps<TimerExecution>['columns']>(() => [
    {
      title: '任务',
      key: 'timer',
      width: 220,
      render: (_, execution) => (
        <button className="table-primary-link" type="button" onClick={() => openDetail(execution.id)}>
          <strong>{execution.timer_name || `Timer #${execution.timer_id}`}</strong>
          <span>Execution #{execution.id}</span>
        </button>
      ),
    },
    {
      title: '计划触发时间',
      dataIndex: 'scheduled_at',
      width: 180,
      render: formatDateTime,
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 110,
      render: (status: ExecutionStatus) => {
        const config = EXECUTION_STATUS_CONFIG[status];
        return <Tag className={`status-tag status-tag-${status.toLowerCase()}`}>{config.label}</Tag>;
      },
    },
    {
      title: '尝试次数',
      key: 'attempt',
      width: 100,
      render: (_, execution) => `${execution.attempt} / ${execution.max_attempts}`,
    },
    {
      title: '响应码',
      dataIndex: 'response_code',
      width: 90,
      render: (code: number) => code || '—',
    },
    {
      title: '耗时',
      dataIndex: 'duration_ms',
      width: 100,
      render: (duration: number) => duration > 0 ? `${duration} ms` : '—',
    },
    {
      title: '完成时间',
      dataIndex: 'finished_at',
      width: 180,
      render: formatDateTime,
    },
    {
      title: '',
      key: 'action',
      width: 56,
      fixed: 'right',
      render: (_, execution) => (
        <Tooltip title="查看详情">
          <Button
            type="text"
            icon={<EyeOutlined />}
            aria-label={`查看 Execution ${execution.id}`}
            onClick={() => void openDetail(execution.id)}
          />
        </Tooltip>
      ),
    },
  ], [openDetail]);

  const codeContent = (value: string) => (
    <div className="code-content">
      <Button
        type="text"
        size="small"
        icon={<CopyOutlined />}
        aria-label="复制内容"
        onClick={() => void copyText(value)}
      />
      <pre className="code-block">{value}</pre>
    </div>
  );

  const detailItems = detail ? [
    { key: 'id', label: 'Execution ID', children: detail.id },
    { key: 'timer', label: '定时任务', children: detail.timer_name || `Timer #${detail.timer_id}` },
    { key: 'status', label: '状态', children: <Tag className={`status-tag status-tag-${detail.status.toLowerCase()}`}>{EXECUTION_STATUS_CONFIG[detail.status].label}</Tag> },
    { key: 'attempt', label: '尝试次数', children: `${detail.attempt} / ${detail.max_attempts}` },
    { key: 'scheduled', label: '计划触发', children: formatDateTime(detail.scheduled_at) },
    { key: 'next', label: '下次重试', children: formatDateTime(detail.next_attempt_at) },
    { key: 'started', label: '开始时间', children: formatDateTime(detail.started_at) },
    { key: 'finished', label: '完成时间', children: formatDateTime(detail.finished_at) },
    { key: 'code', label: '响应码', children: detail.response_code || '—' },
    { key: 'duration', label: '执行耗时', children: detail.duration_ms > 0 ? `${detail.duration_ms} ms` : '—' },
    { key: 'created', label: '创建时间', children: formatDateTime(detail.created_at) },
    { key: 'updated', label: '更新时间', children: formatDateTime(detail.updated_at) },
    ...(detail.response_body ? [{ key: 'response', label: '响应内容', children: codeContent(detail.response_body) }] : []),
    ...(detail.error_message ? [{ key: 'error', label: '错误信息', children: codeContent(detail.error_message) }] : []),
  ] : [];

  return (
    <div className="page-stack">
      {error && <LoadError message={error} onRetry={refresh} />}
      <section className="surface" aria-label="执行记录列表">
        <div className="toolbar">
          <div className="toolbar-filters">
            <Input
              value={timerName}
              placeholder="搜索任务名称"
              prefix={<SearchOutlined />}
              allowClear
              onChange={(event) => setTimerName(event.target.value)}
              className="toolbar-search"
            />
            <Select
              value={params.status}
              placeholder="全部状态"
              allowClear
              onChange={(status) => {
                setLoading(true);
                setError('');
                setParams((current) => ({ ...current, status, page: 1 }));
              }}
              options={Object.entries(EXECUTION_STATUS_CONFIG).map(([value, config]) => ({
                value,
                label: config.label,
              }))}
              className="toolbar-select"
            />
          </div>
          <Tooltip title="刷新">
            <Button icon={<ReloadOutlined />} onClick={refresh} loading={refreshing}>
              刷新
            </Button>
          </Tooltip>
        </div>

        <Table<TimerExecution>
          columns={columns}
          dataSource={executions}
          rowKey="id"
          loading={loading}
          scroll={{ x: 1040 }}
          pagination={{
            current: params.page,
            pageSize: params.page_size,
            total,
            showSizeChanger: true,
            pageSizeOptions: [10, 20, 50],
            showTotal: (count) => `共 ${count} 条`,
            onChange: (page, pageSize) => {
              setLoading(true);
              setError('');
              setParams((current) => ({ ...current, page, page_size: pageSize }));
            },
          }}
          locale={{ emptyText: !loading && <Empty description="没有符合条件的执行记录" /> }}
        />
      </section>

      <Drawer
        title="执行详情"
        open={detailOpen}
        size={560}
        onClose={() => setDetailOpen(false)}
        destroyOnHidden
      >
        {detailLoading ? (
          <Skeleton active paragraph={{ rows: 10 }} />
        ) : (
          <Descriptions column={1} size="small" items={detailItems} className="detail-list" />
        )}
      </Drawer>
    </div>
  );
};

export default ExecutionList;
