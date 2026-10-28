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

import { Alert, Box, Skeleton, Tooltip, Typography } from "@wso2/oxygen-ui";
import type { JSX } from "react";
import { useState } from "react";
import { useGetDashboard } from "@modules/audit/api/useGetDashboard";
import type { Audit, ControlStatus } from "@modules/audit/types/audit";
import type { AssigneeRole } from "@modules/audit/types/dashboard";
import { CONTROL_STATUS_COLORS, CONTROL_STATUS_LABELS } from "@modules/audit/utils/controlStatus";
import { ALL_PROGRAMS, auditIdsFor, groupByAssignee, OVERDUE_STATUS } from "@modules/audit/utils/assigneeProgress";
import AuditProgramFilter from "./AuditProgramFilter";
import SectionCard from "./SectionCard";

const OVERDUE_COLOR = "#E53935";

function statusLabel(s: string): string {
  return s === OVERDUE_STATUS ? "Overdue" : CONTROL_STATUS_LABELS[s as ControlStatus] ?? s;
}
function statusColor(s: string): string {
  return s === OVERDUE_STATUS ? OVERDUE_COLOR : CONTROL_STATUS_COLORS[s as ControlStatus] ?? "#94A3B8";
}

interface RoleProgressCardProps {
  title: string;
  role: AssigneeRole;
  /** The statuses this view tracks — the condition the team is waiting on. */
  statuses: readonly string[];
  audits: Audit[];
}

// Per-assignee workload for one role: a stacked bar per person of the controls
// sitting in the tracked statuses, filterable by audit program.
export default function RoleProgressCard({ title, role, statuses, audits }: RoleProgressCardProps): JSX.Element {
  const [program, setProgram] = useState(ALL_PROGRAMS);
  const { data, isLoading, isError } = useGetDashboard(auditIdsFor(program));

  const distribution = data?.assigneeStatusDistribution;
  const rows = distribution ? groupByAssignee(distribution, role, statuses) : [];
  const max = Math.max(1, ...rows.map((r) => r.byStatus.reduce((n, b) => n + b.count, 0)));

  let body: JSX.Element;
  if (isLoading) {
    body = <Skeleton variant="rectangular" height={160} sx={{ borderRadius: 1 }} />;
  } else if (isError) {
    body = <Alert severity="error">Failed to load this view.</Alert>;
  } else if (!distribution) {
    body = <Typography variant="body2" color="text.secondary">Not available yet.</Typography>;
  } else if (rows.length === 0) {
    body = <Typography variant="body2" color="text.secondary">Nothing waiting here.</Typography>;
  } else {
    body = (
      <Box sx={{ display: "flex", flexDirection: "column", gap: 1.25, maxHeight: 320, overflowY: "auto", pr: 0.5 }}>
        {rows.map((r) => (
          <Box key={r.key}>
            <Box sx={{ display: "flex", justifyContent: "space-between", mb: 0.5 }}>
              <Typography variant="body2" fontWeight={600} noWrap title={r.assignee} sx={{ mr: 1 }}>{r.assignee}</Typography>
              <Typography variant="body2" color="text.secondary">{r.total}</Typography>
            </Box>
            <Box sx={{ display: "flex", height: 8, borderRadius: 4, overflow: "hidden", bgcolor: "action.hover" }}>
              {r.byStatus.map((b) => (
                <Tooltip key={b.status} title={`${statusLabel(b.status)}: ${b.count}`}>
                  <Box sx={{ width: `${(b.count / max) * 100}%`, bgcolor: statusColor(b.status) }} />
                </Tooltip>
              ))}
            </Box>
          </Box>
        ))}
      </Box>
    );
  }

  return (
    <SectionCard
      title={title}
      action={<AuditProgramFilter audits={audits} value={program} onChange={setProgram} />}
    >
      <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1.5, mb: 1.5 }}>
        {statuses.map((s) => (
          <Box key={s} sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
            <Box sx={{ width: 8, height: 8, borderRadius: "50%", bgcolor: statusColor(s) }} />
            <Typography variant="caption" color="text.secondary">{statusLabel(s)}</Typography>
          </Box>
        ))}
      </Box>
      {body}
    </SectionCard>
  );
}
