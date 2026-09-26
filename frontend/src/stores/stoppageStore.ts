import { create } from 'zustand';
import * as stoppageApi from '../api/stoppages';
import type { CreateStoppageInput, StoppageAssessment, VentilationStoppage } from '../types/stoppage';

interface StoppageState {
  stoppages: VentilationStoppage[];
  active: VentilationStoppage[];
  loading: boolean;
  load(status?: 'active' | 'recovered'): Promise<VentilationStoppage[]>;
  loadActive(): Promise<VentilationStoppage[]>;
  create(input: CreateStoppageInput): Promise<VentilationStoppage>;
  preview(edgeId: number, plannedRestoreAt: string): Promise<StoppageAssessment>;
  recover(id: number, note?: string): Promise<VentilationStoppage>;
}

export const useStoppageStore = create<StoppageState>((set, get) => ({
  stoppages: [],
  active: [],
  loading: false,
  async load(status) {
    set({ loading: true });
    try {
      const { items } = await stoppageApi.listStoppages(status);
      set({ stoppages: items });
      return items;
    } finally {
      set({ loading: false });
    }
  },
  async loadActive() {
    const { items } = await stoppageApi.listStoppages('active');
    set({ active: items });
    return items;
  },
  async create(input) {
    const created = await stoppageApi.createStoppage(input);
    await Promise.all([get().loadActive(), get().load()]);
    return created;
  },
  preview(edgeId, plannedRestoreAt) {
    return stoppageApi.previewStoppage(edgeId, plannedRestoreAt);
  },
  async recover(id, note) {
    const recovered = await stoppageApi.recoverStoppage(id, note);
    await Promise.all([get().loadActive(), get().load()]);
    return recovered;
  },
}));
