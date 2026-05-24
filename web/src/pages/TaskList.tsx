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
  Card,
  Row,
  Col,
  Statistic,
  Empty,
} from 'antd';
import {
  PlusOutlined,
  SearchOutlined,
  PlayCircleOutlined,
  PauseCircleOutlined,
  EditOutlined,
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
      setStats({
        total: res.total,
        active: items.filter(t => t.status === 'ACTIVE').length,
        inactive: items.filter(t => t.status === 'INACTIVE').length,
      });
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

  const handleAction = async (action: () => Promise<void>, msg: string) => {
    try {
      await action();
      message.success(msg);
      loadTimers();
    } catch (error: unknown) {
      message.error((error as Error).message || '操作失败');
    }
  };

  const columns: ColumnsType<TimerDefinition> = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '应用', dataIndex: 'app', width: 100 },
    { title: '名称', dataIndex: 'name', width: 160, ellipsis: true },
    {
      title: 'Cron',
      dataIndex: 'cron_expr',
      width: 140,
      render: (text: string) => <code style={{ fontSize: 12 }}>{text}</code>,
    },
    { title: '回调地址', dataIndex: 'callback_url', width: 220, ellipsis: true },
    {
      title: '状态',
      dataIndex: 'status',
      width: 80,
      render: (status: TimerStatus) => {
        const config = TIMER_STATUS_CONFIG[status];
        return <Tag color={config?.color}>{config?.label || status}</Tag>;
      },
    },
    { title: '超时', dataIndex: 'timeout', width: 60, render: (v: number) => `${v}s` },
    { title: '重试', dataIndex: 'max_retries', width: 60 },
    {
      title: '操作',
      key: 'action',
      width: 140,
      render: (_, record) => (
        <Space size={4}>
          {record.status === 'ACTIVE' ? (
            <Tooltip title="停用">
              <Button type="text" size="small" icon={<PauseCircleOutlined />}
                onClick={() => handleAction(() => deactivateTimer(record.id), '停用成功')} />
            </Tooltip>
          ) : record.status === 'INACTIVE' ? (
            <Tooltip title="激活">
              <Button type="text" size="small" icon={<PlayCircleOutlined />}
                onClick={() => handleAction(() => activateTimer(record.id), '激活成功')} />
            </Tooltip>
          ) : null}
          <Tooltip title="编辑">
            <Button type="text" size="small" icon={<EditOutlined />}
              onClick={() => navigate(`/tasks/edit/${record.id}`)} />
          </Tooltip>
          <Popconfirm title="确定删除？" onConfirm={() => handleAction(() => deleteTimer(record.id), '删除成功')}>
            <Tooltip title="删除">
              <Button type="text" size="small" danger icon={<DeleteOutlined />} />
            </Tooltip>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div>
      <Row gutter={16} style={{ marginBottom: 20 }}>
        <Col span={8}>
          <Card size="small"><Statistic title="总计" value={stats.total} /></Card>
        </Col>
        <Col span={8}>
          <Card size="small"><Statistic title="已激活" value={stats.active} valueStyle={{ color: '#3f8600' }} /></Card>
        </Col>
        <Col span={8}>
          <Card size="small"><Statistic title="未激活" value={stats.inactive} /></Card>
        </Col>
      </Row>

      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
        <Space>
          <Input
            placeholder="搜索名称"
            prefix={<SearchOutlined />}
            style={{ width: 200 }}
            onChange={(e) => setParams({ ...params, keyword: e.target.value, page: 1 })}
            allowClear
          />
          <Select
            placeholder="状态"
            style={{ width: 100 }}
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
  );
};

export default TaskList;
