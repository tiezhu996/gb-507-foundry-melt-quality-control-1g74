
import { request } from './client';
import type { DomainRecord, RemeltFurnaceOption, RemeltInput } from '../types/domain';

export async function listQualityDecision(page = 1, pageSize = 20, search = '') {
  return request<DomainRecord[]>(`/decisions?page=${page}&pageSize=${pageSize}&search=${encodeURIComponent(search)}`);
}
export async function createQualityDecision(input: Partial<DomainRecord>) {
  return request<DomainRecord>('/decisions', { method: 'POST', body: JSON.stringify(input) });
}
export async function transitionQualityDecision(id: number, status: string, expectedVersion: number, reason: string) {
  return request<DomainRecord>(`/decisions/${id}/transition`, {
    method: 'POST', body: JSON.stringify({ status, expectedVersion, reason }),
  });
}

// Read-only preview of the furnaces currently able to receive the decision's
// heat. The backend re-validates on submit, so this list only constrains the UI.
export async function listRemeltFurnaces(decisionId: number) {
  return request<RemeltFurnaceOption[]>(`/decisions/${decisionId}/remelt-furnaces`);
}

// Close the "判定返炉" loop in one submission.
export async function submitRemelt(decisionId: number, input: RemeltInput) {
  return request<DomainRecord>(`/decisions/${decisionId}/remelt`, {
    method: 'POST', body: JSON.stringify(input),
  });
}
