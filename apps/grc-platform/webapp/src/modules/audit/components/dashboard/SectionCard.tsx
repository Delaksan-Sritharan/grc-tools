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

import { Box, Paper, Typography } from "@wso2/oxygen-ui";
import type { JSX, ReactNode } from "react";

interface SectionCardProps {
  title: string;
  /** Optional control (e.g. the audit-program dropdown) shown beside the title. */
  action?: ReactNode;
  children: ReactNode;
}

export default function SectionCard({ title, action, children }: SectionCardProps): JSX.Element {
  return (
    // height: 100% + flex column lets grid rows stretch all cards equally;
    // children with flex-basis 0 (AuditProgressList/TeamProgress) then fill
    // whatever height the tallest card (e.g. the detailed donut) sets.
    <Paper variant="outlined" sx={{ borderRadius: 2, overflow: "hidden", height: "100%", display: "flex", flexDirection: "column" }}>
      <Box sx={{ px: 2.5, py: 1.5, borderBottom: 1, borderColor: "divider", display: "flex", alignItems: "center", justifyContent: "space-between", gap: 1 }}>
        <Typography variant="subtitle1" fontWeight={700} sx={{ minWidth: 0 }}>{title}</Typography>
        {action && <Box sx={{ flexShrink: 0 }}>{action}</Box>}
      </Box>
      <Box sx={{ p: 2.5, flex: 1, minHeight: 0, display: "flex", flexDirection: "column" }}>{children}</Box>
    </Paper>
  );
}
