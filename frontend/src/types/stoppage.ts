import type { AirwayEdge } from './edge';

export type StoppageStatus = 'active' | 'recovered';

export interface WorkfaceArrangement {
  workface_node_id: number;
  arrangement: string;
}

export interface StoppageWorkfaceImpact {
  workface_node_id: number;
  workface_code: string;
  required_airflow_m3s: number;
  baseline_airflow_m3s: number;
  projected_airflow_m3s: number;
  shortfall_m3s: number;
  arrangement_needed: boolean;
}

export interface StoppageZeroAirWorkface {
  workface_node_id: number;
  workface_code: string;
  cutting_edge_id: number;
  cutting_edge_code: string;
  reason: string;
}

export interface StoppageEdgeFlowLoss {
  edge_id: number;
  edge_code: string;
  baseline_flow_m3s: number;
  projected_flow_m3s: number;
  flow_loss_m3s: number;
  closed: boolean;
  already_stopped: boolean;
}

export interface StoppageAssessment {
  scenario_id: number;
  solver_status: string;
  combined_edge_ids: number[];
  new_edge_id: number;
  active_edge_ids: number[];
  can_register: boolean;
  workface_impacts: StoppageWorkfaceImpact[];
  zero_air_workfaces: StoppageZeroAirWorkface[];
  edge_flow_losses: StoppageEdgeFlowLoss[];
}

export interface VentilationStoppage {
  id: number;
  edge_id: number;
  status: StoppageStatus;
  reason: string;
  planned_restore_at: string;
  actual_restore_at?: string;
  baseline_door_state: string;
  impact_json: StoppageAssessment;
  arrangement_json: Record<string, string>;
  registered_by: number;
  recovered_by?: number;
  edge?: AirwayEdge;
  created_at: string;
  updated_at: string;
}

export interface CreateStoppageInput {
  edge_id: number;
  reason: string;
  planned_restore_at: string;
  arrangements: WorkfaceArrangement[];
}
