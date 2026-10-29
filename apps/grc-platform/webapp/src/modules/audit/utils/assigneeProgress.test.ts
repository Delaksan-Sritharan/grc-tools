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

import { describe, expect, it } from "vitest";
import { ALL_PROGRAMS, auditIdsFor, groupByAssignee, toMatrix } from "./assigneeProgress";

const rows = [
  { role: "auditor" as const, assignee: "Ann", status: "EVIDENCE_UNDER_VALIDATION", count: 3 },
  { role: "auditor" as const, assignee: "Ann", status: "POPULATION_UNDER_VALIDATION", count: 2 },
  { role: "auditor" as const, assignee: "Bob", status: "EVIDENCE_UNDER_VALIDATION", count: 7 },
  { role: "auditor" as const, assignee: "Bob", status: "COMPLETE", count: 9 },
  { role: "owner" as const, assignee: "Cy", status: "EVIDENCE_PENDING", count: 4 },
  { role: "owner" as const, assignee: "Cy", status: "OVERDUE", count: 1 },
];

describe("groupByAssignee", () => {
  it("filters by role and status, sorted by workload", () => {
    const out = groupByAssignee(rows, "auditor", ["EVIDENCE_UNDER_VALIDATION", "POPULATION_UNDER_VALIDATION"]);
    expect(out.map((r) => [r.assignee, r.total])).toEqual([["Bob", 7], ["Ann", 5]]);
  });

  it("does not sum the OVERDUE pseudo-status into the total", () => {
    const [cy] = groupByAssignee(rows, "owner", ["EVIDENCE_PENDING", "OVERDUE"]);
    expect(cy.total).toBe(4);
    expect(cy.byStatus).toHaveLength(2);
  });

  it("returns nothing when no rows match", () => {
    expect(groupByAssignee(rows, "compliance", ["EVIDENCE_INTERNAL_REVIEW"])).toEqual([]);
  });
});

describe("toMatrix", () => {
  it("fills every tracked status, zero where the person holds none", () => {
    const statuses = ["EVIDENCE_PENDING", "POPULATION_PENDING", "OVERDUE"];
    const [cy] = toMatrix(groupByAssignee(rows, "owner", statuses), statuses);
    expect(cy.counts).toEqual({ EVIDENCE_PENDING: 4, POPULATION_PENDING: 0, OVERDUE: 1 });
    expect(cy.total).toBe(4);
  });
});

describe("groupByAssignee identity", () => {
  it("keeps people with unresolved names apart, and labels them honestly", () => {
    const out = groupByAssignee(
      [
        { role: "owner", assignee: "", assigneeUuid: "u1", status: "EVIDENCE_PENDING", count: 2 },
        { role: "owner", assignee: "", assigneeUuid: "u2", status: "EVIDENCE_PENDING", count: 1 },
        { role: "owner", assignee: "", status: "EVIDENCE_PENDING", count: 3 },
      ],
      "owner",
      ["EVIDENCE_PENDING"],
    );
    expect(out.map((r) => [r.assignee, r.total])).toEqual([["Unassigned", 3], ["Unknown user", 2], ["Unknown user", 1]]);
  });
});

describe("auditIdsFor", () => {
  it("maps a selected audit to its id", () => {
    expect(auditIdsFor("12")).toEqual([12]);
  });

  it("treats the all-programs sentinel and unusable values as no filter", () => {
    for (const v of [ALL_PROGRAMS, "", "0", "-3", "abc", "1.5"]) {
      expect(auditIdsFor(v)).toEqual([]);
    }
  });
});
