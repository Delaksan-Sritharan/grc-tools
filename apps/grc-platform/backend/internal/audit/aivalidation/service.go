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

// Package aivalidation is the in-process AI Validation feature: it assembles
// the context for one evidence or population submission, calls the LLM with
// a single forced tool call, and writes the advisory result back through the
// Compliance Entity.
//
// Nothing here ever blocks a submission or changes evidence/population/
// control status — every entry point is fire-and-forget from the caller's
// point of view (see Service.TriggerEvidence / TriggerPopulation).
package aivalidation

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/audit/model"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/audit/repository"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/llm"
)

// errSubmissionNotFound signals that the current round wasn't found among an
// evidence submission's own listing (should be impossible outside a race with
// a concurrent delete) — reported distinctly from a fetch failure.
var errSubmissionNotFound = errors.New("submission not found")

// submissionRef identifies which submission a job or log row belongs to.
// Exactly one of EvidenceID / PopulationID is set, mirroring the DB's
// chk_ai_owner constraint — this type is the single place that invariant is
// expressed in this package, instead of a bare int pair threaded through
// every function.
type submissionRef struct {
	EvidenceID   int
	PopulationID int
	ControlID    int
}

func evidenceRef(evidenceID, controlID int) submissionRef {
	return submissionRef{EvidenceID: evidenceID, ControlID: controlID}
}

func populationRef(populationID, controlID int) submissionRef {
	return submissionRef{PopulationID: populationID, ControlID: controlID}
}

func (r submissionRef) isPopulation() bool { return r.PopulationID != 0 }

// logAttr names the owning id under the field name a reader would grep for
// ("evidenceId" or "populationId"), rather than always logging both.
func (r submissionRef) logAttr() slog.Attr {
	if r.isPopulation() {
		return slog.Int("populationId", r.PopulationID)
	}
	return slog.Int("evidenceId", r.EvidenceID)
}

// createdBySentinel is the fixed AIValidationLog.CreatedBy value for every
// row this package writes — there is no real user behind an in-process
// trigger.
const createdBySentinel = "system:ai-validation"

// JobTimeout bounds one validation job (context assembly, the LLM call, and
// the write path together). maxConcurrent is the shared worker-pool size
// across evidence and population jobs. Both are engineering tuning knobs, not
// per-environment settings — unlike AI_VALIDATION_ENABLED/ANTHROPIC_API_KEY,
// there's no real scenario where staging needs a different timeout than
// prod, so they're constants here instead of more env vars to keep in step
// across every deployed environment. Change the constant and redeploy if
// they ever need to move.
const (
	JobTimeout    = 180 * time.Second
	maxConcurrent = 4
)

// Service runs AI Validation jobs. It is safe for concurrent use — each
// Trigger* call spawns its own detached goroutine, and the number running
// the LLM call concurrently is bounded by a shared worker-pool semaphore.
type Service struct {
	llm        llm.Caller
	repo       repository.AIValidationLogRepository
	control    ControlSource
	evidence   EvidenceSource
	population PopulationSource
	comment    CommentSource

	sem chan struct{}
	// enabled is the master switch — AI_VALIDATION_ENABLED plus a non-empty
	// ANTHROPIC_API_KEY (checked at construction; see cmd/server/audit_deps.go).
	// false makes every Trigger* call a pure no-op, same as the old nil
	// *aiagent.Client check.
	enabled bool
}

// NewService constructs a Service. enabled=false makes every Trigger* call a
// no-op — the caller (audit_deps.go) is responsible for resolving the
// AI_VALIDATION_ENABLED + ANTHROPIC_API_KEY guard before calling this.
func NewService(
	caller llm.Caller,
	repo repository.AIValidationLogRepository,
	control ControlSource,
	evidence EvidenceSource,
	population PopulationSource,
	comment CommentSource,
	enabled bool,
) *Service {
	return &Service{
		llm:        caller,
		repo:       repo,
		control:    control,
		evidence:   evidence,
		population: population,
		comment:    comment,
		sem:        make(chan struct{}, maxConcurrent),
		enabled:    enabled,
	}
}

// TriggerEvidence starts (or skips) an advisory AI validation job for one
// evidence submission. Detached from the caller's request context (its own
// context.Background() + timeout) and never blocks or returns an error —
// exactly like the trigger it replaces (internal/shared/aiagent, removed).
// A nil *Service (e.g. a test harness that never wires AIValidationRun) is a
// no-op, same as the old nil *aiagent.Client check.
func (s *Service) TriggerEvidence(auditID, controlID, evidenceID int, actor string, skip bool) {
	if s == nil || !s.enabled {
		return
	}
	go s.runEvidence(auditID, controlID, evidenceID, actor, skip)
}

// TriggerPopulation is TriggerEvidence for a population submission.
func (s *Service) TriggerPopulation(auditID, controlID, populationID int, actor string, skip bool) {
	if s == nil || !s.enabled {
		return
	}
	go s.runPopulation(auditID, controlID, populationID, actor, skip)
}

// submissionFetch loads whatever is specific to one submission kind: its
// file blocks, the manifest text describing them, and any "previous rounds"
// context (evidence only — population has none). Returning
// errSubmissionNotFound distinguishes "the round vanished" from an ordinary
// fetch failure so run can report each with its original message.
type submissionFetch func(ctx context.Context) (blocks []llm.Block, manifest, previous string, err error)

func (s *Service) runEvidence(auditID, controlID, evidenceID int, actor string, skip bool) {
	s.run(auditID, evidenceRef(evidenceID, controlID), actor, skip, submissionEvidence, func(ctx context.Context) ([]llm.Block, string, string, error) {
		rounds, err := s.evidence.List(ctx, auditID, controlID, true)
		if err != nil {
			return nil, "", "", err
		}
		var current *model.AuditEvidence
		for _, r := range rounds {
			if r.ID == evidenceID {
				current = r
				break
			}
		}
		if current == nil {
			return nil, "", "", errSubmissionNotFound
		}
		blocks, manifest := buildFileContent(ctx, s.evidence, evidenceFileRefs(current.Files))
		previous := previousEvidenceContext(ctx, s.evidence, s.comment, auditID, controlID, evidenceID)
		return blocks, manifest, previous, nil
	})
}

func (s *Service) runPopulation(auditID, controlID, populationID int, actor string, skip bool) {
	s.run(auditID, populationRef(populationID, controlID), actor, skip, submissionPopulation, func(ctx context.Context) ([]llm.Block, string, string, error) {
		files, err := s.population.ListFiles(ctx, populationID)
		if err != nil {
			return nil, "", "", err
		}
		blocks, manifest := buildFileContent(ctx, s.population, populationFileRefs(files))
		// Population multi-round context assembly is an open item, not yet
		// settled, so no "previous rounds" text here.
		return blocks, manifest, "", nil
	})
}

// run is the shared job lifecycle for both an evidence and a population
// submission: opt-out check, PENDING row, worker-pool slot, control lookup,
// the submission-kind-specific fetch, the LLM call, and the terminal row.
// fetch supplies the one piece of behaviour that actually differs between
// the two submission kinds.
func (s *Service) run(auditID int, ref submissionRef, actor string, skip bool, kind submissionKind, fetch submissionFetch) {
	if skip {
		s.writeSkipped(context.Background(), ref)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), JobTimeout)
	defer cancel()

	s.writePending(ctx, ref)
	s.sem <- struct{}{}
	defer func() { <-s.sem }()

	control, err := s.control.GetByID(ctx, auditID, ref.ControlID)
	if err != nil || control == nil {
		s.writeError(ctx, ref, "could not load the control")
		slog.Warn("ai validation: control lookup failed", ref.logAttr(), "controlId", ref.ControlID, "actor", actor, "err", err)
		return
	}

	blocks, manifest, previous, err := fetch(ctx)
	if err != nil {
		if errors.Is(err, errSubmissionNotFound) {
			s.writeError(ctx, ref, "submission not found")
			return
		}
		s.writeError(ctx, ref, "could not load the submission")
		slog.Warn("ai validation: submission fetch failed", ref.logAttr(), "actor", actor, "err", err)
		return
	}
	if len(blocks) == 0 {
		s.writeError(ctx, ref, "no readable files in this submission")
		return
	}

	result, err := s.call(ctx, control, kind, manifest, previous, blocks)
	if err != nil {
		s.writeError(ctx, ref, "AI validation could not complete")
		slog.Warn("ai validation: call failed", ref.logAttr(), "actor", actor, "err", err)
		return
	}
	s.writeResult(ctx, ref, result)
}

// call assembles the user-turn content (manifest + optional previous-rounds
// context + file blocks) and the system prompt, then makes the one forced
// tool call.
func (s *Service) call(ctx context.Context, control *model.AuditControl, kind submissionKind, manifest, previousContext string, fileBlocks []llm.Block) (*validationResult, error) {
	var intro strings.Builder
	intro.WriteString("Review this submission.\n\n")
	intro.WriteString(manifest)
	if previousContext != "" {
		intro.WriteString("\n")
		intro.WriteString(previousContext)
	}

	content := make([]llm.Block, 0, len(fileBlocks)+1)
	content = append(content, llm.NewTextBlock(intro.String()))
	content = append(content, fileBlocks...)

	res, err := s.llm.Call(ctx, llm.Request{
		SystemStatic:  staticSystemPrompt,
		SystemDynamic: buildDynamicPrompt(control, kind),
		Content:       content,
		Tool:          submitValidationTool(),
	})
	if err != nil {
		return nil, err
	}
	var vr validationResult
	if err := json.Unmarshal(res.ToolInput, &vr); err != nil {
		return nil, err
	}
	vr.Result = strings.ToUpper(strings.TrimSpace(vr.Result))
	return &vr, nil
}

func (s *Service) writePending(ctx context.Context, ref submissionRef) {
	s.writeRow(ctx, ref, model.CreateAIValidationLogRequest{
		ControlID: ref.ControlID,
		Result:    "PENDING",
		CreatedBy: createdBySentinel,
	})
}

func (s *Service) writeSkipped(ctx context.Context, ref submissionRef) {
	s.writeRow(ctx, ref, model.CreateAIValidationLogRequest{
		ControlID: ref.ControlID,
		Result:    "SKIPPED",
		CreatedBy: createdBySentinel,
	})
}

func (s *Service) writeError(ctx context.Context, ref submissionRef, summary string) {
	s.writeRow(ctx, ref, model.CreateAIValidationLogRequest{
		ControlID: ref.ControlID,
		Result:    "ERROR",
		Summary:   &summary,
		CreatedBy: createdBySentinel,
	})
}

func (s *Service) writeResult(ctx context.Context, ref submissionRef, vr *validationResult) {
	gapsJSON, err := json.Marshal(vr.GapsFound)
	if err != nil {
		gapsJSON = []byte("[]")
	}
	feedbackJSON, err := json.Marshal(vr.Feedback)
	if err != nil {
		feedbackJSON = []byte("[]")
	}
	gaps, feedback, summary := string(gapsJSON), string(feedbackJSON), vr.Summary
	confidence := clampConfidence(vr.ConfidenceScore)

	result := vr.Result
	if result != "PASS" && result != "FAIL" && result != "UNCERTAIN" {
		// The model didn't return one of the three verdicts the tool schema
		// enumerates — treat as UNCERTAIN rather than reject a validation the
		// job otherwise completed (advisory only; a human still decides).
		result = "UNCERTAIN"
	}

	s.writeRow(ctx, ref, model.CreateAIValidationLogRequest{
		ControlID:       ref.ControlID,
		Result:          result,
		GapsFound:       &gaps,
		Feedback:        &feedback,
		Summary:         &summary,
		ConfidenceScore: &confidence,
		CreatedBy:       createdBySentinel,
	})
}

func clampConfidence(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}

// writeRow appends one row, best-effort. A failure here is logged and
// swallowed — there is no retry, and no way to surface it to anyone but the
// logs, since this always runs detached from a request.
func (s *Service) writeRow(ctx context.Context, ref submissionRef, req model.CreateAIValidationLogRequest) {
	var err error
	if ref.isPopulation() {
		err = s.repo.CreateForPopulation(ctx, ref.PopulationID, req)
	} else {
		err = s.repo.CreateForEvidence(ctx, ref.EvidenceID, req)
	}
	if err != nil {
		slog.Warn("ai validation: failed to write result row",
			ref.logAttr(), "result", req.Result, "err", err)
	}
}
