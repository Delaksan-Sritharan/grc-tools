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

import { Box, CircularProgress, Collapse, LinearProgress, Typography } from "@wso2/oxygen-ui";
import { AlertTriangle, Bot, ChevronDown, ChevronRight, Sparkles } from "@wso2/oxygen-ui-icons-react";
import { useState, type JSX } from "react";
import { useGetEvidence } from "@modules/audit/api/useGetEvidence";
import { useGetPopulation } from "@modules/audit/api/useGetPopulation";
import {
  isFreshPending,
  parseFeedback,
  parseGaps,
  useGetAIValidation,
  useGetPopulationAIValidation,
  type AIGap,
  type AIValidationLog,
} from "@modules/audit/api/useGetAIValidation";
import { useAuditPrivileges } from "@modules/audit/hooks/useAuditPrivileges";
import { AuditPrivilege } from "@modules/audit/privileges";

const AI_PURPLE = "#7c3aed";
const AI_PURPLE_BG = "#faf5ff";

// Severity → dot colour for the gap list.
const SEVERITY_COLOR: Record<AIGap["severity"], string> = {
  HIGH: "#dc2626",
  MEDIUM: "#b45309",
  LOW: "#6b7280",
};

// Terminal/skipped result → row label + colour.
const RESULT_STYLE: Record<"PASS" | "FAIL" | "UNCERTAIN" | "SKIPPED", { label: string; color: string }> = {
  PASS:      { label: "AI: Looks Complete",                 color: "#16a34a" },
  FAIL:      { label: "AI: Gaps Found",                      color: "#dc2626" },
  UNCERTAIN: { label: "AI: Needs Human Review",              color: "#b45309" },
  SKIPPED:   { label: "AI validation skipped by submitter",  color: "#6b7280" },
};

const ADVISORY_SUBMITTER = "AI-generated hint — does not affect review status.";
const ADVISORY_REVIEWER = "Advisory only — your decision is authoritative.";

// Single named kill switch.
const AI_VALIDATION_ENABLED = true;

interface AIValidationCardProps {
  auditId: number;
  controlId: number;
  variant: "submitter" | "reviewer";
  /** Which submission this card validates. Defaults to "evidence". */
  phase?: "evidence" | "population";
}

/**
 * AIValidationCard renders the advisory AI pre-review for a control's latest
 * evidence or population submission. Internal-only — an external caller
 * never sees this at all, regardless of caller.
 *
 * Deliberately NOT a card/box: the default rendering is always a single
 * line — icon, state, and (for a terminal verdict only) a chevron — never a
 * bordered panel, always collapsed by default. PENDING, SKIPPED,
 * and ERROR rows have nothing further to show, so they stay a fixed line
 * with no expand affordance. Only a terminal verdict (PASS/FAIL/UNCERTAIN)
 * is clickable, and only then does clicking it reveal a boxed detail panel
 * below the line (summary, confidence, gaps, feedback) — manual click only,
 * never auto-expanded.
 */
export default function AIValidationCard({ auditId, controlId, variant, phase = "evidence" }: AIValidationCardProps): JSX.Element | null {
  const { can, loading: privilegesLoading } = useAuditPrivileges();
  const isInternal = can(AuditPrivilege.ViewInternalComments);

  // Computed once; every phase-dependent pick below keys off this instead of
  // re-testing `phase` at each call site.
  const isPopulation = phase === "population";
  const evidenceEnabled = !isPopulation && AI_VALIDATION_ENABLED && isInternal;
  const populationEnabled = isPopulation && AI_VALIDATION_ENABLED && isInternal;

  const { data: submissions } = useGetEvidence(auditId, controlId, evidenceEnabled);
  const { data: population } = useGetPopulation(auditId, controlId, populationEnabled);
  const latestEvidenceId = evidenceEnabled ? (submissions?.[0]?.id ?? null) : null;
  const latestPopulationId = populationEnabled ? (population?.round?.id ?? null) : null;

  const evidenceValidations = useGetAIValidation(auditId, controlId, evidenceEnabled ? latestEvidenceId : null);
  const populationValidations = useGetPopulationAIValidation(
    auditId,
    controlId,
    populationEnabled ? latestPopulationId : null,
  );
  const { data: validations, isLoading } = isPopulation ? populationValidations : evidenceValidations;
  const latestId = isPopulation ? latestPopulationId : latestEvidenceId;

  if (!AI_VALIDATION_ENABLED || privilegesLoading || !isInternal) return null;

  const latest = validations?.[0];

  // Reviewer variant stays out of the way until there is something to show.
  if (variant === "reviewer" && (latestId === null || !latest)) {
    return null;
  }

  // No submission yet: nothing has run, nothing to expand.
  if (latestId === null || (!latest && !isLoading)) {
    return <StaticLine icon={<Bot size={14} />} text="AI review runs automatically after you submit." />;
  }
  if (!latest) {
    return <StaticLine icon={<CircularProgress size={12} />} text="Loading AI review…" />;
  }

  return <AIValidationRow latest={latest} variant={variant} />;
}

/** A single muted line — icon + text, no box, no border. */
function StaticLine({ icon, text }: { icon: JSX.Element; text: string }): JSX.Element {
  return (
    <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
      <Box sx={{ display: "flex", color: "text.secondary", flexShrink: 0 }}>{icon}</Box>
      <Typography variant="body2" color="text.secondary">
        {text}
      </Typography>
    </Box>
  );
}

function AIValidationRow({ latest, variant }: { latest: AIValidationLog; variant: "submitter" | "reviewer" }): JSX.Element {
  const [expanded, setExpanded] = useState(false);

  if (isFreshPending(latest)) {
    return (
      <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
        <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
          <Sparkles size={14} color={AI_PURPLE} />
          <Typography variant="body2" color="text.secondary">
            Analyzing evidence…
          </Typography>
        </Box>
        <LinearProgress
          sx={{
            height: 3,
            borderRadius: 1,
            "& .MuiLinearProgress-bar": { bgcolor: AI_PURPLE },
            bgcolor: AI_PURPLE_BG,
            "[data-color-scheme='dark'] &": { bgcolor: `${AI_PURPLE}33` },
          }}
        />
      </Box>
    );
  }

  // ERROR, or a PENDING row that never resolved (stale): unavailable.
  if (latest.result === "ERROR" || latest.result === "PENDING") {
    return <StaticLine icon={<AlertTriangle size={14} color="#b45309" />} text="AI validation unavailable — proceed as usual" />;
  }

  if (latest.result === "SKIPPED") {
    const style = RESULT_STYLE.SKIPPED;
    return <StaticLine icon={<Sparkles size={14} color={style.color} />} text={style.label} />;
  }

  const style = RESULT_STYLE[latest.result];
  const gaps = parseGaps(latest.gapsFound);

  return (
    <Box>
      <Box
        role="button"
        tabIndex={0}
        aria-expanded={expanded}
        onClick={() => setExpanded((v) => !v)}
        onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); setExpanded((v) => !v); } }}
        sx={{ display: "flex", alignItems: "center", gap: 1, cursor: "pointer", border: "none", background: "none", p: 0, width: "100%", textAlign: "left" }}
      >
        <Sparkles size={14} color={style.color} />
        <Typography variant="body2" fontWeight={600} sx={{ color: style.color }}>
          {style.label}
        </Typography>
        {latest.result === "FAIL" && gaps.length > 0 && (
          <Typography variant="caption" color="text.secondary">· {gaps.length} {gaps.length === 1 ? "gap" : "gaps"}</Typography>
        )}
        <Box sx={{ flex: 1 }} />
        {expanded ? <ChevronDown size={15} /> : <ChevronRight size={15} />}
      </Box>
      <Collapse in={expanded}>
        <Box sx={{ mt: 1.25, p: 1.75, borderRadius: 2, bgcolor: "action.hover" }}>
          {variant === "reviewer" ? <ReviewerVerdict latest={latest} /> : <SubmitterVerdict latest={latest} />}
        </Box>
      </Collapse>
    </Box>
  );
}

function Confidence({ score }: { score: number | null }): JSX.Element | null {
  if (score === null || score === undefined) return null;
  return (
    <Typography variant="caption" color="text.secondary">
      Confidence: {Math.round(score * 100)}%
    </Typography>
  );
}

function GapList({ gaps }: { gaps: AIGap[] }): JSX.Element {
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
      {gaps.map((g, i) => (
        <Box key={i} sx={{ display: "flex", gap: 1 }}>
          <Box sx={{ mt: 0.65, width: 8, height: 8, borderRadius: "50%", bgcolor: SEVERITY_COLOR[g.severity] ?? "#6b7280", flexShrink: 0 }} />
          <Box>
            <Typography variant="body2" sx={{ fontWeight: 600, lineHeight: 1.5 }}>
              {g.severity} · {g.requirementAspect}
              {g.fileName ? (
                <Typography component="span" variant="caption" color="text.secondary">
                  {" "}
                  ({g.fileName})
                </Typography>
              ) : null}
            </Typography>
            <Typography variant="body2" color="text.secondary" sx={{ lineHeight: 1.55 }}>
              {g.issue}
            </Typography>
          </Box>
        </Box>
      ))}
    </Box>
  );
}

function SubmitterVerdict({ latest }: { latest: AIValidationLog }): JSX.Element {
  const gaps = parseGaps(latest.gapsFound);
  const feedback = parseFeedback(latest.feedback);

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
      {latest.summary && (
        <Typography variant="body2" sx={{ lineHeight: 1.7 }}>
          {latest.summary}
        </Typography>
      )}
      <Confidence score={latest.confidenceScore} />

      {gaps.length > 0 && (
        <Box>
          <Typography variant="body2" fontWeight={600} sx={{ mb: 0.75 }}>
            {gaps.length} {gaps.length === 1 ? "gap" : "gaps"} found
          </Typography>
          <GapList gaps={gaps} />
        </Box>
      )}

      {feedback.length > 0 && (
        <Box>
          <Typography variant="body2" fontWeight={600} sx={{ mb: 0.75 }}>
            Suggested fixes before review:
          </Typography>
          <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
            {feedback.map((f, i) => (
              <Box key={i} sx={{ display: "flex", gap: 1 }}>
                <Typography variant="body2" color="text.secondary" sx={{ lineHeight: 1.6 }}>
                  ☐ {f}
                </Typography>
              </Box>
            ))}
          </Box>
        </Box>
      )}

      <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5 }}>
        ⓘ {ADVISORY_SUBMITTER}
      </Typography>
    </Box>
  );
}

function ReviewerVerdict({ latest }: { latest: AIValidationLog }): JSX.Element {
  const gaps = parseGaps(latest.gapsFound);

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
      {latest.confidenceScore !== null && <Confidence score={latest.confidenceScore} />}

      {latest.summary && (
        <Typography variant="body2" sx={{ lineHeight: 1.6 }}>
          {latest.summary}
        </Typography>
      )}

      {gaps.length > 0 && <GapList gaps={gaps} />}

      <Typography variant="caption" color="text.secondary">
        ⓘ {ADVISORY_REVIEWER}
      </Typography>
    </Box>
  );
}
