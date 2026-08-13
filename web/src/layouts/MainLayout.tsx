import { useState } from 'react';
import { Button, Drawer, Layout, Menu } from 'antd';
import {
  ClockCircleOutlined,
  HistoryOutlined,
  MenuOutlined,
  PlusOutlined,
  ScheduleOutlined,
} from '@ant-design/icons';
import { Outlet, useLocation, useNavigate } from 'react-router-dom';

const { Content, Header, Sider } = Layout;

const menuItems = [
  { key: '/tasks', icon: <ScheduleOutlined />, label: '定时任务' },
  { key: '/executions', icon: <HistoryOutlined />, label: '执行记录' },
];

const pageMeta = (pathname: string) => {
  if (pathname.startsWith('/executions')) {
    return { title: '执行记录', description: '查看每次计划触发的执行结果与失败原因' };
  }
  if (pathname.includes('/create')) {
    return { title: '创建定时任务', description: '配置触发计划、错过策略与 HTTP 回调' };
  }
  return { title: '定时任务', description: '管理任务定义、运行状态与下一次触发时间' };
};

const Brand = () => (
  <div className="brand">
    <span className="brand-mark"><ClockCircleOutlined /></span>
    <div>
      <div className="brand-title">ChronoFlow</div>
      <div className="brand-subtitle">任务调度控制台</div>
    </div>
  </div>
);

const MainLayout = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const meta = pageMeta(location.pathname);
  const selectedKey = location.pathname.startsWith('/executions') ? '/executions' : '/tasks';

  const navigateFromMenu = (path: string) => {
    navigate(path);
    setMobileMenuOpen(false);
  };

  const navigation = (
    <Menu
      mode="inline"
      selectedKeys={[selectedKey]}
      items={menuItems}
      onClick={({ key }) => navigateFromMenu(key)}
      className="side-menu"
    />
  );

  return (
    <Layout className="app-shell">
      <Sider width={232} className="app-sidebar" theme="light">
        <Brand />
        {navigation}
        <div className="sidebar-footer">
          <span>ChronoFlow</span>
          <span>API v1</span>
        </div>
      </Sider>

      <Drawer
        placement="left"
        size={272}
        open={mobileMenuOpen}
        onClose={() => setMobileMenuOpen(false)}
        className="mobile-navigation"
        title={<Brand />}
      >
        {navigation}
      </Drawer>

      <Layout className="main-pane">
        <Header className="app-header">
          <div className="header-title-group">
            <Button
              type="text"
              className="mobile-menu-button"
              icon={<MenuOutlined />}
              aria-label="打开导航"
              onClick={() => setMobileMenuOpen(true)}
            />
            <div>
              <h1>{meta.title}</h1>
              <p>{meta.description}</p>
            </div>
          </div>
          <div className="header-actions">
            {!location.pathname.includes('/create') && (
              <Button
                type="primary"
                icon={<PlusOutlined />}
                onClick={() => navigate('/tasks/create')}
              >
                新建任务
              </Button>
            )}
          </div>
        </Header>
        <Content className="app-content">
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
};

export default MainLayout;
