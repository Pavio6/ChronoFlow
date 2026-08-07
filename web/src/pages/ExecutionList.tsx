import React, { useEffect, useState } from 'react';
import {
  Button,
  Descriptions,
  Empty,
  Input,
  message,
  Modal,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
} from 'antd';
import {
  CheckCircleOutlined,
  ClockCircleOutlined,
  CloseCircleOutlined,
  EyeOutlined,
  ReloadOutlined,
  SearchOutlined,
  SyncOutlined,
} from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import type {
  ExecutionListParams,
  ExecutionStatus,
  TimerExecution,
} from '../types';
import { getExecutions } from '../api/executions';
import { EXECUTION_STATUS_CONFIG } from '../utils/constants';

const statusIcon: Partial<Record<ExecutionStatus, React.ReactNode>> = {
  SUCCESS: <CheckCircleOutlined className="success-text" />,
  FAILED: <CloseCircleOutlined className="danger-text" />,
  RUNNING: <SyncOutlined spin className="info-text" />,
  PENDING: <ClockCircleOutlined className="muted" />,
  RETRY_WAIT: <ClockCircleOutlined className="muted" />,
};

const formatTime = (value: string | null) =>
  value ? new Date(value).toLocaleString('zh-CN') : '-';

const ExecutionList: React.FC = () => {
  const [loading, setLoading] = useState(true);
  const [executions, setExecutions] = useState<TimerExecution[]>([]);
  const [total, setTotal] = useState(0);
  const [params, setParams] = useState<ExecutionListParams>({
    page: 1,
    page_size: 10,
  });
  const [selected, setSelected] = useState<TimerExecution | null>(null);
  const [refreshToken, setRefreshToken] = useState(0);
  const [stats, setStats] = useState({
    total: 0,
    success: 0,
    failed: 0,
    running: 0,
  });

  useEffect(() => {
    let cancelled = false;
    getExecutions(params).then((result) => {
      if (cancelled) return;
      setExecutions(result.items || []);
      setTotal(result.total);
      setStats({
        total: result.total,
        success: result.stats.SUCCESS || 0,
        failed: result.stats.FAILED || 0,
        running: result.stats.RUNNING || 0,
      });
    }).catch((error: unknown) => {
      if (cancelled) return;
      message.error((error as Error).message || '加载失败');
    }).finally(() => {
      if (!cancelled) setLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, [params, refreshToken]);

  const updateParams = (next: ExecutionListParams) => {
    setLoading(true);
    setParams(next);
  };

  const refresh = () => {
    setLoading(true);
    setRefreshToken((current) => current + 1);
  };

  const columns: ColumnsType<TimerExecution> = [
    {
      title: 'ID',
      dataIndex: 'id',
      width: 72,
      render: (id: number) => <span className="muted">#{id}</span>,
    },
    {
      title: '定时器名称',
      dataIndex: 'timer_name',
      width: 180,
      ellipsis: true,
      render: (name: string | undefined, execution) => (
        <Tooltip title={name || `#${execution.timer_id}`}>
          <span>{name || `#${execution.timer_id}`}</span>
        </Tooltip>
      ),
    },
    {
      title: '计划时间',
      dataIndex: 'scheduled_at',
      width: 170,
      render: formatTime,
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 110,
      render: (status: ExecutionStatus) => {
        const config = EXECUTION_STATUS_CONFIG[status];
        return (
          <Space size={4}>
            {statusIcon[status]}
            <Tag color={config.color} className="status-tag">{config.label}</Tag>
          </Space>
        );
      },
    },
    {
      title: '尝试',
      width: 72,
      render: (_, execution) => `${execution.attempt}/${execution.max_attempts}`,
    },
    {
      title: '响应码',
      dataIndex: 'response_code',
      width: 82,
      render: (code: number) => code || '-',
    },
    {
      title: '耗时',
      dataIndex: 'duration_ms',
      width: 90,
      render: (duration: number) => duration ? `${duration}ms` : '-',
    },
    {
      title: '操作',
      key: 'action',
      width: 60,
      render: (_, execution) => (
        <Tooltip title="详情">
          <Button
            className="icon-button"
            type="text"
            size="small"
            icon={<EyeOutlined />}
            onClick={() => setSelected(execution)}
          />
        </Tooltip>
      ),
    },
  ];

  const detailItems = selected ? [
    { key: 'id', label: 'Execution ID', children: selected.id },
    { key: 'timer', label: '定时器', children: selected.timer_name || `#${selected.timer_id}` },
    { key: 'scheduled', label: '计划时间', children: formatTime(selected.scheduled_at) },
    {
      key: 'status',
      label: '状态',
      children: (
        <Tag color={EXECUTION_STATUS_CONFIG[selected.status].color}>
          {EXECUTION_STATUS_CONFIG[selected.status].label}
        </Tag>
      ),
    },
    { key: 'attempt', label: '尝试次数', children: `${selected.attempt}/${selected.max_attempts}` },
    { key: 'next', label: '下次重试', children: formatTime(selected.next_attempt_at) },
    { key: 'started', label: '开始时间', children: formatTime(selected.started_at) },
    { key: 'finished', label: '完成时间', children: formatTime(selected.finished_at) },
    { key: 'code', label: '响应码', children: selected.response_code || '-' },
    { key: 'duration', label: '耗时', children: selected.duration_ms ? `${selected.duration_ms}ms` : '-' },
    {
      key: 'response',
      label: '响应体',
      span: 2,
      children: <pre className="code-block">{selected.response_body || '-'}</pre>,
    },
    ...(selected.error_message ? [{
      key: 'error',
      label: '错误',
      span: 2,
      children: <span className="danger-text">{selected.error_message}</span>,
    }] : []),
  ] : [];

  return (
    <div className="page-stack">
      <div className="metric-grid four">
        <div className="metric-tile"><span>总计</span><strong>{stats.total}</strong></div>
        <div className="metric-tile"><span>成功</span><strong className="success-text">{stats.success}</strong></div>
        <div className="metric-tile"><span>失败</span><strong className="danger-text">{stats.failed}</strong></div>
        <div className="metric-tile"><span>运行中</span><strong>{stats.running}</strong></div>
      </div>

      <div className="surface">
        <div className="toolbar">
          <Space wrap>
            <Input
              placeholder="搜索定时器名称"
              prefix={<SearchOutlined />}
              className="toolbar-control"
              allowClear
              onChange={(event) => updateParams({
                ...params,
                timer_name: event.target.value,
                page: 1,
              })}
            />
            <Select
              placeholder="状态"
              className="toolbar-select"
              allowClear
              onChange={(status) => updateParams({ ...params, status, page: 1 })}
              options={Object.entries(EXECUTION_STATUS_CONFIG).map(([value, config]) => ({
                label: config.label,
                value,
              }))}
            />
          </Space>
          <Button icon={<ReloadOutlined />} onClick={refresh}>刷新</Button>
        </div>

        <Table
          columns={columns}
          dataSource={executions}
          rowKey="id"
          loading={loading}
          size="middle"
          scroll={{ x: 900 }}
          pagination={{
            current: params.page,
            pageSize: params.page_size,
            total,
            showSizeChanger: true,
            showTotal: (count) => `共 ${count} 条`,
            onChange: (page, pageSize) => updateParams({ ...params, page, page_size: pageSize }),
          }}
          locale={{ emptyText: <Empty description="暂无数据" /> }}
        />
      </div>

      <Modal
        title="执行详情"
        open={selected !== null}
        onCancel={() => setSelected(null)}
        footer={null}
        width={700}
        className="detail-modal"
      >
        {selected && <Descriptions bordered column={2} size="small" items={detailItems} />}
      </Modal>
    </div>
  );
};

export default ExecutionList;
