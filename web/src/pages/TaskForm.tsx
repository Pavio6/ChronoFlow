import { useState } from 'react';
import {
  Button,
  Col,
  Form,
  Input,
  InputNumber,
  message,
  Row,
  Select,
  Space,
} from 'antd';
import { ArrowLeftOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import { createTimer } from '../api/tasks';
import type { CreateTimerRequest, MisfirePolicy } from '../types';
import { CRON_PRESETS, HTTP_METHODS } from '../utils/constants';

const { TextArea } = Input;

interface FormValues {
  app: string;
  name: string;
  cron_expr: string;
  callback_url: string;
  callback_method: CreateTimerRequest['callback_method'];
  callback_body?: string;
  callback_headers?: string;
  misfire_policy: MisfirePolicy;
  max_catch_up?: number;
}

const policyHelp: Record<MisfirePolicy, string> = {
  SKIP: '跳过已经错过的触发点，直接推进到未来的下一次触发',
  FIRE_ONCE: '将多个错过的触发点合并为一次补发，然后恢复正常调度',
  CATCH_UP: '按历史顺序补发错过的触发点，并受单轮补偿上限限制',
};

const TaskForm = () => {
  const navigate = useNavigate();
  const [form] = Form.useForm<FormValues>();
  const [submitting, setSubmitting] = useState(false);
  const misfirePolicy = Form.useWatch('misfire_policy', form) ?? 'FIRE_ONCE';

  const submit = async (values: FormValues) => {
    setSubmitting(true);
    try {
      let headers: Record<string, string> | undefined;
      if (values.callback_headers?.trim()) {
        const parsed: unknown = JSON.parse(values.callback_headers);
        if (
          typeof parsed !== 'object' ||
          parsed === null ||
          Array.isArray(parsed) ||
          Object.values(parsed).some((value) => typeof value !== 'string')
        ) {
          throw new Error('请求头必须是仅包含字符串键值的 JSON 对象');
        }
        headers = parsed as Record<string, string>;
      }

      await createTimer({
        app: values.app.trim(),
        name: values.name.trim(),
        cron_expr: values.cron_expr.trim(),
        callback_url: values.callback_url.trim(),
        callback_method: values.callback_method,
        callback_body: values.callback_body,
        callback_headers: headers,
        misfire_policy: values.misfire_policy,
        max_catch_up: values.misfire_policy === 'CATCH_UP' ? values.max_catch_up : undefined,
      });
      message.success('定时任务已创建');
      navigate('/tasks');
    } catch (requestError) {
      message.error((requestError as Error).message || '创建任务失败');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="form-page">
      <Button
        type="text"
        icon={<ArrowLeftOutlined />}
        className="back-button"
        onClick={() => navigate('/tasks')}
      >
        返回任务列表
      </Button>

      <section className="surface form-surface">
        <Form<FormValues>
          form={form}
          layout="vertical"
          requiredMark="optional"
          onFinish={submit}
          initialValues={{
            callback_method: 'POST',
            misfire_policy: 'FIRE_ONCE',
            max_catch_up: 10,
          }}
          disabled={submitting}
        >
          <div className="form-section-heading first">
            <h2>基本信息</h2>
            <p>任务创建后定义不可修改，可以通过启用和停用控制调度状态</p>
          </div>

          <Row gutter={16}>
            <Col xs={24} md={12}>
              <Form.Item
                name="app"
                label="所属应用"
                rules={[
                  { required: true, message: '请输入所属应用' },
                  { max: 128, message: '最多输入 128 个字符' },
                  { whitespace: true, message: '所属应用不能为空' },
                ]}
              >
                <Input placeholder="例如 order-service" autoComplete="off" />
              </Form.Item>
            </Col>
            <Col xs={24} md={12}>
              <Form.Item
                name="name"
                label="任务名称"
                rules={[
                  { required: true, message: '请输入任务名称' },
                  { max: 128, message: '最多输入 128 个字符' },
                  { whitespace: true, message: '任务名称不能为空' },
                ]}
              >
                <Input placeholder="例如同步订单状态" autoComplete="off" />
              </Form.Item>
            </Col>
          </Row>

          <Form.Item
            name="cron_expr"
            label="Cron 表达式"
            extra="格式为：秒 分 时 日 月 周"
            rules={[{ required: true, message: '请输入 Cron 表达式' }]}
          >
            <Space.Compact block>
              <Input placeholder="0 */5 * * * *" className="mono-input" />
              <Select
                placeholder="常用预设"
                className="cron-preset"
                options={CRON_PRESETS.map((preset) => ({ label: preset.label, value: preset.value }))}
                onChange={(value) => form.setFieldValue('cron_expr', value)}
              />
            </Space.Compact>
          </Form.Item>

          <Row gutter={16}>
            <Col xs={24} md={misfirePolicy === 'CATCH_UP' ? 14 : 24}>
              <Form.Item
                name="misfire_policy"
                label="错过触发策略"
                extra={policyHelp[misfirePolicy]}
                rules={[{ required: true }]}
              >
                <Select
                  options={[
                    { value: 'SKIP', label: '跳过遗漏触发' },
                    { value: 'FIRE_ONCE', label: '合并补发一次' },
                    { value: 'CATCH_UP', label: '按上限依次补发' },
                  ]}
                />
              </Form.Item>
            </Col>
            {misfirePolicy === 'CATCH_UP' && (
              <Col xs={24} md={10}>
                <Form.Item
                  name="max_catch_up"
                  label="单轮补偿上限"
                  rules={[{ required: true, message: '请输入补偿上限' }]}
                >
                  <InputNumber min={1} max={1000} precision={0} className="full-width" />
                </Form.Item>
              </Col>
            )}
          </Row>

          <div className="form-section-heading">
            <h2>HTTP 回调</h2>
            <p>任务到期后，Worker 将按照以下配置发起请求</p>
          </div>

          <Row gutter={16}>
            <Col xs={24} md={16}>
              <Form.Item
                name="callback_url"
                label="回调地址"
                rules={[
                  { required: true, message: '请输入回调地址' },
                  { type: 'url', message: '请输入有效的 HTTP 或 HTTPS 地址' },
                ]}
              >
                <Input placeholder="https://api.example.com/jobs/run" />
              </Form.Item>
            </Col>
            <Col xs={24} md={8}>
              <Form.Item name="callback_method" label="请求方法" rules={[{ required: true }]}>
                <Select options={HTTP_METHODS.map((method) => ({ label: method, value: method }))} />
              </Form.Item>
            </Col>
          </Row>

          <Form.Item name="callback_headers" label="请求头" extra="可选，必须是字符串键值的 JSON 对象">
            <TextArea
              rows={3}
              className="mono-input"
              placeholder={'{\n  "Authorization": "Bearer token"\n}'}
            />
          </Form.Item>

          <Form.Item name="callback_body" label="请求体" extra="可选，将按原始文本发送">
            <TextArea rows={5} className="mono-input" placeholder={'{\n  "source": "chronoflow"\n}'} />
          </Form.Item>

          <div className="form-actions">
            <Space>
              <Button type="primary" htmlType="submit" loading={submitting}>
                创建任务
              </Button>
              <Button onClick={() => navigate('/tasks')}>取消</Button>
            </Space>
          </div>
        </Form>
      </section>
    </div>
  );
};

export default TaskForm;
