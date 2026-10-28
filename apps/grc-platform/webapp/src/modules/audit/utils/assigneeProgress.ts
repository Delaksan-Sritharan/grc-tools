// Copyright (c) 2026 WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

import type { AssigneeRole, AssigneeStatusCount } from "@modules/audit/types/dashboard";

export interface AssigneeRow {
  /** Stable key: the person's id when known, else their display name. */
  key: string;
  assignee: string;
  total: number;
  byStatus: { status: string; count: number }[];
}

// Pseudo-status the entity emits for an assignee's past-due controls.
export const OVERDUE_STATUS = "OVERDUE";

// Groups the role's rows by assignee, keeping only the statuses in `statuses`
// (the view's condition) and dropping assignees left with nothing. Largest
// workload first so the people holding the most open work lead the list.
export function groupByAssignee(
  rows: AssigneeStatusCount[],
  role: AssigneeRole,
  statuses: readonly string[],
): AssigneeRow[] {
  const keep = new Set(statuses);
  const byPerson = new Map<string, { name: string; counts: Map<string, number> }>();
  for (const r of rows) {
    if (r.role !== role || !keep.has(r.status) || r.count <= 0) continue;
    // Key by id so two people whose names failed to resolve stay separate rows;
    // only a control with nobody assigned (no id, no name) is "Unassigned".
    const key = r.assigneeUuid || r.assignee || "";
    const name = r.assignee || (r.assigneeUuid ? "Unknown user" : "Unassigned");
    const entry = byPerson.get(key) ?? { name, counts: new Map<string, number>() };
    entry.counts.set(r.status, (entry.counts.get(r.status) ?? 0) + r.count);
    byPerson.set(key, entry);
  }
  return [...byPerson.entries()]
    .map(([key, { name, counts }]) => {
      const byStatus = statuses
        .filter((s) => counts.has(s))
        .map((status) => ({ status, count: counts.get(status) ?? 0 }));
      // OVERDUE overlaps the lifecycle statuses, so it is shown but not summed.
      const total = byStatus.filter((b) => b.status !== OVERDUE_STATUS).reduce((n, b) => n + b.count, 0);
      return { key, assignee: name, total, byStatus };
    })
    .sort((a, b) => b.total - a.total || a.assignee.localeCompare(b.assignee));
}

// A person's counts for every tracked status (zero where they hold none), for
// the table view's fixed columns.
export interface AssigneeMatrixRow {
  key: string;
  assignee: string;
  total: number;
  counts: Record<string, number>;
}

export function toMatrix(rows: AssigneeRow[], statuses: readonly string[]): AssigneeMatrixRow[] {
  return rows.map((r) => {
    const counts: Record<string, number> = Object.fromEntries(statuses.map((s) => [s, 0]));
    for (const b of r.byStatus) counts[b.status] = b.count;
    return { key: r.key, assignee: r.assignee, total: r.total, counts };
  });
}

// Dropdown value meaning "no audit-program filter".
export const ALL_PROGRAMS = "all";

// auditIdsFor converts the dropdown value into the hook's auditIds argument.
// Anything that isn't a positive integer (the "all" sentinel, an empty value)
// means no filter — never audit 0.
export function auditIdsFor(value: string): number[] {
  const id = Number(value);
  return Number.isInteger(id) && id > 0 ? [id] : [];
}
