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

// Work-queue tab ids. Shared by the queue itself and the KPI cards above it, so
// each card jumps to the tab it mirrors and both show the same set per role.
export const QUEUE_TAB_AWAITING = 0;
export const QUEUE_TAB_DUE_SOON = 1;
export const QUEUE_TAB_PENDING = 2;
export const QUEUE_TAB_VALIDATION = 3;
export const QUEUE_TAB_OVERDUE = 4;
export const QUEUE_TAB_ALL_PENDING = 5;
export const QUEUE_TAB_COMPLETED = 6;

export interface QueueAccess {
  // canViewAll (AUDIT_VIEW_ALL_AUDITS) sees every tab; narrower roles see the
  // union of the privilege-gated tabs they hold plus the status/date views.
  canViewAll: boolean;
  canApprove: boolean;
  canSubmit: boolean;
  canValidate: boolean;
}

// Privilege-driven tab visibility (never role names). Due Soon, Overdue, All
// Pending and Completed are status/date views over rows the backend has already
// scoped to the caller, so they stay visible to everyone; the rest are gated.
export function isQueueTabVisible(tab: number, a: QueueAccess): boolean {
  if (a.canViewAll) return true;
  switch (tab) {
    case QUEUE_TAB_AWAITING:   return a.canApprove;
    case QUEUE_TAB_PENDING:    return a.canSubmit;
    case QUEUE_TAB_VALIDATION: return a.canValidate;
    default:                   return true;
  }
}
