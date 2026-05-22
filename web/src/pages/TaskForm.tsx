import React, { useState, useEffect } from 'react';
import {
  Form,
  Input,
  Select,
  Button,
  Card,
  InputNumber,
  Space,
  message,
  Divider,
  Alert,
} from 'antd';
import { useNavigate, useParams } from 'react-router-dom';
import { ArrowLeftOutlined } from '@ant-design/icons';
import type { CreateTimerRequest, UpdateTimerRequest } from '../types';
import { createTimer, getTimer, updateTimer } from '../api/tasks';
import { HTTP_METHODS, CRON_PRESETS, APP_PRESETS } from '../utils/constants';

const { TextArea } = Input;

const TaskForm: React.FC = () => {
  const navigate = useNavigate();
  const { id } = useParams<{ id: string }>();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [fetchLoading, setFetchLoading] = useState(false);
  const isEdit = !!id;

  // 如果是编辑模式，加载定时器数据
  useEffect(() => {
    if (isEdit) {
      loadTimer();
    }
  }, [id]);

  const loadTimer = async () => {
    setFetchLoading(true);
    try {
      const res = await getTimer(Number(id));
      if (res.data) {
        const timer = res.data;
        // 解析 callback_headers
        let headers = {};
        try {
          headers = JSON.parse(timer.callback_headers || '{}');
        } catch {}

        form.setFieldsValue({
          app: timer.app,
          name: timer.name,
          cron_expr: timer.cron_expr,
          callback_url: timer.callback_url,
          callback_method: timer.callback_method,
          callback_body: timer.callback_body,
          callback_headers: JSON.stringify(headers, null, 2),
          timeout: timer.timeout,
          max_retries: timer.max_retries,
        });
      }
    } catch (error: any) {
      message.error(error.message || '加载定时器失败');
      navigate('/tasks');
    } finally {
      setFetchLoading(false);
    }
  };

  // 提交表单
  const handleSubmit = async (values: any) => {
    setLoading(true);
    try {
      // 解析 headers
      let headers = {};
      if (values.callback_headers) {
        try {
          headers = JSON.parse(values.callback_headers);
        } catch {
          message.error('回调头格式错误，请输入有效的 JSON');
          setLoading(false);
          return;
        }
      }

      const data = {
        ...values,
        callback_headers: headers,
      };

      if (isEdit) {
        await updateTimer(Number(id), data as UpdateTimerRequest);
        message.success('更新成功');
      } else {
        await createTimer(data as CreateTimerRequest);
        message.success('创建成功');
      }
      navigate('/tasks');
    } catch (error: any) {
      message.error(error.message || '操作失败');
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
        style={{ marginBottom: 16, padding: 0 }}
      >
        返回定时器列表
      </Button>

      <Card
        title={isEdit ? '编辑定时器' : '创建定时器'}
        loading={fetchLoading}
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={handleSubmit}
          initialValues={{
            app: 'default',
            callback_method: 'POST',
            timeout: 30,
            max_retries: 3,
          }}
        >
          <Form.Item
            name="app"
            label="应用名"
            rules={[{ required: true, message: '请输入应用名' }]}
          >
            <Select
              placeholder="请选择或输入应用名"
              showSearch
              options={APP_PRESETS.map(app => ({ label: app, value: app }))}
            />
          </Form.Item>

          <Form.Item
            name="name"
            label="定时器名称"
            rules={[{ required: true, message: '请输入定时器名称' }]}
          >
            <Input placeholder="请输入定时器名称" maxLength={128} />
          </Form.Item>

          <Form.Item
            name="cron_expr"
            label="Cron 表达式"
            rules={[{ required: true, message: '请输入 Cron 表达式' }]}
            extra="格式: 秒 分 时 日 月 周，例如: 0 0 2 * * * 表示每天凌晨2点"
          >
            <Space.Compact style={{ width: '100%' }}>
              <Input
                placeholder="0 0 2 * * *"
                style={{ flex: 1 }}
              />
              <Select
                placeholder="预设"
                style={{ width: 150 }}
                allowClear
                onChange={(value) => {
                  if (value) {
                    form.setFieldValue('cron_expr', value);
                  }
                }}
                options={CRON_PRESETS.map(p => ({ label: p.label, value: p.value }))}
              />
            </Space.Compact>
          </Form.Item>

          <Divider>回调配置</Divider>

          <Form.Item
            name="callback_url"
            label="回调 URL"
            rules={[
              { required: true, message: '请输入回调 URL' },
              { type: 'url', message: '请输入有效的 URL' },
            ]}
          >
            <Input placeholder="https://api.example.com/callback" />
          </Form.Item>

          <Form.Item
            name="callback_method"
            label="请求方法"
            rules={[{ required: true }]}
          >
            <Select options={HTTP_METHODS.map(m => ({ label: m, value: m }))} />
          </Form.Item>

          <Form.Item
            name="callback_body"
            label="请求体"
          >
            <TextArea
              placeholder='{"key": "value"}'
              rows={4}
              style={{ fontFamily: 'monospace' }}
            />
          </Form.Item>

          <Form.Item
            name="callback_headers"
            label="请求头 (JSON 格式)"
          >
            <TextArea
              placeholder='{"Authorization": "Bearer token123"}'
              rows={3}
              style={{ fontFamily: 'monospace' }}
            />
          </Form.Item>

          <Divider>执行配置</Divider>

          <Space size="large">
            <Form.Item
              name="timeout"
              label="超时时间(秒)"
              rules={[{ required: true }]}
            >
              <InputNumber min={1} max={300} />
            </Form.Item>

            <Form.Item
              name="max_retries"
              label="最大重试次数"
              rules={[{ required: true }]}
            >
              <InputNumber min={0} max={10} />
            </Form.Item>
          </Space>

          <Alert
            message="重试策略"
            description="失败后采用指数退避重试：第1次10秒后重试，第2次30秒后重试，第3次60秒后重试。"
            type="info"
            showIcon
            style={{ marginBottom: 24 }}
          />

          <Form.Item>
            <Space>
              <Button type="primary" htmlType="submit" loading={loading}>
                {isEdit ? '更新' : '创建'}
              </Button>
              <Button onClick={() => navigate('/tasks')}>
                取消
              </Button>
            </Space>
          </Form.Item>
        </Form>
      </Card>
    </div>
  );
};

export default TaskForm;
