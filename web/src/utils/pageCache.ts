import type {
  ExecutionListParams,
  ExecutionListResponse,
  TimerListParams,
  TimerListResponse,
} from '../types';

interface PageCache<T, P> {
  data: T;
  params: P;
  savedAt: number;
}

const cacheTTL = 30_000;

let timerListCache: PageCache<TimerListResponse, TimerListParams> | null = null;
let executionListCache: PageCache<ExecutionListResponse, ExecutionListParams> | null = null;

const timerListKey = (params: TimerListParams) =>
  [params.page, params.page_size, params.app ?? '', params.status ?? '', params.keyword ?? ''].join('|');

const executionListKey = (params: ExecutionListParams) =>
  [params.page, params.page_size, params.timer_id ?? '', params.timer_name ?? '', params.status ?? ''].join('|');

export const getTimerListCache = () => timerListCache;

export const saveTimerListCache = (data: TimerListResponse, params: TimerListParams) => {
  timerListCache = { data, params, savedAt: Date.now() };
};

export const hasFreshTimerListCache = (params: TimerListParams) =>
  timerListCache !== null &&
  timerListKey(timerListCache.params) === timerListKey(params) &&
  Date.now() - timerListCache.savedAt < cacheTTL;

export const getExecutionListCache = () => executionListCache;

export const saveExecutionListCache = (
  data: ExecutionListResponse,
  params: ExecutionListParams,
) => {
  executionListCache = { data, params, savedAt: Date.now() };
};

export const hasFreshExecutionListCache = (params: ExecutionListParams) =>
  executionListCache !== null &&
  executionListKey(executionListCache.params) === executionListKey(params) &&
  Date.now() - executionListCache.savedAt < cacheTTL;
