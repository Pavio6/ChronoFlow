import React, { useState } from 'react';
import {
  Form,
  Input,
  InputNumber,
  Select,
  Button,
  Space,
  message,
  Typography,
  Row,
  Col,
} from 'antd';
import { useNavigate } from 'react-router-dom';
import { ArrowLeftOutlined } from '@ant-design/icons';
import type { CreateTimerRequest, MisfirePolicy } from '../types';
import { createTimer } from '../api/tasks';
import { HTTP_METHODS, CRON_PRESETS, APP_PRESETS } from '../utils/constants';

const { TextArea } = Input;
const { Text } = Typography;

interface FormValues {
  app: string;
  name: string;
  cron_expr: string;
  callback_url: string;
  callback_method: CreateTimerRequest['callback_method'];
  callback_body?: string;
  callback_headers?: string;
  misfire_policy: MisfirePolicy;
  max_catch_up: number;
}

const TaskForm: React.FC = () => {
  const navigate = useNavigate();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (values: FormValues) => {
    setLoading(true);
    try {
      let headers: Record<string, string> = {};
      if (values.callback_headers) {
        try {
          const parsed: unknown = JSON.parse(values.callback_headers);
          if (
            typeof parsed !== 'object' ||
            parsed === null ||
            Array.isArray(parsed) ||
            Object.values(parsed).some((value) => typeof value !== 'string')
          ) {
            throw new Error('headers must be a string map');
          }
          headers = parsed as Record<string, string>;
        } catch {
          message.error('请求头必须是字符串键值对 JSON 对象');
          setLoading(false);
          return;
        }
      }
      const data: CreateTimerRequest = { ...values, callback_headers: headers };
      await createTimer(data);
      message.success('创建成功');
      navigate('/tasks');
    } catch (error: unknown) {
      message.error((error as Error).message || '操作失败');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="page-stack form-page">
      <Button
        type="text"
        icon={<ArrowLeftOutlined />}
        onClick={() => navigate('/tasks')}
        className="back-button"
      >
        返回
      </Button>

      <div className="surface form-surface">
        <Form
          form={form}
          layout="vertical"
          onFinish={handleSubmit}
          initialValues={{
            app: 'default',
            callback_method: 'POST',
            misfire_policy: 'FIRE_ONCE',
            max_catch_up: 10,
          }}
          className="task-form"
        >
          <div className="form-section-heading">
            <h2>基本信息</h2>
            <p>定义任务所属应用、名称和触发节奏。</p>
          </div>

          <Row gutter={16}>
            <Col xs={24} md={12}>
              <Form.Item name="app" label="应用" rules={[{ required: true }]}>
                <Select
                  showSearch
                  placeholder="选择应用"
                  options={APP_PRESETS.map(a => ({ label: a, value: a }))}
                />
              </Form.Item>
            </Col>
            <Col xs={24} md={12}>
              <Form.Item name="name" label="名称" rules={[{ required: true }]}>
                <Input placeholder="定时器名称" maxLength={128} />
              </Form.Item>
            </Col>
          </Row>

          <Form.Item
            name="cron_expr"
            label="Cron 表达式"
            rules={[{ required: true }]}
            extra={<Text type="secondary" style={{ fontSize: 12 }}>格式: 秒 分 时 日 月 周</Text>}
          >
            <Space.Compact style={{ width: '100%' }}>
              <Input placeholder="0 0 2 * * *" style={{ flex: 1 }} />
              <Select
                placeholder="预设"
                style={{ width: 140 }}
                allowClear
                onChange={(v) => v && form.setFieldValue('cron_expr', v)}
                options={CRON_PRESETS.map(p => ({ label: p.label, value: p.value }))}
              />
            </Space.Compact>
          </Form.Item>

          <Row gutter={16}>
            <Col xs={24} md={12}>
              <Form.Item
                name="misfire_policy"
                label="错过触发策略"
                rules={[{ required: true }]}
              >
                <Select
                  options={[
                    { value: 'SKIP', label: '跳过错过触发' },
                    { value: 'FIRE_ONCE', label: '补执行一次' },
                    { value: 'CATCH_UP', label: '按上限追赶' },
                  ]}
                />
              </Form.Item>
            </Col>
            <Col xs={24} md={12}>
              <Form.Item
                name="max_catch_up"
                label="单轮追赶上限"
                rules={[{ required: true }]}
              >
                <InputNumber min={1} max={1000} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
          </Row>

          <div className="form-section-heading">
            <h2>回调配置</h2>
            <p>任务触发时会按这里的 HTTP 参数发起请求。</p>
          </div>

          <Row gutter={16}>
            <Col xs={24} md={16}>
              <Form.Item
                name="callback_url"
                label="URL"
                rules={[{ required: true }, { type: 'url', message: 'URL 格式错误' }]}
              >
                <Input placeholder="https://api.example.com/callback" />
              </Form.Item>
            </Col>
            <Col xs={24} md={8}>
              <Form.Item name="callback_method" label="方法" rules={[{ required: true }]}>
                <Select options={HTTP_METHODS.map(m => ({ label: m, value: m }))} />
              </Form.Item>
            </Col>
          </Row>

          <Form.Item name="callback_body" label="请求体">
            <TextArea placeholder='{"key": "value"}' rows={3} style={{ fontFamily: 'monospace' }} />
          </Form.Item>

          <Form.Item name="callback_headers" label="请求头 (JSON)">
            <TextArea placeholder='{"Authorization": "Bearer xxx"}' rows={2} style={{ fontFamily: 'monospace' }} />
          </Form.Item>

          <Form.Item className="form-actions">
            <Space>
              <Button type="primary" htmlType="submit" loading={loading}>
                创建
              </Button>
              <Button onClick={() => navigate('/tasks')}>取消</Button>
            </Space>
          </Form.Item>
        </Form>
      </div>
    </div>
  );
};

export default TaskForm;
