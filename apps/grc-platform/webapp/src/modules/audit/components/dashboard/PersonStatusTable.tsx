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

import {
  Alert, Box, Skeleton, Table, TableBody, TableCell, TableContainer, TableHead,
  TablePagination, TableRow, Typography,
} from "@wso2/oxygen-ui";
import type { JSX } from "react";
import { useState } from "react";
import { useGetDashboard } from "@modules/audit/api/useGetDashboard";
import type { Audit, ControlStatus } from "@modules/audit/types/audit";
import { CONTROL_STATUS_COLORS, CONTROL_STATUS_LABELS } from "@modules/audit/utils/controlStatus";
import {
  ALL_PROGRAMS, auditIdsFor, groupByAssignee, OVERDUE_STATUS, toMatrix,
} from "@modules/audit/utils/assigneeProgress";
import AuditProgramFilter from "./AuditProgramFilter";
import SectionCard from "./SectionCard";

const OVERDUE_COLOR = "#E53935";
const PAGE_SIZE = 10;

function statusLabel(s: string): string {
  return s === OVERDUE_STATUS ? "Overdue" : CONTROL_STATUS_LABELS[s as ControlStatus] ?? s;
}
function statusColor(s: string): string {
  return s === OVERDUE_STATUS ? OVERDUE_COLOR : CONTROL_STATUS_COLORS[s as ControlStatus] ?? "#94A3B8";
}

interface PersonStatusTableProps {
  title: string;
  /** Which assignee role's rows to list; one table row per person. */
  role: "auditor" | "owner";
  /** One column per status, in this order. */
  statuses: readonly string[];
  personLabel: string;
  audits: Audit[];
}

// Full-width table: one row per person with a count under every tracked status,
// filterable by audit program and paginated. "All programs" lists everyone with
// work in any program; picking a program lists only the people on that audit.
export default function PersonStatusTable({
  title, role, statuses, personLabel, audits,
}: PersonStatusTableProps): JSX.Element {
  const [program, setProgram] = useState(ALL_PROGRAMS);
  const [page, setPage] = useState(0);
  const { data, isLoading, isError, isPlaceholderData } = useGetDashboard(auditIdsFor(program));

  const distribution = data?.assigneeStatusDistribution;
  const rows = distribution ? toMatrix(groupByAssignee(distribution, role, statuses), statuses) : [];
  // Filtering shrinks the list; keep the page in range.
  const lastPage = Math.max(0, Math.ceil(rows.length / PAGE_SIZE) - 1);
  const safePage = Math.min(page, lastPage);
  const visible = rows.slice(safePage * PAGE_SIZE, (safePage + 1) * PAGE_SIZE);

  let body: JSX.Element;
  if (isLoading) {
    body = <Skeleton variant="rectangular" height={200} sx={{ borderRadius: 1 }} />;
  } else if (isError) {
    body = <Alert severity="error">Failed to load this view.</Alert>;
  } else if (!distribution) {
    body = <Typography variant="body2" color="text.secondary">Not available yet.</Typography>;
  } else if (rows.length === 0) {
    body = <Typography variant="body2" color="text.secondary">Nothing waiting here.</Typography>;
  } else {
    body = (
      <>
        <TableContainer>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell sx={{ fontWeight: 600, whiteSpace: "nowrap" }}>{personLabel}</TableCell>
                {statuses.map((s) => (
                  <TableCell key={s} align="right" sx={{ fontWeight: 600, whiteSpace: "nowrap" }}>
                    <Box sx={{ display: "inline-flex", alignItems: "center", gap: 0.75 }}>
                      <Box sx={{ width: 8, height: 8, borderRadius: "50%", bgcolor: statusColor(s) }} />
                      {statusLabel(s)}
                    </Box>
                  </TableCell>
                ))}
                <TableCell align="right" sx={{ fontWeight: 600 }}>Total</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {visible.map((r) => (
                <TableRow key={r.key} hover>
                  <TableCell sx={{ fontWeight: 600 }}>{r.assignee}</TableCell>
                  {statuses.map((s) => (
                    <TableCell
                      key={s}
                      align="right"
                      sx={{ color: r.counts[s] === 0 ? "text.disabled" : s === OVERDUE_STATUS ? OVERDUE_COLOR : undefined }}
                    >
                      {r.counts[s]}
                    </TableCell>
                  ))}
                  <TableCell align="right" sx={{ fontWeight: 700 }}>{r.total}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
        <TablePagination
          component="div"
          count={rows.length}
          page={safePage}
          onPageChange={(_, p) => setPage(p)}
          rowsPerPage={PAGE_SIZE}
          rowsPerPageOptions={[PAGE_SIZE]}
          sx={{ borderTop: 1, borderColor: "divider" }}
        />
        <Typography variant="caption" color="text.secondary">
          Total counts the four lifecycle statuses. Overdue controls are already counted under their own status, so they are not added again.
        </Typography>
      </>
    );
  }

  return (
    <SectionCard
      title={title}
      action={<AuditProgramFilter audits={audits} value={program} onChange={(v) => { setProgram(v); setPage(0); }} />}
    >
      <Box sx={{ opacity: isPlaceholderData ? 0.5 : 1, transition: "opacity 150ms" }}>{body}</Box>
    </SectionCard>
  );
}
