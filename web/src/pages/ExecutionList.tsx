import React, { useState, useEffect } from 'react';
import {
  Table,
  Tag,
  Space,
  Select,
  Button,
  message,
  Tooltip,
  Modal,
  Descriptions,
} from 'antd';
import { ReloadOutlined, EyeOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import type { TaskExecution, ExecutionStatus, ExecutionListParams } from '../types';
import { getExecutions } from '../api/executions';
import { EXECUTION_STATUS_CONFIG } from '../utils/constants';

const ExecutionList: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const [executions, setExecutions] = useState<TaskExecution[]>([]);
  const [total, setTotal] = useState(0);
  const [params, setParams] = useState<ExecutionListParams>({
    page: 1,
    page_size: 10,
  });
  const [detailVisible, setDetailVisible] = useState(false);
  const [selectedExecution, setSelectedExecution] = useState<TaskExecution | null>(null);

  // 加载执行记录列表
  const loadExecutions = async () => {
    setLoading(true);
    try {
      const res = await getExecutions(params);
      setExecutions(res.executions || []);
      setTotal(res.total);
    } catch (error: any) {
      message.error(error.message || '加载失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadExecutions();
  }, [params]);

  // 查看详情
  const handleViewDetail = (record: TaskExecution) => {
    setSelectedExecution(record);
    setDetailVisible(true);
  };

  // 表格列定义
  const columns: ColumnsType<TaskExecution> = [
    {
      title: 'ID',
      dataIndex: 'id',
      width: 60,
    },
    {
      title: '任务ID',
      dataIndex: 'task_id',
      width: 80,
    },
    {
      title: '触发时间',
      dataIndex: 'trigger_time',
      width: 180,
      render: (time: string) => new Date(time).toLocaleString('zh-CN'),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      render: (status: ExecutionStatus) => {
        const config = EXECUTION_STATUS_CONFIG[status];
        return <Tag color={config?.color}>{config?.label || status}</Tag>;
      },
    },
    {
      title: '重试次数',
      dataIndex: 'retry_count',
      width: 80,
    },
    {
      title: '请求方法',
      dataIndex: 'request_method',
      width: 80,
    },
    {
      title: '请求地址',
      dataIndex: 'request_url',
      width: 200,
      ellipsis: true,
    },
    {
      title: '响应码',
      dataIndex: 'response_code',
      width: 80,
      render: (code: number) => code || '-',
    },
    {
      title: '耗时(ms)',
      dataIndex: 'duration',
      width: 100,
      render: (duration: number) => duration ? `${duration}ms` : '-',
    },
    {
      title: '开始时间',
      dataIndex: 'started_at',
      width: 180,
      render: (time: string | null) => time ? new Date(time).toLocaleString('zh-CN') : '-',
    },
    {
      title: '完成时间',
      dataIndex: 'finished_at',
      width: 180,
      render: (time: string | null) => time ? new Date(time).toLocaleString('zh-CN') : '-',
    },
    {
      title: '操作',
      key: 'action',
      width: 60,
      render: (_, record) => (
        <Tooltip title="查看详情">
          <Button
            type="link"
            size="small"
            icon={<EyeOutlined />}
            onClick={() => handleViewDetail(record)}
          />
        </Tooltip>
      ),
    },
  ];

  return (
    <div>
      {/* 筛选栏 */}
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
        <Space>
          <Select
            placeholder="任务ID筛选"
            style={{ width: 120 }}
            allowClear
            onChange={(value) => setParams({ ...params, task_id: value, page: 1 })}
            showSearch
          />
          <Select
            placeholder="状态筛选"
            style={{ width: 120 }}
            allowClear
            onChange={(value) => setParams({ ...params, status: value, page: 1 })}
            options={Object.entries(EXECUTION_STATUS_CONFIG).map(([key, config]) => ({
              label: config.label,
              value: key,
            }))}
          />
        </Space>
        <Button
          icon={<ReloadOutlined />}
          onClick={loadExecutions}
        >
          刷新
        </Button>
      </div>

      {/* 执行记录表格 */}
      <Table
        columns={columns}
        dataSource={executions}
        rowKey="id"
        loading={loading}
        scroll={{ x: 1500 }}
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

      {/* 详情弹窗 */}
      <Modal
        title="执行详情"
        open={detailVisible}
        onCancel={() => setDetailVisible(false)}
        footer={null}
        width={800}
      >
        {selectedExecution && (
          <Descriptions bordered column={2} size="small">
            <Descriptions.Item label="执行ID">{selectedExecution.id}</Descriptions.Item>
            <Descriptions.Item label="任务ID">{selectedExecution.task_id}</Descriptions.Item>
            <Descriptions.Item label="触发时间">
              {new Date(selectedExecution.trigger_time).toLocaleString('zh-CN')}
            </Descriptions.Item>
            <Descriptions.Item label="状态">
              <Tag color={EXECUTION_STATUS_CONFIG[selectedExecution.status]?.color}>
                {EXECUTION_STATUS_CONFIG[selectedExecution.status]?.label}
              </Tag>
            </Descriptions.Item>
            <Descriptions.Item label="重试次数">{selectedExecution.retry_count}</Descriptions.Item>
            <Descriptions.Item label="响应码">{selectedExecution.response_code || '-'}</Descriptions.Item>
            <Descriptions.Item label="耗时">
              {selectedExecution.duration ? `${selectedExecution.duration}ms` : '-'}
            </Descriptions.Item>
            <Descriptions.Item label="请求方法">{selectedExecution.request_method}</Descriptions.Item>
            <Descriptions.Item label="请求地址" span={2}>
              {selectedExecution.request_url}
            </Descriptions.Item>
            <Descriptions.Item label="请求体" span={2}>
              <pre style={{ maxHeight: 100, overflow: 'auto', margin: 0 }}>
                {selectedExecution.request_body || '-'}
              </pre>
            </Descriptions.Item>
            <Descriptions.Item label="响应体" span={2}>
              <pre style={{ maxHeight: 100, overflow: 'auto', margin: 0 }}>
                {selectedExecution.response_body || '-'}
              </pre>
            </Descriptions.Item>
            {selectedExecution.error_message && (
              <Descriptions.Item label="错误信息" span={2}>
                <span style={{ color: '#ff4d4f' }}>{selectedExecution.error_message}</span>
              </Descriptions.Item>
            )}
            <Descriptions.Item label="开始时间">
              {selectedExecution.started_at
                ? new Date(selectedExecution.started_at).toLocaleString('zh-CN')
                : '-'}
            </Descriptions.Item>
            <Descriptions.Item label="完成时间">
              {selectedExecution.finished_at
                ? new Date(selectedExecution.finished_at).toLocaleString('zh-CN')
                : '-'}
            </Descriptions.Item>
          </Descriptions>
        )}
      </Modal>
    </div>
  );
};

export default ExecutionList;
