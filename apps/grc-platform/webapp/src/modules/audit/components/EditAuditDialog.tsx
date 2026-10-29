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
  Alert,
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  TextField,
} from "@wso2/oxygen-ui";
import { type JSX, useEffect, useState } from "react";
import { type EditAuditFields, useEditAudit } from "@modules/audit/api/useEditAudit";
import { auditDisplayName } from "@modules/audit/utils/format";
import type { Audit } from "@modules/audit/types/audit";

const MAX_NAME_LENGTH = 200;

interface EditAuditDialogProps {
  /** The audit being edited; the dialog is open while this is set. */
  audit: Audit | null;
  onClose: () => void;
  onSaved: () => void;
}

export default function EditAuditDialog({ audit, onClose, onSaved }: EditAuditDialogProps): JSX.Element {
  // Escape/backdrop dismissal while a save is in flight would unmount the form and drop onSaved.
  const [saving, setSaving] = useState(false);

  return (
    <Dialog open={Boolean(audit)} onClose={saving ? undefined : onClose} maxWidth="sm" fullWidth>
      {/* Keyed by audit so the form state resets for each audit it opens on. */}
      {audit && (
        <EditForm key={audit.id} audit={audit} onClose={onClose} onSaved={onSaved} onPendingChange={setSaving} />
      )}
    </Dialog>
  );
}

function EditForm({
  audit,
  onClose,
  onSaved,
  onPendingChange,
}: {
  audit: Audit;
  onClose: () => void;
  onSaved: () => void;
  onPendingChange: (pending: boolean) => void;
}): JSX.Element {
  const edit = useEditAudit();

  useEffect(() => {
    onPendingChange(edit.isPending);
  }, [edit.isPending, onPendingChange]);
  const currentName = auditDisplayName(audit);
  const currentScope = audit.scopeDescription ?? "";

  const [name, setName] = useState(currentName);
  const [periodStart, setPeriodStart] = useState(audit.periodStart);
  const [periodEnd, setPeriodEnd] = useState(audit.periodEnd);
  const [scope, setScope] = useState(currentScope);

  // Only what actually changed is sent, so an untouched field is never rewritten.
  const changes: EditAuditFields = {};
  if (name.trim() !== currentName) changes.displayName = name.trim();
  if (periodStart !== audit.periodStart) changes.periodStart = periodStart;
  if (periodEnd !== audit.periodEnd) changes.periodEnd = periodEnd;
  if (scope.trim() !== currentScope.trim()) changes.scopeDescription = scope.trim();

  const periodInvalid = !periodStart || !periodEnd || periodEnd < periodStart;
  const canSave =
    Object.keys(changes).length > 0 && name.trim().length > 0 && !periodInvalid && !edit.isPending;

  const handleSave = () => {
    if (!canSave) return;
    edit.mutate({ auditId: audit.id, fields: changes }, { onSuccess: onSaved });
  };

  // Clears a stale server error as soon as the user changes anything.
  const touched = () => {
    if (edit.isError) edit.reset();
  };

  return (
    <>
      <DialogTitle>Edit audit</DialogTitle>
      <DialogContent sx={{ display: "flex", flexDirection: "column", gap: 2, pt: "16px !important" }}>
        <DialogContentText>
          Renaming changes only the name shown in the app. Files already uploaded for this audit are not moved.
        </DialogContentText>
        <TextField
          autoFocus
          fullWidth
          size="small"
          label="Audit name"
          value={name}
          onChange={(e) => {
            setName(e.target.value);
            touched();
          }}
          slotProps={{ htmlInput: { maxLength: MAX_NAME_LENGTH } }}
          disabled={edit.isPending}
        />
        <Box sx={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 2 }}>
          <TextField
            label="Period start"
            type="date"
            size="small"
            value={periodStart}
            onChange={(e) => {
              setPeriodStart(e.target.value);
              touched();
            }}
            InputLabelProps={{ shrink: true }}
            disabled={edit.isPending}
          />
          <TextField
            label="Period end"
            type="date"
            size="small"
            value={periodEnd}
            onChange={(e) => {
              setPeriodEnd(e.target.value);
              touched();
            }}
            InputLabelProps={{ shrink: true }}
            inputProps={{ min: periodStart || undefined }}
            error={Boolean(periodStart && periodEnd && periodEnd < periodStart)}
            helperText={
              periodStart && periodEnd && periodEnd < periodStart
                ? "Period end cannot be before period start."
                : undefined
            }
            disabled={edit.isPending}
          />
        </Box>
        <TextField
          label="Scope description"
          value={scope}
          onChange={(e) => {
            setScope(e.target.value);
            touched();
          }}
          multiline
          rows={3}
          fullWidth
          placeholder="Optional - describe what systems, processes, or criteria are in scope."
          disabled={edit.isPending}
        />
        {edit.isError && <Alert severity="error">{edit.error.message}</Alert>}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={edit.isPending}>
          Cancel
        </Button>
        <Button variant="contained" onClick={handleSave} disabled={!canSave}>
          {edit.isPending ? "Saving…" : "Save"}
        </Button>
      </DialogActions>
    </>
  );
}
