export type UserRole = 'viewer' | 'operator' | 'reviewer' | 'admin';

export interface BaseRecord {
  id: number;
  code: string;
  name: string;
  status: string;
  version: number;
  description: string;
  createdAt: string;
  updatedAt: string;
}

// Generic API helpers use the shared base contract when a page does not need
// the entity-specific fields. Keeping the alias here makes those helpers
// type-safe without duplicating a second, divergent record shape.
export type DomainRecord = BaseRecord;

export interface Furnace extends BaseRecord {
  plantArea: string;
  furnaceType: string;
  capacityTonnes: number;
  supportedAlloys: string;
  maxTemperatureC: number;
  operator: string;
  lastInspectionAt: string;
  evidence: string;
}

export interface Heat extends BaseRecord {
  furnaceCode: string;
  alloyGrade: string;
  owner: string;
  chargeWeightKg: number;
  targetTemperatureC: number;
  carbonMinPct: number;
  carbonMaxPct: number;
  siliconMinPct: number;
  siliconMaxPct: number;
  sulfurMaxPct: number;
  phosphorusMaxPct: number;
  startedAt: string;
  evidence: string;
  // Lineage for the "判定返炉" closed loop.
  // remeltOfCode is set on the return heat and points at the rejected origin.
  remeltOfCode?: string;
  // remeltedIntoCode is set on the rejected origin and points at the heat
  // that carried the charge forward.
  remeltedIntoCode?: string;
}

export interface ChemicalSample extends BaseRecord {
  heatCode: string;
  samplePoint: string;
  methodVersion: string;
  analyst: string;
  carbonPct: number;
  siliconPct: number;
  manganesePct: number;
  sulfurPct: number;
  phosphorusPct: number;
  sampledAt: string;
  evidence: string;
}

export interface QualityDecision extends BaseRecord {
  heatCode: string;
  sampleCode: string;
  reviewer: string;
  reason: string;
  conditions: string;
  decidedAt: string;
  evidence: string;
  // Populated once a remelt decision closes its loop; empty otherwise.
  remeltHeatCode?: string;
  remeltFurnaceCode?: string;
  remeltedAt?: string | null;
}

// Furnace currently eligible to receive a rejected heat (available and covering
// the heat's alloy grade, charge weight and target temperature).
export interface RemeltFurnaceOption {
  code: string;
  name: string;
  plantArea: string;
  furnaceType: string;
  capacityTonnes: number;
  maxTemperatureC: number;
  status: string;
  operator: string;
}

// Request body for POST /decisions/:id/remelt.
export interface RemeltInput {
  expectedVersion: number;
  reason: string;
  furnaceCode: string;
  returnHeatCode: string;
  returnHeatName: string;
  owner: string;
  evidence: string;
}

export interface PageMeta {
  page: number;
  pageSize: number;
  total: number;
}

export interface ApiEnvelope<T> {
  data: T;
  error?: string;
  message?: string;
  meta?: PageMeta;
}

export interface UserSession {
  token: string;
  username: string;
  displayName: string;
  role: UserRole;
  expiresIn: number;
}

export interface AuditLog {
  id: number;
  requestId: string;
  actor: string;
  action: string;
  entityType: string;
  entityId: number;
  beforeState: string;
  afterState: string;
  detail: string;
  createdAt: string;
}
