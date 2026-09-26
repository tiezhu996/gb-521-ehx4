import { create } from 'zustand';
import * as stoppageApi from '../api/stoppages';
import type { AirStoppage, CreateStoppageInput } from '../types/stoppage';

interface StoppageState {
  stoppages: AirStoppage[];
  loading: boolean;
  load(status?: string): Promise<void>;
  create(input: CreateStoppageInput): Promise<AirStoppage>;
  restore(id: number): Promise<void>;
}

export const useStoppageStore = create<StoppageState>((set) => ({
  stoppages: [], loading: false,
  async load(status) {
    set({ loading: true });
    try { set({ stoppages: (await stoppageApi.listStoppages(status)).items }); } finally { set({ loading: false }); }
  },
  async create(input) {
    const created = await stoppageApi.createStoppage(input);
    set({ stoppages: (await stoppageApi.listStoppages()).items });
    return created;
  },
  async restore(id) {
    await stoppageApi.restoreStoppage(id);
    set({ stoppages: (await stoppageApi.listStoppages()).items });
  },
}));
