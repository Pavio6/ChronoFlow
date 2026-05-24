import React, { useState, useEffect, useCallback } from 'react';
import {
  Form,
  Input,
  Select,
  Button,
  Card,
  InputNumber,
  Space,
  message,
  Typography,
  Row,
  Col,
  Divider,
} from 'antd';
import { useNavigate, useParams } from 'react-router-dom';
import { ArrowLeftOutlined } from '@ant-design/icons';
import type { CreateTimerRequest, UpdateTimerRequest } from '../types';
import { createTimer, getTimer, updateTimer } from '../api/tasks';
import { HTTP_METHODS, CRON_PRESETS, APP_PRESETS } from '../utils/constants';

const { TextArea } = Input;
const { Text } = Typography;

interface FormValues {
  app: string;
  name: string;
  cron_expr: string;
  callback_url: string;
  callback_method: string;
  callback_body?: string;
  callback_headers?: string;
  timeout: number;
  max_retries: number;
}

const TaskForm: React.FC = () => {
  const navigate = useNavigate();
  const { id } = useParams<{ id: string }>();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const isEdit = !!id;

  const loadTimer = useCallback(async () => {
    try {
      const res = await getTimer(Number(id));
      if (res.data) {
        const t = res.data;
        let headers = {};
        try { headers = JSON.parse(t.callback_headers || '{}'); } catch { /* */ }
        form.setFieldsValue({
          app: t.app, name: t.name, cron_expr: t.cron_expr,
          callback_url: t.callback_url, callback_method: t.callback_method,
          callback_body: t.callback_body,
          callback_headers: JSON.stringify(headers, null, 2),
          timeout: t.timeout, max_retries: t.max_retries,
        });
      }
    } catch (error: unknown) {
      message.error((error as Error).message || '加载失败');
      navigate('/tasks');
    }
  }, [id, form, navigate]);

  useEffect(() => {
    if (isEdit) loadTimer();
  }, [isEdit, loadTimer]);

  const handleSubmit = async (values: FormValues) => {
    setLoading(true);
    try {
      let headers = {};
      if (values.callback_headers) {
        try {
          headers = JSON.parse(values.callback_headers);
        } catch {
          message.error('请求头 JSON 格式错误');
          setLoading(false);
          return;
        }
      }
      const data = { ...values, callback_headers: headers };
      if (isEdit) {
        await updateTimer(Number(id), data as UpdateTimerRequest);
        message.success('更新成功');
      } else {
        await createTimer(data as CreateTimerRequest);
        message.success('创建成功');
      }
      navigate('/tasks');
    } catch (error: unknown) {
      message.error((error as Error).message || '操作失败');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div>
      <Button
        type="link"
        icon={<ArrowLeftOutlined />}
        onClick={() => navigate('/tasks')}
        style={{ padding: 0, marginBottom: 16 }}
      >
        返回
      </Button>

      <Card>
        <Form
          form={form}
          layout="vertical"
          onFinish={handleSubmit}
          initialValues={{ app: 'default', callback_method: 'POST', timeout: 30, max_retries: 3 }}
          style={{ maxWidth: 680 }}
        >
          <Divider orientation="left" plain>基本信息</Divider>

          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="app" label="应用" rules={[{ required: true }]}>
                <Select
                  showSearch
                  placeholder="选择应用"
                  options={APP_PRESETS.map(a => ({ label: a, value: a }))}
                />
              </Form.Item>
            </Col>
            <Col span={12}>
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

          <Divider orientation="left" plain>回调配置</Divider>

          <Row gutter={16}>
            <Col span={16}>
              <Form.Item
                name="callback_url"
                label="URL"
                rules={[{ required: true }, { type: 'url', message: 'URL 格式错误' }]}
              >
                <Input placeholder="https://api.example.com/callback" />
              </Form.Item>
            </Col>
            <Col span={8}>
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

          <Divider orientation="left" plain>执行配置</Divider>

          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="timeout" label="超时 (秒)" rules={[{ required: true }]}>
                <InputNumber min={1} max={300} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="max_retries" label="最大重试" rules={[{ required: true }]}>
                <InputNumber min={0} max={10} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
          </Row>

          <Form.Item>
            <Space>
              <Button type="primary" htmlType="submit" loading={loading}>
                {isEdit ? '更新' : '创建'}
              </Button>
              <Button onClick={() => navigate('/tasks')}>取消</Button>
            </Space>
          </Form.Item>
        </Form>
      </Card>
    </div>
  );
};

export default TaskForm;
