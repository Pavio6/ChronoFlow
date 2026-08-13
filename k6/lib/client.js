import http from 'k6/http';

export const baseURL = (__ENV.BASE_URL || 'http://127.0.0.1:8080').replace(/\/+$/, '');

// apiParams 为请求设置统一请求头和低基数指标名称
export function apiParams(operation) {
  const headers = {
    'Content-Type': 'application/json',
  };
  if (__ENV.API_KEY) {
    headers['X-API-Key'] = __ENV.API_KEY;
  }
  return {
    headers,
    tags: { name: operation },
    timeout: __ENV.REQUEST_TIMEOUT || '10s',
  };
}

// parseJSON 安全解析响应，解析失败时返回 null
export function parseJSON(response) {
  try {
    return response.json();
  } catch (_) {
    return null;
  }
}

// deleteTimer 清理指定 Timer
export function deleteTimer(id) {
  return http.del(`${baseURL}/api/v1/timers/${id}`, null, apiParams('timer_delete'));
}

// deactivateTimer 停用指定 Timer
export function deactivateTimer(id) {
  return http.post(
    `${baseURL}/api/v1/timers/${id}/deactivate`,
    null,
    apiParams('timer_deactivate'),
  );
}
