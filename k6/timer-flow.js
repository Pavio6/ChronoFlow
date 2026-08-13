import http from 'k6/http';
import { check, sleep } from 'k6';
import { Gauge, Rate } from 'k6/metrics';

import {
  apiParams,
  baseURL,
  deactivateTimer,
  deleteTimer,
  parseJSON,
} from './lib/client.js';

const callbackBaseURL = (__ENV.CALLBACK_BASE_URL || 'http://127.0.0.1:9091').replace(/\/+$/, '');
const dispatcherMetricsURL = __ENV.DISPATCHER_METRICS_URL || 'http://127.0.0.1:8082/metrics';
const workerMetricsURL = __ENV.WORKER_METRICS_URL || 'http://127.0.0.1:8083/metrics';
const timerCount = Number.parseInt(__ENV.TIMER_COUNT || '10', 10);
const pollSeconds = Number.parseFloat(__ENV.POLL_INTERVAL_SECONDS || '1');
const drainTimeoutSeconds = Number.parseInt(__ENV.DRAIN_TIMEOUT_SECONDS || '30', 10);
const runID = __ENV.RUN_ID || `${Date.now()}`;

const flowErrors = new Rate('flow_business_errors');
const executionTotal = new Gauge('flow_execution_total');
const executionFailed = new Gauge('flow_execution_failed');
const executionUnfinished = new Gauge('flow_execution_unfinished');
const missingExecutionRecords = new Gauge('flow_missing_execution_records');
const callbackUnique = new Gauge('flow_callback_unique_total');
const callbackDuplicates = new Gauge('flow_callback_duplicate_total');
const callbackMissingKeys = new Gauge('flow_callback_missing_key_total');
const outboxUnpublished = new Gauge('flow_outbox_unpublished');
const workerPending = new Gauge('flow_worker_pending');
const finalMismatch = new Gauge('flow_final_mismatch_total');

export const options = {
  setupTimeout: '10m',
  teardownTimeout: '10m',
  scenarios: {
    observe_timer_flow: {
      executor: 'constant-vus',
      vus: 1,
      duration: __ENV.DURATION || '5m',
      gracefulStop: '5s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.001'],
    flow_business_errors: ['rate<0.001'],
    flow_execution_failed: ['value==0'],
    flow_execution_unfinished: ['value==0'],
    flow_missing_execution_records: ['value==0'],
    flow_callback_duplicate_total: ['value==0'],
    flow_callback_missing_key_total: ['value==0'],
    flow_outbox_unpublished: ['value==0'],
    flow_worker_pending: ['value==0'],
    flow_final_mismatch_total: ['value==0'],
  },
};

// setup 重置回调计数器，并创建、激活指定数量的每秒 Timer
export function setup() {
  assertPositiveInteger(timerCount, 'TIMER_COUNT');
  assertPositiveInteger(drainTimeoutSeconds, 'DRAIN_TIMEOUT_SECONDS');

  const healthResponse = http.get(`${baseURL}/health`, apiParams('api_health'));
  if (healthResponse.status !== 200) {
    throw new Error(`API is not healthy: status=${healthResponse.status}`);
  }
  const resetResponse = http.post(
    `${callbackBaseURL}/reset`,
    null,
    { tags: { name: 'callback_reset' }, timeout: '5s' },
  );
  if (resetResponse.status !== 204) {
    throw new Error(`callback receiver reset failed: status=${resetResponse.status}`);
  }

  const prefix = `k6-flow-${runID}`;
  const timerIDs = [];
  for (let index = 0; index < timerCount; index += 1) {
    const createResponse = http.post(
      `${baseURL}/api/v1/timers`,
      JSON.stringify({
        app: 'k6-flow',
        name: `${prefix}-${index}`,
        cron_expr: '* * * * * *',
        callback_url: `${callbackBaseURL}/callback`,
        callback_method: 'POST',
        callback_body: JSON.stringify({ run_id: runID, timer_index: index }),
        misfire_policy: 'FIRE_ONCE',
        max_catch_up: 10,
      }),
      apiParams('flow_timer_create'),
    );
    const payload = parseJSON(createResponse);
    const timerID = payload && payload.data ? payload.data.id : 0;
    if (createResponse.status !== 201 || !Number.isInteger(timerID) || timerID < 1) {
      cleanupTimers(timerIDs);
      throw new Error(`create timer failed at index=${index}, status=${createResponse.status}`);
    }
    timerIDs.push(timerID);

    const activateResponse = http.post(
      `${baseURL}/api/v1/timers/${timerID}/activate`,
      null,
      apiParams('flow_timer_activate'),
    );
    if (activateResponse.status !== 200) {
      cleanupTimers(timerIDs);
      throw new Error(`activate timer failed: id=${timerID}, status=${activateResponse.status}`);
    }
  }

  return {
    prefix,
    timerIDs,
    measurementStartedAt: Date.now(),
  };
}

// default 低频轮询执行状态和积压指标，不额外制造业务负载
export default function (data) {
  const snapshot = observe(data.prefix);
  const observed = check(snapshot, {
    'execution statistics are available': (value) => value.executions !== null,
    'callback statistics are available': (value) => value.callback !== null,
    'dispatcher metrics are available': (value) => value.outbox !== null,
    'worker metrics are available': (value) => value.pending !== null,
  });
  flowErrors.add(!observed);
  sleep(pollSeconds);
}

// teardown 停用 Timer，等待链路排空，输出最终结果并逻辑删除测试数据
export function teardown(data) {
  const stoppedAt = Date.now();
  for (const timerID of data.timerIDs) {
    const response = deactivateTimer(timerID);
    flowErrors.add(response.status !== 200);
  }

  const deadline = Date.now() + drainTimeoutSeconds * 1000;
  let snapshot = observe(data.prefix);
  while (!isDrained(snapshot) && Date.now() < deadline) {
    sleep(1);
    snapshot = observe(data.prefix);
  }

  const elapsedWholeSeconds = Math.max(
    0,
    Math.floor((stoppedAt - data.measurementStartedAt) / 1000) - 1,
  );
  const expectedMinimum = data.timerIDs.length * elapsedWholeSeconds;
  const actualExecutions = snapshot.executions ? snapshot.executions.total : 0;
  const missing = Math.max(0, expectedMinimum - actualExecutions);
  missingExecutionRecords.add(missing);

  const successfulExecutions = snapshot.executions ? snapshot.executions.success : 0;
  const uniqueCallbacks = snapshot.callback ? snapshot.callback.unique : 0;
  finalMismatch.add(Math.abs(successfulExecutions - uniqueCallbacks));

  console.log(JSON.stringify({
    run_id: runID,
    timer_count: data.timerIDs.length,
    measured_seconds: elapsedWholeSeconds,
    expected_minimum_executions: expectedMinimum,
    executions: snapshot.executions,
    callbacks: snapshot.callback,
    outbox_unpublished: snapshot.outbox,
    worker_pending: snapshot.pending,
  }));

  cleanupTimers(data.timerIDs, false);
}

// observe 读取 Execution、回调、Outbox 和 Redis Pending 的当前快照
function observe(prefix) {
  const executions = readExecutions(prefix);
  const callback = readCallbackStats();
  const outbox = readPrometheusGauge(
    dispatcherMetricsURL,
    'chronoflow_outbox_unpublished_count',
    'dispatcher_metrics',
  );
  const pending = readPrometheusGauge(
    workerMetricsURL,
    'chronoflow_worker_pending_messages',
    'worker_metrics',
  );

  if (executions) {
    executionTotal.add(executions.total);
    executionFailed.add(executions.failed);
    executionUnfinished.add(executions.unfinished);
  }
  if (callback) {
    callbackUnique.add(callback.unique);
    callbackDuplicates.add(callback.duplicates);
    callbackMissingKeys.add(callback.missing_idempotency_key);
  }
  if (outbox !== null) {
    outboxUnpublished.add(outbox);
  }
  if (pending !== null) {
    workerPending.add(pending);
  }
  return { executions, callback, outbox, pending };
}

// readExecutions 汇总本轮 Timer 的 Execution 状态
function readExecutions(prefix) {
  const response = http.get(
    `${baseURL}/api/v1/executions?page=1&page_size=1&timer_name=${encodeURIComponent(prefix)}`,
    apiParams('flow_execution_stats'),
  );
  const payload = parseJSON(response);
  if (response.status !== 200 || !payload || !payload.data) {
    return null;
  }
  const stats = payload.data.stats || {};
  return {
    total: payload.data.total || 0,
    success: stats.SUCCESS || 0,
    failed: stats.FAILED || 0,
    unfinished: (stats.PENDING || 0) + (stats.RUNNING || 0) + (stats.RETRY_WAIT || 0),
  };
}

// readCallbackStats 获取回调接收器统计
function readCallbackStats() {
  const response = http.get(
    `${callbackBaseURL}/stats`,
    { tags: { name: 'callback_stats' }, timeout: '5s' },
  );
  const payload = parseJSON(response);
  if (response.status !== 200 || !payload) {
    return null;
  }
  return payload;
}

// readPrometheusGauge 从角色的 /metrics 文本中读取单个 Gauge
function readPrometheusGauge(url, metricName, operation) {
  const response = http.get(url, { tags: { name: operation }, timeout: '5s' });
  if (response.status !== 200) {
    return null;
  }
  const pattern = new RegExp(`^${metricName}\\s+(-?[0-9.]+)$`, 'm');
  const match = response.body.match(pattern);
  return match ? Number.parseFloat(match[1]) : null;
}

// isDrained 判断所有持久化任务与消息积压是否已经排空
function isDrained(snapshot) {
  return snapshot.executions !== null &&
    snapshot.executions.unfinished === 0 &&
    snapshot.outbox === 0 &&
    snapshot.pending === 0;
}

// cleanupTimers 尽可能停用并删除本轮创建的 Timer
function cleanupTimers(timerIDs, deactivate = true) {
  for (const timerID of timerIDs) {
    if (deactivate) {
      deactivateTimer(timerID);
    }
    deleteTimer(timerID);
  }
}

// assertPositiveInteger 校验正整数环境变量
function assertPositiveInteger(value, name) {
  if (!Number.isInteger(value) || value < 1) {
    throw new Error(`${name} must be a positive integer`);
  }
}
