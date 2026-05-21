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
  ThunderboltOutlined,
  EditOutlined,
  DeleteOutlined,
  ReloadOutlined,
} from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import type { ColumnsType } from 'antd/es/table';
import type { Task, TaskStatus, TaskListParams } from '../types';
import { getTasks, deleteTask, enableTask, disableTask, triggerTask } from '../api/tasks';
import { TASK_STATUS_CONFIG } from '../utils/constants';

const TaskList: React.FC = () => {
  const navigate = useNavigate();
  const [loading, setLoading] = useState(false);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [total, setTotal] = useState(0);
  const [params, setParams] = useState<TaskListParams>({
    page: 1,
    page_size: 10,
  });

  // 统计数据
  const [stats, setStats] = useState({
    total: 0,
    enabled: 0,
    running: 0,
    failed: 0,
  });

  // 加载任务列表
  const loadTasks = async () => {
    setLoading(true);
    try {
      const res = await getTasks(params);
      setTasks(res.tasks || []);
      setTotal(res.total);
      
      // 计算统计数据
      const allTasks = res.tasks || [];
      setStats({
        total: res.total,
        enabled: allTasks.filter(t => t.status === 'ENABLED').length,
        running: allTasks.filter(t => t.status === 'RUNNING').length,
        failed: allTasks.filter(t => t.status === 'FAILED').length,
      });
    } catch (error: any) {
      message.error(error.message || '加载失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadTasks();
  }, [params]);

  // 删除任务
  const handleDelete = async (id: number) => {
    try {
      await deleteTask(id);
      message.success('删除成功');
      loadTasks();
    } catch (error: any) {
      message.error(error.message || '删除失败');
    }
  };

  // 启用任务
  const handleEnable = async (id: number) => {
    try {
      await enableTask(id);
      message.success('启用成功');
      loadTasks();
    } catch (error: any) {
      message.error(error.message || '启用失败');
    }
  };

  // 禁用任务
  const handleDisable = async (id: number) => {
    try {
      await disableTask(id);
      message.success('禁用成功');
      loadTasks();
    } catch (error: any) {
      message.error(error.message || '禁用失败');
    }
  };

  // 手动触发
  const handleTrigger = async (id: number) => {
    try {
      await triggerTask(id);
      message.success('触发成功');
      loadTasks();
    } catch (error: any) {
      message.error(error.message || '触发失败');
    }
  };

  // 表格列定义
  const columns: ColumnsType<Task> = [
    {
      title: 'ID',
      dataIndex: 'id',
      width: 60,
    },
    {
      title: '任务名称',
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
      render: (status: TaskStatus) => {
        const config = TASK_STATUS_CONFIG[status];
        return <Tag color={config?.color}>{config?.label || status}</Tag>;
      },
    },
    {
      title: '下次触发',
      dataIndex: 'next_trigger_time',
      width: 180,
      render: (time: string | null) => {
        if (!time) return '-';
        return new Date(time).toLocaleString('zh-CN');
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
          {record.status === 'ENABLED' ? (
            <Tooltip title="禁用">
              <Button
                type="link"
                size="small"
                icon={<PauseCircleOutlined />}
                onClick={() => handleDisable(record.id)}
              />
            </Tooltip>
          ) : record.status === 'DISABLED' || record.status === 'INIT' ? (
            <Tooltip title="启用">
              <Button
                type="link"
                size="small"
                icon={<PlayCircleOutlined />}
                onClick={() => handleEnable(record.id)}
              />
            </Tooltip>
          ) : null}
          <Tooltip title="手动触发">
            <Button
              type="link"
              size="small"
              icon={<ThunderboltOutlined />}
              onClick={() => handleTrigger(record.id)}
              disabled={record.status !== 'ENABLED'}
            />
          </Tooltip>
          <Tooltip title="编辑">
            <Button
              type="link"
              size="small"
              icon={<EditOutlined />}
              onClick={() => navigate(`/tasks/edit/${record.id}`)}
            />
          </Tooltip>
          <Popconfirm
            title="确定删除此任务？"
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
        <Col span={6}>
          <Card size="small">
            <Statistic title="总任务数" value={stats.total} />
          </Card>
        </Col>
        <Col span={6}>
          <Card size="small">
            <Statistic title="已启用" value={stats.enabled} valueStyle={{ color: '#52c41a' }} />
          </Card>
        </Col>
        <Col span={6}>
          <Card size="small">
            <Statistic title="运行中" value={stats.running} valueStyle={{ color: '#1890ff' }} />
          </Card>
        </Col>
        <Col span={6}>
          <Card size="small">
            <Statistic title="失败" value={stats.failed} valueStyle={{ color: '#ff4d4f' }} />
          </Card>
        </Col>
      </Row>

      {/* 搜索和操作栏 */}
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
        <Space>
          <Input
            placeholder="搜索任务名称"
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
            options={Object.entries(TASK_STATUS_CONFIG).map(([key, config]) => ({
              label: config.label,
              value: key,
            }))}
          />
        </Space>
        <Space>
          <Button
            icon={<ReloadOutlined />}
            onClick={loadTasks}
          >
            刷新
          </Button>
          <Button
            type="primary"
            icon={<PlusOutlined />}
            onClick={() => navigate('/tasks/create')}
          >
            创建任务
          </Button>
        </Space>
      </div>

      {/* 任务表格 */}
      <Table
        columns={columns}
        dataSource={tasks}
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
