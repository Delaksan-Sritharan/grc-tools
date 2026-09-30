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
// KIND, either express or implied. See the License for the
// specific language governing permissions and limitations
// under the License.

package service

import (
	"context"
	"regexp"

	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/apierror"
	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/domain"
	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/repository"
)

type riskReminderService struct {
	repo repository.RiskReminderRepository
}

// NewRiskReminderService constructs a RiskReminderService.
func NewRiskReminderService(repo repository.RiskReminderRepository) RiskReminderService {
	return &riskReminderService{repo: repo}
}

// validReminderTypes are the three due-date tiers, matching risk_reminder's
// ENUM. Validated here so a typo comes back as a 400 naming the bad value
// rather than MySQL's opaque truncation error.
var validRiskReminderTypes = map[string]bool{
	"DUE_IN_15_DAYS": true,
	"DUE_IN_5_DAYS":  true,
	"DUE_TODAY":      true,
}

// isoDate matches the YYYY-MM-DD the API speaks. A malformed date would
// otherwise reach MySQL and be coerced to '0000-00-00' or rejected, either of
// which breaks de-dup silently — the whole point of the claim.
var isoDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// ClaimRiskReminder validates the request before it reaches MySQL, then
// makes the claim. claimed=false with a nil error means another caller
// already holds it.
func (s *riskReminderService) ClaimRiskReminder(ctx context.Context, req domain.ClaimRiskReminderRequest) (bool, int64, error) {
	if req.RiskID <= 0 {
		return false, 0, &apierror.ValidationError{Msg: "riskId must be a positive integer"}
	}
	if !validRiskReminderTypes[req.ReminderType] {
		return false, 0, &apierror.ValidationError{Msg: "invalid reminderType: " + req.ReminderType}
	}
	if !isoDate.MatchString(req.DueDateSnapshot) {
		return false, 0, &apierror.ValidationError{Msg: "dueDateSnapshot must be a date in YYYY-MM-DD format"}
	}
	id, claimed, err := s.repo.ClaimRiskReminder(ctx, req)
	if err != nil || !claimed {
		return false, 0, err
	}
	return true, id, nil
}

// ReleaseRiskReminderClaim validates the id, then deletes that claim row.
func (s *riskReminderService) ReleaseRiskReminderClaim(ctx context.Context, id int64) error {
	if id <= 0 {
		return &apierror.ValidationError{Msg: "id must be a positive integer"}
	}
	return s.repo.ReleaseRiskReminderClaim(ctx, id)
}
