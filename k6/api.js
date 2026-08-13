import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate } from 'k6/metrics';

import { apiParams, baseURL, deleteTimer, parseJSON } from './lib/client.js';

const businessErrors = new Rate('api_business_errors');
const runID = __ENV.RUN_ID || `${Date.now()}`;
const iterationSleep = Number.parseFloat(__ENV.ITERATION_SLEEP_SECONDS || '0.1');

export const options = {
  scenarios: {
    timer_api: {
      executor: 'constant-vus',
      vus: Number.parseInt(__ENV.VUS || '10', 10),
      duration: __ENV.DURATION || '5m',
      gracefulStop: '10s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.001'],
    http_req_duration: ['p(95)<200', 'p(99)<500'],
    api_business_errors: ['rate<0.001'],
  },
};

// default 持续执行 Timer 创建、详情查询和删除，测量 API 与 MySQL 写入能力
export default function () {
  const name = `k6-api-${runID}-${__VU}-${__ITER}`;
  const createResponse = http.post(
    `${baseURL}/api/v1/timers`,
    JSON.stringify({
      app: 'k6-api',
      name,
      cron_expr: '* * * * * *',
      callback_url: 'https://example.com/chronoflow-k6',
      callback_method: 'POST',
      callback_body: '{"source":"k6"}',
      misfire_policy: 'FIRE_ONCE',
      max_catch_up: 10,
    }),
    apiParams('timer_create'),
  );
  const createPayload = parseJSON(createResponse);
  const timerID = createPayload && createPayload.data ? createPayload.data.id : 0;
  const created = check(createResponse, {
    'timer create returns 201': (response) => response.status === 201,
    'timer create returns id': () => Number.isInteger(timerID) && timerID > 0,
  });
  businessErrors.add(!created);
  if (!created) {
    sleep(iterationSleep);
    return;
  }

  const getResponse = http.get(
    `${baseURL}/api/v1/timers/${timerID}`,
    apiParams('timer_get'),
  );
  const fetched = check(getResponse, {
    'timer get returns 200': (response) => response.status === 200,
  });
  businessErrors.add(!fetched);

  const deleteResponse = deleteTimer(timerID);
  const deleted = check(deleteResponse, {
    'timer delete returns 200': (response) => response.status === 200,
  });
  businessErrors.add(!deleted);
  sleep(iterationSleep);
}
