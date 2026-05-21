import React from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import MainLayout from './layouts/MainLayout';
import TaskList from './pages/TaskList';
import TaskForm from './pages/TaskForm';
import ExecutionList from './pages/ExecutionList';

const App: React.FC = () => {
  return (
    <ConfigProvider locale={zhCN}>
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
