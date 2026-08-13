import { Alert, Button } from 'antd';

interface LoadErrorProps {
  message: string;
  onRetry: () => void;
}

const LoadError = ({ message, onRetry }: LoadErrorProps) => (
  <Alert
    type="error"
    showIcon
    title="数据加载失败"
    description={message}
    action={<Button size="small" onClick={onRetry}>重新加载</Button>}
  />
);

export default LoadError;
