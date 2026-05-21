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
import type { CreateTaskRequest, UpdateTaskRequest } from '../types';
import { createTask, getTask, updateTask } from '../api/tasks';
import { HTTP_METHODS, CRON_PRESETS } from '../utils/constants';

const { TextArea } = Input;

const TaskForm: React.FC = () => {
  const navigate = useNavigate();
  const { id } = useParams<{ id: string }>();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [fetchLoading, setFetchLoading] = useState(false);
  const isEdit = !!id;

  // 如果是编辑模式，加载任务数据
  useEffect(() => {
    if (isEdit) {
      loadTask();
    }
  }, [id]);

  const loadTask = async () => {
    setFetchLoading(true);
    try {
      const res = await getTask(Number(id));
      if (res.data) {
        const task = res.data;
        // 解析 callback_headers
        let headers = {};
        try {
          headers = JSON.parse(task.callback_headers || '{}');
        } catch {}

        form.setFieldsValue({
          name: task.name,
          description: task.description,
          cron_expr: task.cron_expr,
          callback_url: task.callback_url,
          callback_method: task.callback_method,
          callback_body: task.callback_body,
          callback_headers: JSON.stringify(headers, null, 2),
          timeout: task.timeout,
          max_retries: task.max_retries,
        });
      }
    } catch (error: any) {
      message.error(error.message || '加载任务失败');
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
        await updateTask(Number(id), data as UpdateTaskRequest);
        message.success('更新成功');
      } else {
        await createTask(data as CreateTaskRequest);
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
        返回任务列表
      </Button>

      <Card
        title={isEdit ? '编辑任务' : '创建任务'}
        loading={fetchLoading}
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={handleSubmit}
          initialValues={{
            callback_method: 'POST',
            timeout: 30,
            max_retries: 3,
          }}
        >
          <Form.Item
            name="name"
            label="任务名称"
            rules={[{ required: true, message: '请输入任务名称' }]}
          >
            <Input placeholder="请输入任务名称" maxLength={128} />
          </Form.Item>

          <Form.Item
            name="description"
            label="任务描述"
          >
            <TextArea placeholder="请输入任务描述" rows={2} maxLength={512} />
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
