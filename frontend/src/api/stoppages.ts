import { request, requestPage } from './client';
import type { AirStoppage, CreateStoppageInput, StoppageEvaluation } from '../types/stoppage';

export const listStoppages = (status?: string) =>
  requestPage<AirStoppage>(`/api/v1/stoppages?page_size=100${status ? `&status=${status}` : ''}`);
export const previewStoppage = (edge_id: number) =>
  request<StoppageEvaluation>('/api/v1/stoppages/preview', { method: 'POST', body: JSON.stringify({ edge_id }) });
export const createStoppage = (input: CreateStoppageInput) =>
  request<AirStoppage>('/api/v1/stoppages', { method: 'POST', body: JSON.stringify(input) });
export const restoreStoppage = (id: number) =>
  request<AirStoppage>(`/api/v1/stoppages/${id}/restore`, { method: 'POST' });
