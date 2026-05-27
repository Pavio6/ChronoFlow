import React, { useState } from 'react';
import { Button, Spin, Tag } from 'antd';
import { ExportOutlined, ReloadOutlined } from '@ant-design/icons';

const dashboardUrl = '/grafana/d/chronoflow-overview/chronoflow-overview?orgId=1&kiosk&refresh=10s&theme=light';

const Monitoring: React.FC = () => {
  const [loaded, setLoaded] = useState(false);
  const [revision, setRevision] = useState(0);

  const reload = () => {
    setLoaded(false);
    setRevision((current) => current + 1);
  };

  return (
    <div className="page-stack monitor-embed-page">
      <div className="monitor-embed-toolbar">
        <div>
          <strong>Grafana Dashboard</strong>
          <p>图表由 Grafana 渲染，数据来自实时调度任务执行产生的 Prometheus 指标。</p>
        </div>
        <div className="monitor-embed-actions">
          <Tag color={loaded ? 'success' : 'processing'}>{loaded ? 'Dashboard connected' : '加载中'}</Tag>
          <Button icon={<ReloadOutlined />} onClick={reload}>刷新</Button>
          <Button icon={<ExportOutlined />} href={dashboardUrl} target="_blank" rel="noreferrer">
            在 Grafana 打开
          </Button>
        </div>
      </div>
      <section className="surface grafana-frame-shell">
        {!loaded && (
          <div className="grafana-frame-loading">
            <Spin />
            <span>正在加载 Grafana Dashboard</span>
          </div>
        )}
        <iframe
          key={revision}
          className={`grafana-frame ${loaded ? 'is-ready' : ''}`}
          title="ChronoFlow Grafana Dashboard"
          src={dashboardUrl}
          onLoad={() => setLoaded(true)}
        />
      </section>
    </div>
  );
};

export default Monitoring;
