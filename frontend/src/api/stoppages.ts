import { request, requestPage } from './client';
import type { CreateStoppageInput, StoppageAssessment, VentilationStoppage } from '../types/stoppage';

export const listStoppages = (status?: 'active' | 'recovered', edgeId?: number) => {
  const query = new URLSearchParams({ page_size: '100' });
  if (status) query.set('status', status);
  if (edgeId) query.set('edge_id', String(edgeId));
  return requestPage<VentilationStoppage>(`/api/v1/stoppages?${query}`);
};

export const getStoppage = (id: number) => request<VentilationStoppage>(`/api/v1/stoppages/${id}`);

export const previewStoppage = (edgeId: number, plannedRestoreAt: string) =>
  request<StoppageAssessment>('/api/v1/stoppages/preview', {
    method: 'POST',
    body: JSON.stringify({ edge_id: edgeId, planned_restore_at: plannedRestoreAt }),
  });

export const createStoppage = (input: CreateStoppageInput) =>
  request<VentilationStoppage>('/api/v1/stoppages', { method: 'POST', body: JSON.stringify(input) });

export const recoverStoppage = (id: number, note?: string) =>
  request<VentilationStoppage>(`/api/v1/stoppages/${id}/recover`, { method: 'POST', body: JSON.stringify({ note: note ?? '' }) });
