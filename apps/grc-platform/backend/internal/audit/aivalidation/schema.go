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

package aivalidation

import "github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/llm"

// submitValidationToolName is the one tool the model is forced to call —
// tool_choice locks the response to exactly this, so a successful prompt
// injection can still only move these fields within the schema below.
const submitValidationToolName = "submit_validation_result"

// gap is one entry of gaps_found — locked to the shape useGetAIValidation.ts
// parses on the frontend.
type gap struct {
	RequirementAspect string `json:"requirementAspect"`
	Issue             string `json:"issue"`
	Severity          string `json:"severity"` // HIGH | MEDIUM | LOW
	FileName          string `json:"fileName,omitempty"`
}

// validationResult is submit_validation_result's input, unmarshaled from the
// forced tool call.
type validationResult struct {
	Result          string   `json:"result"` // PASS | FAIL | UNCERTAIN
	Summary         string   `json:"summary"`
	ConfidenceScore float64  `json:"confidence_score"`
	GapsFound       []gap    `json:"gaps_found"`
	Feedback        []string `json:"feedback"`
}

// submitValidationTool builds the forced-tool definition. The schema is
// intentionally exactly the shape validationResult unmarshals into.
func submitValidationTool() llm.Tool {
	gapSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"requirementAspect": map[string]any{"type": "string"},
			"issue":             map[string]any{"type": "string"},
			"severity":          map[string]any{"type": "string", "enum": []string{"HIGH", "MEDIUM", "LOW"}},
			"fileName":          map[string]any{"type": "string"},
		},
		"required": []string{"requirementAspect", "issue", "severity"},
	}
	return llm.Tool{
		Name: submitValidationToolName,
		Description: "Report the result of validating one evidence or population submission against " +
			"the control's evidence requirement and the Standing Evidence Rules.",
		Properties: map[string]any{
			"result":  map[string]any{"type": "string", "enum": []string{"PASS", "FAIL", "UNCERTAIN"}},
			"summary": map[string]any{"type": "string"},
			"confidence_score": map[string]any{
				"type": "number", "minimum": 0, "maximum": 1,
			},
			"gaps_found": map[string]any{"type": "array", "items": gapSchema},
			"feedback":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		Required: []string{"result", "summary", "confidence_score", "gaps_found", "feedback"},
	}
}
