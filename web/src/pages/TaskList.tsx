import React, { useState, useEffect } from 'react';
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
  const [params, setParams] = useState<TimerListParams>({
    page: 1,
    page_size: 10,
  });

  // 统计数据
  const [stats, setStats] = useState({
    total: 0,
    active: 0,
    inactive: 0,
  });

  // 加载定时器列表
  const loadTimers = async () => {
    setLoading(true);
    try {
      const res = await getTimers(params);
      setTimers(res.items || []);
      setTotal(res.total);

      // 计算统计数据
      const allTimers = res.items || [];
      setStats({
        total: res.total,
        active: allTimers.filter(t => t.status === 'ACTIVE').length,
        inactive: allTimers.filter(t => t.status === 'INACTIVE').length,
      });
    } catch (error: any) {
      message.error(error.message || '加载失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadTimers();
  }, [params]);

  // 删除定时器
  const handleDelete = async (id: number) => {
    try {
      await deleteTimer(id);
      message.success('删除成功');
      loadTimers();
    } catch (error: any) {
      message.error(error.message || '删除失败');
    }
  };

  // 激活定时器
  const handleActivate = async (id: number) => {
    try {
      await activateTimer(id);
      message.success('激活成功');
      loadTimers();
    } catch (error: any) {
      message.error(error.message || '激活失败');
    }
  };

  // 停用定时器
  const handleDeactivate = async (id: number) => {
    try {
      await deactivateTimer(id);
      message.success('停用成功');
      loadTimers();
    } catch (error: any) {
      message.error(error.message || '停用失败');
    }
  };

  // 表格列定义
  const columns: ColumnsType<TimerDefinition> = [
    {
      title: 'ID',
      dataIndex: 'id',
      width: 60,
    },
    {
      title: '应用',
      dataIndex: 'app',
      width: 120,
    },
    {
      title: '定时器名称',
      dataIndex: 'name',
      width: 150,
      ellipsis: true,
    },
    {
      title: 'Cron 表达式',
      dataIndex: 'cron_expr',
      width: 140,
      render: (text: string) => (
        <Tooltip title={text}>
          <code style={{ fontSize: 12 }}>{text}</code>
        </Tooltip>
      ),
    },
    {
      title: '回调地址',
      dataIndex: 'callback_url',
      width: 200,
      ellipsis: true,
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      render: (status: TimerStatus) => {
        const config = TIMER_STATUS_CONFIG[status];
        return <Tag color={config?.color}>{config?.label || status}</Tag>;
      },
    },
    {
      title: '超时(秒)',
      dataIndex: 'timeout',
      width: 80,
    },
    {
      title: '最大重试',
      dataIndex: 'max_retries',
      width: 80,
    },
    {
      title: '操作',
      key: 'action',
      width: 200,
      render: (_, record) => (
        <Space size="small">
          {record.status === 'ACTIVE' ? (
            <Tooltip title="停用">
              <Button
                type="link"
                size="small"
                icon={<PauseCircleOutlined />}
                onClick={() => handleDeactivate(record.id)}
              />
            </Tooltip>
          ) : record.status === 'INACTIVE' ? (
            <Tooltip title="激活">
              <Button
                type="link"
                size="small"
                icon={<PlayCircleOutlined />}
                onClick={() => handleActivate(record.id)}
              />
            </Tooltip>
          ) : null}
          <Tooltip title="编辑">
            <Button
              type="link"
              size="small"
              icon={<EditOutlined />}
              onClick={() => navigate(`/tasks/edit/${record.id}`)}
            />
          </Tooltip>
          <Popconfirm
            title="确定删除此定时器？"
            onConfirm={() => handleDelete(record.id)}
            okText="确定"
            cancelText="取消"
          >
            <Tooltip title="删除">
              <Button
                type="link"
                size="small"
                danger
                icon={<DeleteOutlined />}
              />
            </Tooltip>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div>
      {/* 统计卡片 */}
      <Row gutter={16} style={{ marginBottom: 16 }}>
        <Col span={8}>
          <Card size="small">
            <Statistic title="总定时器" value={stats.total} />
          </Card>
        </Col>
        <Col span={8}>
          <Card size="small">
            <Statistic title="已激活" value={stats.active} valueStyle={{ color: '#52c41a' }} />
          </Card>
        </Col>
        <Col span={8}>
          <Card size="small">
            <Statistic title="未激活" value={stats.inactive} valueStyle={{ color: '#faad14' }} />
          </Card>
        </Col>
      </Row>

      {/* 搜索和操作栏 */}
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
        <Space>
          <Input
            placeholder="搜索定时器名称"
            prefix={<SearchOutlined />}
            style={{ width: 200 }}
            onChange={(e) => setParams({ ...params, keyword: e.target.value, page: 1 })}
            allowClear
          />
          <Select
            placeholder="状态筛选"
            style={{ width: 120 }}
            allowClear
            onChange={(value) => setParams({ ...params, status: value, page: 1 })}
            options={Object.entries(TIMER_STATUS_CONFIG).map(([key, config]) => ({
              label: config.label,
              value: key,
            }))}
          />
        </Space>
        <Space>
          <Button
            icon={<ReloadOutlined />}
            onClick={loadTimers}
          >
            刷新
          </Button>
          <Button
            type="primary"
            icon={<PlusOutlined />}
            onClick={() => navigate('/tasks/create')}
          >
            创建定时器
          </Button>
        </Space>
      </div>

      {/* 定时器表格 */}
      <Table
        columns={columns}
        dataSource={timers}
        rowKey="id"
        loading={loading}
        pagination={{
          current: params.page,
          pageSize: params.page_size,
          total,
          showSizeChanger: true,
          showQuickJumper: true,
          showTotal: (total) => `共 ${total} 条`,
          onChange: (page, pageSize) => setParams({ ...params, page, page_size: pageSize }),
        }}
      />
    </div>
  );
};

export default TaskList;
