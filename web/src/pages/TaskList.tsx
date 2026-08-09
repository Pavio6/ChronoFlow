import React, { useState, useEffect, useCallback } from 'react';
import {
  Table,
  Button,
  Space,
  Tag,
  Input,
  Select,
  message,
  Popconfirm,
  Tooltip,
  Empty,
} from 'antd';
import {
  PlusOutlined,
  SearchOutlined,
  PlayCircleOutlined,
  PauseCircleOutlined,
  DeleteOutlined,
  ReloadOutlined,
} from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import type { ColumnsType } from 'antd/es/table';
import type { TimerDefinition, TimerStatus, TimerListParams } from '../types';
import { getTimers, deleteTimer, activateTimer, deactivateTimer } from '../api/tasks';
import { TIMER_STATUS_CONFIG } from '../utils/constants';

const TaskList: React.FC = () => {
  const navigate = useNavigate();
  const [loading, setLoading] = useState(false);
  const [timers, setTimers] = useState<TimerDefinition[]>([]);
  const [total, setTotal] = useState(0);
  const [params, setParams] = useState<TimerListParams>({ page: 1, page_size: 10 });
  const [stats, setStats] = useState({ total: 0, active: 0, inactive: 0 });

  const loadTimers = useCallback(async () => {
    setLoading(true);
    try {
      const res = await getTimers(params);
      const items = res.items || [];
      setTimers(items);
      setTotal(res.total);
      setStats(res.stats);
    } catch (error: unknown) {
      message.error((error as Error).message || '加载失败');
    } finally {
      setLoading(false);
    }
  }, [params]);

  useEffect(() => {
    const run = async () => { await loadTimers(); };
    run();
  }, [loadTimers]);

  const handleAction = async (action: () => Promise<unknown>, msg: string) => {
    try {
      await action();
      message.success(msg);
      loadTimers();
    } catch (error: unknown) {
      message.error((error as Error).message || '操作失败');
    }
  };

  const columns: ColumnsType<TimerDefinition> = [
    { title: 'ID', dataIndex: 'id', width: 72, render: (id: number) => <span className="muted">#{id}</span> },
    { title: '应用', dataIndex: 'app', width: 140, render: (app: string) => <span className="mono-chip">{app}</span> },
    { title: '名称', dataIndex: 'name', width: 180, ellipsis: true, render: (name: string) => <strong>{name}</strong> },
    {
      title: 'Cron',
      dataIndex: 'cron_expr',
      width: 140,
      render: (text: string) => <code className="inline-code">{text}</code>,
    },
    {
      title: '下次触发',
      dataIndex: 'next_fire_at',
      width: 170,
      render: (value: string | null) => value ? new Date(value).toLocaleString('zh-CN') : '-',
    },
    { title: '回调地址', dataIndex: 'callback_url', width: 220, ellipsis: true },
    {
      title: '状态',
      dataIndex: 'status',
      width: 80,
      render: (status: TimerStatus) => {
        const config = TIMER_STATUS_CONFIG[status];
        return <Tag color={config?.color} className="status-tag">{config?.label || status}</Tag>;
      },
    },
    {
      title: '操作',
      key: 'action',
      width: 140,
      render: (_, record) => (
        <Space size={4}>
          {record.status === 'ACTIVE' ? (
            <Tooltip title="停用">
              <Button className="icon-button" type="text" size="small" icon={<PauseCircleOutlined />}
                onClick={() => handleAction(() => deactivateTimer(record.id), '停用成功')} />
            </Tooltip>
          ) : record.status === 'INACTIVE' ? (
            <Tooltip title="激活">
              <Button className="icon-button" type="text" size="small" icon={<PlayCircleOutlined />}
                onClick={() => handleAction(() => activateTimer(record.id), '激活成功')} />
            </Tooltip>
          ) : null}
          <Popconfirm title="确定删除？" onConfirm={() => handleAction(() => deleteTimer(record.id), '删除成功')}>
            <Tooltip title="删除">
              <Button className="icon-button" type="text" size="small" danger icon={<DeleteOutlined />} />
            </Tooltip>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div className="page-stack">
      <div className="metric-grid three">
        <div className="metric-tile">
          <span>总计</span>
          <strong>{stats.total}</strong>
        </div>
        <div className="metric-tile">
          <span>已激活</span>
          <strong className="success-text">{stats.active}</strong>
        </div>
        <div className="metric-tile">
          <span>未激活</span>
          <strong>{stats.inactive}</strong>
        </div>
      </div>

      <div className="surface">
        <div className="toolbar">
          <Space wrap>
          <Input
            placeholder="搜索名称"
            prefix={<SearchOutlined />}
            className="toolbar-control"
            onChange={(e) => setParams({ ...params, keyword: e.target.value, page: 1 })}
            allowClear
          />
          <Select
            placeholder="状态"
            className="toolbar-select"
            allowClear
            onChange={(value) => setParams({ ...params, status: value, page: 1 })}
            options={Object.entries(TIMER_STATUS_CONFIG).map(([k, v]) => ({ label: v.label, value: k }))}
          />
          </Space>
          <Space>
          <Button icon={<ReloadOutlined />} onClick={loadTimers}>刷新</Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate('/tasks/create')}>创建</Button>
          </Space>
        </div>

        <Table
          columns={columns}
          dataSource={timers}
          rowKey="id"
          loading={loading}
          size="middle"
          scroll={{ x: 1280 }}
          pagination={{
            current: params.page,
            pageSize: params.page_size,
            total,
            showSizeChanger: true,
            showTotal: (t) => `共 ${t} 条`,
            onChange: (p, ps) => setParams({ ...params, page: p, page_size: ps }),
          }}
          locale={{ emptyText: <Empty description="暂无数据" /> }}
        />
      </div>
    </div>
  );
};

export default TaskList;
