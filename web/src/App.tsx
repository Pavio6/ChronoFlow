import { lazy, Suspense, useEffect } from 'react';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { ConfigProvider, Skeleton } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import MainLayout from './layouts/MainLayout';
import './App.css';

const loadTaskList = () => import('./pages/TaskList');
const loadTaskForm = () => import('./pages/TaskForm');
const loadExecutionList = () => import('./pages/ExecutionList');

const TaskList = lazy(loadTaskList);
const TaskForm = lazy(loadTaskForm);
const ExecutionList = lazy(loadExecutionList);

const RouteFallback = () => (
  <div className="route-fallback" aria-label="页面加载中">
    <Skeleton active paragraph={{ rows: 6 }} />
  </div>
);

const App = () => {
  useEffect(() => {
    const prefetchTimer = window.setTimeout(() => {
      void loadTaskList();
      void loadExecutionList();
      void loadTaskForm();
    }, 1_200);
    return () => window.clearTimeout(prefetchTimer);
  }, []);

  return (
    <ConfigProvider
      locale={zhCN}
      theme={{
        token: {
          colorPrimary: '#18181b',
          colorInfo: '#2563eb',
          colorSuccess: '#15803d',
          colorWarning: '#b45309',
          colorError: '#dc2626',
          colorText: '#18181b',
          colorTextSecondary: '#71717a',
          colorTextTertiary: '#a1a1aa',
          colorBorder: '#e4e4e7',
          colorBorderSecondary: '#ededf0',
          colorBgLayout: '#fafafa',
          colorBgContainer: '#ffffff',
          colorBgElevated: '#ffffff',
          colorFillTertiary: '#f4f4f5',
          colorFillQuaternary: '#f4f4f5',
          colorSplit: '#e4e4e7',
          borderRadius: 7,
          borderRadiusLG: 10,
          controlHeight: 36,
          fontFamily: 'Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
          boxShadow: '0 1px 2px rgb(0 0 0 / 0.04)',
          boxShadowSecondary: '0 12px 32px rgb(0 0 0 / 0.12)',
        },
        components: {
          Button: { defaultShadow: 'none', primaryShadow: 'none', dangerShadow: 'none' },
          Card: { boxShadowTertiary: 'none' },
          Menu: {
            itemBg: 'transparent',
            itemColor: '#71717a',
            itemHoverBg: '#f4f4f5',
            itemHoverColor: '#18181b',
            itemSelectedBg: '#f4f4f5',
            itemSelectedColor: '#18181b',
          },
          Table: {
            headerBg: '#f4f4f5',
            headerColor: '#71717a',
            rowHoverBg: '#fafafa',
            borderColor: '#e4e4e7',
          },
        },
      }}
    >
      <BrowserRouter>
        <Suspense fallback={<RouteFallback />}>
          <Routes>
            <Route path="/" element={<MainLayout />}>
              <Route index element={<Navigate to="/tasks" replace />} />
              <Route path="tasks" element={<TaskList />} />
              <Route path="tasks/create" element={<TaskForm />} />
              <Route path="executions" element={<ExecutionList />} />
              <Route path="*" element={<Navigate to="/tasks" replace />} />
            </Route>
          </Routes>
        </Suspense>
      </BrowserRouter>
    </ConfigProvider>
  );
};

export default App;
