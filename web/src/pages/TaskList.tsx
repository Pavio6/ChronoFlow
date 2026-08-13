import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Button,
  Descriptions,
  Drawer,
  Dropdown,
  Empty,
  Input,
  message,
  Modal,
  Select,
  Skeleton,
  Table,
  Tag,
  Tooltip,
} from 'antd';
import {
  DeleteOutlined,
  EyeOutlined,
  MoreOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
  SearchOutlined,
} from '@ant-design/icons';
import type { MenuProps, TableProps } from 'antd';
import LoadError from '../components/LoadError';
import type { TimerDefinition, TimerListParams, TimerStatus } from '../types';
import { activateTimer, deactivateTimer, deleteTimer, getTimer, getTimers } from '../api/tasks';
import { TIMER_STATUS_CONFIG } from '../utils/constants';
import { formatDateTime } from '../utils/date';
import {
  getTimerListCache,
  hasFreshTimerListCache,
  saveTimerListCache,
} from '../utils/pageCache';

const TaskList = () => {
  const cachedList = getTimerListCache();
  const [timers, setTimers] = useState<TimerDefinition[]>(() => cachedList?.data.items ?? []);
  const [total, setTotal] = useState(() => cachedList?.data.total ?? 0);
  const [loading, setLoading] = useState(() => cachedList === null);
  const [error, setError] = useState('');
  const [params, setParams] = useState<TimerListParams>(
    () => cachedList?.params ?? { page: 1, page_size: 20 },
  );
  const [keyword, setKeyword] = useState(() => cachedList?.params.keyword ?? '');
  const [appFilter, setAppFilter] = useState(() => cachedList?.params.app ?? '');
  const [reloadKey, setReloadKey] = useState(0);
  const [refreshing, setRefreshing] = useState(false);
  const [pendingTimerId, setPendingTimerId] = useState<number | null>(null);
  const [detailOpen, setDetailOpen] = useState(false);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detail, setDetail] = useState<TimerDefinition | null>(null);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      const nextKeyword = keyword.trim();
      const nextApp = appFilter.trim();
      if ((params.keyword ?? '') === nextKeyword && (params.app ?? '') === nextApp) return;
      setLoading(true);
      setError('');
      setParams((current) => ({
        ...current,
        keyword: nextKeyword || undefined,
        app: nextApp || undefined,
        page: 1,
      }));
    }, 300);
    return () => window.clearTimeout(timer);
  }, [appFilter, keyword, params.app, params.keyword]);

  useEffect(() => {
    if (reloadKey === 0 && hasFreshTimerListCache(params)) {
      return;
    }

    const controller = new AbortController();
    getTimers(params, controller.signal)
      .then((result) => {
        setTimers(result.items ?? []);
        setTotal(result.total);
        saveTimerListCache(result, params);
      })
      .catch((requestError: Error) => {
        if (!controller.signal.aborted) setError(requestError.message || '无法加载定时任务');
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
      setDetail(await getTimer(id));
    } catch (requestError) {
      message.error((requestError as Error).message || '无法加载任务详情');
      setDetailOpen(false);
    } finally {
      setDetailLoading(false);
    }
  }, []);

  const runAction = useCallback(async (
    timer: TimerDefinition,
    action: 'activate' | 'deactivate' | 'delete',
  ) => {
    setPendingTimerId(timer.id);
    try {
      if (action === 'activate') await activateTimer(timer.id);
      if (action === 'deactivate') await deactivateTimer(timer.id);
      if (action === 'delete') await deleteTimer(timer.id);
      message.success(action === 'activate' ? '任务已启用' : action === 'deactivate' ? '任务已停用' : '任务已删除');
      refresh();
    } catch (requestError) {
      message.error((requestError as Error).message || '操作失败');
    } finally {
      setPendingTimerId(null);
    }
  }, [refresh]);

  const confirmDelete = useCallback((timer: TimerDefinition) => {
    Modal.confirm({
      title: '删除定时任务',
      content: `确定删除“${timer.name}”吗？删除后不能恢复`,
      okText: '删除',
      cancelText: '取消',
      okButtonProps: { danger: true },
      onOk: () => runAction(timer, 'delete'),
    });
  }, [runAction]);

  const columns = useMemo<TableProps<TimerDefinition>['columns']>(() => [
    {
      title: '任务',
      key: 'task',
      width: 240,
      render: (_, timer) => (
        <button className="table-primary-link" type="button" onClick={() => openDetail(timer.id)}>
          <strong>{timer.name}</strong>
          <span>{timer.app}</span>
        </button>
      ),
    },
    {
      title: '触发计划',
      dataIndex: 'cron_expr',
      width: 150,
      render: (cron: string) => <code className="inline-code">{cron}</code>,
    },
    {
      title: '下一次触发',
      dataIndex: 'next_fire_at',
      width: 180,
      render: formatDateTime,
    },
    {
      title: '错过策略',
      dataIndex: 'misfire_policy',
      width: 120,
      render: (policy: TimerDefinition['misfire_policy']) => (
        <span className="secondary-value">{policy.replace('_', ' ')}</span>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      render: (status: TimerStatus) => {
        const config = TIMER_STATUS_CONFIG[status];
        return <Tag className={`status-tag status-tag-${status.toLowerCase()}`}>{config.label}</Tag>;
      },
    },
    {
      title: '',
      key: 'actions',
      width: 56,
      fixed: 'right',
      render: (_, timer) => {
        const actionItems: MenuProps['items'] = [
          { key: 'view', icon: <EyeOutlined />, label: '查看详情' },
          timer.status === 'ACTIVE'
            ? { key: 'deactivate', icon: <PauseCircleOutlined />, label: '停用任务' }
            : { key: 'activate', icon: <PlayCircleOutlined />, label: '启用任务' },
          { type: 'divider' },
          { key: 'delete', icon: <DeleteOutlined />, label: '删除任务', danger: true },
        ];
        return (
          <Dropdown
            trigger={['click']}
            menu={{
              items: actionItems,
              onClick: ({ key }) => {
                if (key === 'view') void openDetail(timer.id);
                if (key === 'activate') void runAction(timer, 'activate');
                if (key === 'deactivate') void runAction(timer, 'deactivate');
                if (key === 'delete') confirmDelete(timer);
              },
            }}
          >
            <Tooltip title="更多操作">
              <Button
                type="text"
                icon={<MoreOutlined />}
                loading={pendingTimerId === timer.id}
                aria-label={`${timer.name}的更多操作`}
              />
            </Tooltip>
          </Dropdown>
        );
      },
    },
  ], [confirmDelete, openDetail, pendingTimerId, runAction]);

  const detailItems = detail ? [
    { key: 'name', label: '名称', children: detail.name },
    { key: 'app', label: '应用', children: detail.app },
    { key: 'status', label: '状态', children: <Tag className={`status-tag status-tag-${detail.status.toLowerCase()}`}>{TIMER_STATUS_CONFIG[detail.status].label}</Tag> },
    { key: 'cron', label: 'Cron', children: <code className="inline-code">{detail.cron_expr}</code> },
    { key: 'next', label: '下次触发', children: formatDateTime(detail.next_fire_at) },
    { key: 'policy', label: '错过策略', children: detail.misfire_policy },
    { key: 'catchup', label: '补偿上限', children: detail.max_catch_up },
    { key: 'method', label: '回调方法', children: detail.callback_method },
    { key: 'url', label: '回调地址', children: <span className="breakable-value">{detail.callback_url}</span> },
    { key: 'body', label: '请求体', children: <pre className="code-block">{detail.callback_body || '—'}</pre> },
    { key: 'created', label: '创建时间', children: formatDateTime(detail.created_at) },
    { key: 'updated', label: '更新时间', children: formatDateTime(detail.updated_at) },
  ] : [];

  return (
    <div className="page-stack">
      {error && <LoadError message={error} onRetry={refresh} />}
      <section className="surface" aria-label="定时任务列表">
        <div className="toolbar">
          <div className="toolbar-filters">
            <Input
              value={keyword}
              placeholder="搜索任务名称"
              prefix={<SearchOutlined />}
              allowClear
              onChange={(event) => setKeyword(event.target.value)}
              className="toolbar-search"
            />
            <Input
              value={appFilter}
              placeholder="筛选应用"
              allowClear
              onChange={(event) => setAppFilter(event.target.value)}
              className="toolbar-app"
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
              options={Object.entries(TIMER_STATUS_CONFIG)
                .filter(([status]) => status !== 'DELETED')
                .map(([value, config]) => ({ value, label: config.label }))}
              className="toolbar-select"
            />
          </div>
          <Tooltip title="刷新">
            <Button icon={<ReloadOutlined />} onClick={refresh} loading={refreshing}>
              刷新
            </Button>
          </Tooltip>
        </div>

        <Table<TimerDefinition>
          columns={columns}
          dataSource={timers}
          rowKey="id"
          loading={loading}
          scroll={{ x: 900 }}
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
          locale={{ emptyText: !loading && <Empty description="没有符合条件的定时任务" /> }}
        />
      </section>

      <Drawer
        title="任务详情"
        open={detailOpen}
        size={520}
        onClose={() => setDetailOpen(false)}
        destroyOnHidden
      >
        {detailLoading ? (
          <Skeleton active paragraph={{ rows: 9 }} />
        ) : (
          <Descriptions column={1} size="small" items={detailItems} className="detail-list" />
        )}
      </Drawer>
    </div>
  );
};

export default TaskList;
