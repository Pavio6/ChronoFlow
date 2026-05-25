import React, { useState, useEffect, useCallback } from 'react';
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
  Empty,
} from 'antd';
import {
  ReloadOutlined,
  EyeOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  SyncOutlined,
  ClockCircleOutlined,
} from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import type { TimerRecord, RecordStatus, RecordListParams } from '../types';
import { getRecords } from '../api/executions';
import { RECORD_STATUS_CONFIG } from '../utils/constants';

const statusIcon: Record<string, React.ReactNode> = {
  SUCCESS: <CheckCircleOutlined className="success-text" />,
  FAILED: <CloseCircleOutlined className="danger-text" />,
  RUNNING: <SyncOutlined spin className="info-text" />,
  PENDING: <ClockCircleOutlined className="muted" />,
};

const ExecutionList: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const [records, setRecords] = useState<TimerRecord[]>([]);
  const [total, setTotal] = useState(0);
  const [params, setParams] = useState<RecordListParams>({ page: 1, page_size: 10 });
  const [detailVisible, setDetailVisible] = useState(false);
  const [selected, setSelected] = useState<TimerRecord | null>(null);
  const [stats, setStats] = useState({ total: 0, success: 0, failed: 0, running: 0 });

  const loadRecords = useCallback(async () => {
    setLoading(true);
    try {
      const res = await getRecords(params);
      const items = res.items || [];
      setRecords(items);
      setTotal(res.total);
      setStats({
        total: res.total,
        success: items.filter(r => r.status === 'SUCCESS').length,
        failed: items.filter(r => r.status === 'FAILED').length,
        running: items.filter(r => r.status === 'RUNNING').length,
      });
    } catch (error: unknown) {
      message.error((error as Error).message || '加载失败');
    } finally {
      setLoading(false);
    }
  }, [params]);

  useEffect(() => {
    const run = async () => { await loadRecords(); };
    run();
  }, [loadRecords]);

  const columns: ColumnsType<TimerRecord> = [
    { title: 'ID', dataIndex: 'id', width: 72, render: (id: number) => <span className="muted">#{id}</span> },
    {
      title: '定时器',
      dataIndex: 'timer_id',
      width: 80,
      render: (id: number) => <span className="mono-chip">#{id}</span>,
    },
    {
      title: '触发时间',
      dataIndex: 'trigger_time',
      width: 160,
      render: (t: string) => new Date(t).toLocaleString('zh-CN'),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 80,
      render: (status: RecordStatus) => {
        const config = RECORD_STATUS_CONFIG[status];
        return (
          <Space size={4}>
            {statusIcon[status]}
            <Tag color={config?.color} className="status-tag">{config?.label || status}</Tag>
          </Space>
        );
      },
    },
    {
      title: '请求',
      width: 200,
      ellipsis: true,
      render: (_, r) => (
        <Tooltip title={`${r.request_method} ${r.request_url}`}>
          <span>{r.request_method} {r.request_url}</span>
        </Tooltip>
      ),
    },
    {
      title: '响应码',
      dataIndex: 'response_code',
      width: 70,
      render: (code: number) => code || '-',
    },
    {
      title: '耗时',
      dataIndex: 'duration',
      width: 80,
      render: (ms: number) => ms ? `${ms}ms` : '-',
    },
    {
      title: '操作',
      key: 'action',
      width: 50,
      render: (_, record) => (
        <Tooltip title="详情">
          <Button className="icon-button" type="text" size="small" icon={<EyeOutlined />}
            onClick={() => { setSelected(record); setDetailVisible(true); }} />
        </Tooltip>
      ),
    },
  ];

  return (
    <div className="page-stack">
      <div className="metric-grid four">
        <div className="metric-tile">
          <span>总计</span>
          <strong>{stats.total}</strong>
        </div>
        <div className="metric-tile">
          <span>成功</span>
          <strong className="success-text">{stats.success}</strong>
        </div>
        <div className="metric-tile">
          <span>失败</span>
          <strong className="danger-text">{stats.failed}</strong>
        </div>
        <div className="metric-tile">
          <span>运行中</span>
          <strong>{stats.running}</strong>
        </div>
      </div>

      <div className="surface">
        <div className="toolbar">
          <Space wrap>
          <Select
            placeholder="定时器ID"
            className="toolbar-select"
            allowClear
            showSearch
            onChange={(v) => setParams({ ...params, timer_id: v, page: 1 })}
          />
          <Select
            placeholder="状态"
            className="toolbar-select"
            allowClear
            onChange={(v) => setParams({ ...params, status: v, page: 1 })}
            options={Object.entries(RECORD_STATUS_CONFIG).map(([k, v]) => ({ label: v.label, value: k }))}
          />
          </Space>
          <Button icon={<ReloadOutlined />} onClick={loadRecords}>刷新</Button>
        </div>

        <Table
          columns={columns}
          dataSource={records}
          rowKey="id"
          loading={loading}
          size="middle"
          scroll={{ x: 1000 }}
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

      <Modal
        title="执行详情"
        open={detailVisible}
        onCancel={() => setDetailVisible(false)}
        footer={null}
        width={700}
        className="detail-modal"
      >
        {selected && (
          <Descriptions bordered column={2} size="small">
            <Descriptions.Item label="ID">{selected.id}</Descriptions.Item>
            <Descriptions.Item label="定时器ID">{selected.timer_id}</Descriptions.Item>
            <Descriptions.Item label="触发时间">
              {new Date(selected.trigger_time).toLocaleString('zh-CN')}
            </Descriptions.Item>
            <Descriptions.Item label="状态">
              <Tag color={RECORD_STATUS_CONFIG[selected.status]?.color} className="status-tag">
                {RECORD_STATUS_CONFIG[selected.status]?.label}
              </Tag>
            </Descriptions.Item>
            <Descriptions.Item label="响应码">{selected.response_code || '-'}</Descriptions.Item>
            <Descriptions.Item label="耗时">{selected.duration ? `${selected.duration}ms` : '-'}</Descriptions.Item>
            <Descriptions.Item label="方法">{selected.request_method}</Descriptions.Item>
            <Descriptions.Item label="URL" span={2}>{selected.request_url}</Descriptions.Item>
            <Descriptions.Item label="请求体" span={2}>
              <pre className="code-block">
                {selected.request_body || '-'}
              </pre>
            </Descriptions.Item>
            <Descriptions.Item label="响应体" span={2}>
              <pre className="code-block">
                {selected.response_body || '-'}
              </pre>
            </Descriptions.Item>
            {selected.error_message && (
              <Descriptions.Item label="错误" span={2}>
                <span className="danger-text">{selected.error_message}</span>
              </Descriptions.Item>
            )}
            <Descriptions.Item label="开始">
              {selected.started_at ? new Date(selected.started_at).toLocaleString('zh-CN') : '-'}
            </Descriptions.Item>
            <Descriptions.Item label="完成">
              {selected.finished_at ? new Date(selected.finished_at).toLocaleString('zh-CN') : '-'}
            </Descriptions.Item>
          </Descriptions>
        )}
      </Modal>
    </div>
  );
};

export default ExecutionList;
