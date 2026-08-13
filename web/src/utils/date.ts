import dayjs from 'dayjs';

export const formatDateTime = (value: string | null | undefined) =>
  value ? dayjs(value).format('YYYY/MM/DD HH:mm:ss') : '—';
