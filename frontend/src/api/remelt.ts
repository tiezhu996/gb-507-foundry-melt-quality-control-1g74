
import { request } from './client';
import type { RemeltFurnaceOption, RemeltHandoverInput, RemeltHandoverResult } from '../types/domain';

// Lists the furnaces that can currently承接 a return-to-furnace heat. Furnaces
// that fail availability, alloy, capacity or temperature checks are returned
// with explicit mismatch reasons so the UI can explain why they are disabled.
export async function listRemeltFurnaces(heatCode: string) {
  return request<RemeltFurnaceOption[]>(`/heats/remelt-furnaces?heatCode=${encodeURIComponent(heatCode)}`);
}

export async function submitRemeltHandover(decisionId: number, input: RemeltHandoverInput) {
  return request<RemeltHandoverResult>(`/decisions/${decisionId}/remelt`, {
    method: 'POST', body: JSON.stringify(input),
  });
}
