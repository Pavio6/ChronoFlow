import React from 'react';
import { Layout, Menu, Button } from 'antd';
import {
  ScheduleOutlined,
  HistoryOutlined,
  ClockCircleOutlined,
  PlusOutlined,
  DashboardOutlined,
} from '@ant-design/icons';
import { useNavigate, useLocation, Outlet } from 'react-router-dom';

const { Header, Sider, Content } = Layout;

const menuItems = [
  { key: '/tasks', icon: <ScheduleOutlined />, label: '任务管理' },
  { key: '/executions', icon: <HistoryOutlined />, label: '执行记录' },
  { key: '/monitoring', icon: <DashboardOutlined />, label: '监控面板' },
];

const MainLayout: React.FC = () => {
  const navigate = useNavigate();
  const location = useLocation();

  const pageMeta = location.pathname.startsWith('/monitoring')
    ? { title: '监控面板', desc: '观察执行质量、队列状态和运行时指标' }
    : location.pathname.startsWith('/executions')
    ? { title: '执行记录', desc: '查看回调状态、耗时和失败原因' }
    : location.pathname.includes('/create')
      ? { title: '创建任务', desc: '配置 Cron、回调和执行超时' }
      : location.pathname.includes('/edit')
        ? { title: '编辑任务', desc: '调整任务定义和执行参数' }
        : { title: '任务管理', desc: '管理定时任务定义、状态和回调目标' };

  return (
    <Layout className="app-shell">
      <Sider width={248} className="app-sidebar" breakpoint="lg" collapsedWidth={0}>
        <div className="brand">
          <span className="brand-mark"><ClockCircleOutlined /></span>
          <div>
            <div className="brand-title">ChronoFlow</div>
            <div className="brand-subtitle">Timer orchestration</div>
          </div>
        </div>
        <Menu
          mode="inline"
          selectedKeys={[
            location.pathname.startsWith('/monitoring')
              ? '/monitoring'
              : location.pathname.startsWith('/executions')
                ? '/executions'
                : '/tasks',
          ]}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
          className="side-menu"
        />
        <div className="sidebar-footer">
          <div className="sidebar-label">Runtime</div>
          <div className="runtime-pill">API /api/v1</div>
        </div>
      </Sider>
      <Layout className="main-pane">
        <Header className="app-header">
          <div>
            <h1>{pageMeta.title}</h1>
            <p>{pageMeta.desc}</p>
          </div>
          {!location.pathname.includes('/create') && !location.pathname.includes('/edit') && (
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => navigate('/tasks/create')}
            >
              新建任务
            </Button>
          )}
        </Header>
        <Content className="app-content">
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
};

export default MainLayout;
