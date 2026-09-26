import type { AirwayEdge } from './edge';

export type StoppageStatus = 'stopping' | 'restored';

export interface StoppageArrangementInput {
  node_id: number;
  note: string;
}

export interface CreateStoppageInput {
  edge_id: number;
  reason: string;
  planned_restore_at: string;
  arrangements: StoppageArrangementInput[];
}

export interface WorkfaceAirDeficit {
  node_id: number;
  code: string;
  required_m3s: number;
  normal_m3s: number;
  stopped_m3s: number;
  deficit_m3s: number;
  arrangement?: string;
}

export interface EdgeAirLoss {
  edge_id: number;
  code: string;
  normal_m3s: number;
  stopped_m3s: number;
  loss_m3s: number;
}

export interface ZeroAirWorkface {
  node_id: number;
  code: string;
  cut_edge_ids: number[];
  cut_edge_codes: string[];
}

export interface StoppageEvaluation {
  scenario_id: number;
  solver_status: string;
  included_edge_ids: number[];
  deficits: WorkfaceAirDeficit[];
  edge_losses: EdgeAirLoss[];
  zero_air_workfaces: ZeroAirWorkface[];
}

export interface AirStoppage {
  id: number;
  edge_id: number;
  reason: string;
  planned_restore_at: string;
  status: StoppageStatus;
  evaluation_json: StoppageEvaluation;
  created_by: number;
  restored_by?: number;
  restored_at?: string;
  edge?: AirwayEdge;
  created_at: string;
  updated_at: string;
}
