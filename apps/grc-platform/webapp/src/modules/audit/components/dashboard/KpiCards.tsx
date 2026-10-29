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
import type { JSX, KeyboardEvent, ReactNode } from "react";

interface KpiCardProps {
  icon: ReactNode;
  iconColor: string;
  value: number;
  label: string;
  sub?: string;
  onClick?: () => void;
  valueColor?: string;
}

function KpiCard({ icon, iconColor, value, label, sub, onClick, valueColor }: KpiCardProps): JSX.Element {
  return (
    <Paper
      variant="outlined"
      onClick={onClick}
      {...(onClick && {
        role: "button",
        tabIndex: 0,
        onKeyDown: (e: KeyboardEvent) => {
          if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onClick(); }
        },
      })}
      sx={{
        borderRadius: 2,
        p: 2,
        minWidth: 0,
        display: "flex",
        alignItems: "center",
        gap: 2,
        ...(onClick && {
          cursor: "pointer",
          transition: "border-color 0.15s, box-shadow 0.15s",
          "&:hover": { borderColor: iconColor, boxShadow: 1 },
        }),
      }}
    >
      <Box
        sx={{
          width: 44, height: 44, borderRadius: 1.5, flexShrink: 0,
          display: "flex", alignItems: "center", justifyContent: "center",
          color: iconColor, bgcolor: `${iconColor}18`,
          "[data-color-scheme='dark'] &": { bgcolor: `${iconColor}33` },
        }}
      >
        {icon}
      </Box>
      <Box sx={{ minWidth: 0 }}>
        <Typography variant="h4" fontWeight={700} lineHeight={1.1} color={valueColor}>
          {value}
        </Typography>
        <Typography variant="body2" color="text.secondary" noWrap>{label}</Typography>
        {sub && (
          <Typography variant="caption" color="text.secondary" noWrap sx={{ display: "block" }}>
            {sub}
          </Typography>
        )}
      </Box>
    </Paper>
  );
}

export interface KpiItem {
  key: string;
  icon: ReactNode;
  iconColor: string;
  value: number;
  label: string;
  sub?: string;
  valueColor?: string;
  onClick?: () => void;
}

// One card per work-queue tab, in tab order. Layout is a fluid grid so seven
// cards wrap cleanly instead of squeezing.
export default function KpiCards({ items }: { items: KpiItem[] }): JSX.Element {
  return (
    <Box sx={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(190px, 1fr))", gap: 2 }}>
      {items.map(({ key, ...item }) => (
        <KpiCard key={key} {...item} />
      ))}
    </Box>
  );
}
