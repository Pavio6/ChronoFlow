import React from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import MainLayout from './layouts/MainLayout';
import TaskList from './pages/TaskList';
import TaskForm from './pages/TaskForm';
import ExecutionList from './pages/ExecutionList';
import './App.css';

const App: React.FC = () => {
  return (
    <ConfigProvider
      locale={zhCN}
      theme={{
        token: {
          colorPrimary: '#18181b',
          colorSuccess: '#16a34a',
          colorWarning: '#d97706',
          colorError: '#dc2626',
          colorInfo: '#2563eb',
          colorText: '#18181b',
          colorTextSecondary: '#71717a',
          colorBorder: '#e4e4e7',
          colorBgLayout: '#fafafa',
          colorBgContainer: '#ffffff',
          borderRadius: 6,
          fontFamily:
            'ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
          controlHeight: 36,
        },
        components: {
          Button: {
            borderRadius: 6,
            defaultShadow: 'none',
            primaryShadow: 'none',
          },
          Card: {
            borderRadiusLG: 8,
            boxShadowTertiary: 'none',
          },
          Table: {
            borderColor: '#d4d4d8',
            headerBg: '#fafafa',
            headerColor: '#52525b',
            rowHoverBg: '#fafafa',
          },
          Select: {
            optionActiveBg: '#f4f4f5',
            optionSelectedBg: '#f4f4f5',
            optionSelectedColor: '#18181b',
          },
          Tag: {
            borderRadiusSM: 999,
          },
        },
      }}
    >
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<MainLayout />}>
            <Route index element={<Navigate to="/tasks" replace />} />
            <Route path="tasks" element={<TaskList />} />
            <Route path="tasks/create" element={<TaskForm />} />
            <Route path="tasks/edit/:id" element={<TaskForm />} />
            <Route path="executions" element={<ExecutionList />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </ConfigProvider>
  );
};

export default App;
