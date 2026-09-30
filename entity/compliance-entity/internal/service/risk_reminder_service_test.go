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
	"errors"
	"testing"

	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/apierror"
	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/domain"
)

// stubRiskReminderRepo records whether the repository was reached at all —
// what the validation tests below actually assert, since a request that
// reaches MySQL malformed is the failure mode this validation exists to stop.
type stubRiskReminderRepo struct{ called bool }

// ClaimRiskReminder records the call and always wins the claim, with id 1.
func (s *stubRiskReminderRepo) ClaimRiskReminder(context.Context, domain.ClaimRiskReminderRequest) (int64, bool, error) {
	s.called = true
	return 1, true, nil
}

// ReleaseRiskReminderClaim records the call and always succeeds.
func (s *stubRiskReminderRepo) ReleaseRiskReminderClaim(context.Context, int64) error {
	s.called = true
	return nil
}

// TestClaimRiskReminder_RejectsBadRequests checks that every malformed claim
// is a ValidationError and never reaches the repository.
func TestClaimRiskReminder_RejectsBadRequests(t *testing.T) {
	valid := domain.ClaimRiskReminderRequest{RiskID: 42, ReminderType: "DUE_TODAY", DueDateSnapshot: "2026-10-06"}

	cases := map[string]struct {
		mutate func(*domain.ClaimRiskReminderRequest)
	}{
		"zero riskId":         {func(r *domain.ClaimRiskReminderRequest) { r.RiskID = 0 }},
		"negative riskId":     {func(r *domain.ClaimRiskReminderRequest) { r.RiskID = -1 }},
		"unknown tier":        {func(r *domain.ClaimRiskReminderRequest) { r.ReminderType = "DUE_IN_10_DAYS" }},
		"audit tier name":     {func(r *domain.ClaimRiskReminderRequest) { r.ReminderType = "REMINDER_DUE_5" }},
		"empty date":          {func(r *domain.ClaimRiskReminderRequest) { r.DueDateSnapshot = "" }},
		"non-ISO date":        {func(r *domain.ClaimRiskReminderRequest) { r.DueDateSnapshot = "06/10/2026" }},
		"datetime not a date": {func(r *domain.ClaimRiskReminderRequest) { r.DueDateSnapshot = "2026-10-06T00:00:00Z" }},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			repo := &stubRiskReminderRepo{}
			svc := NewRiskReminderService(repo)
			req := valid
			tc.mutate(&req)

			_, _, err := svc.ClaimRiskReminder(context.Background(), req)

			var invalid *apierror.ValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("err = %v, want ValidationError", err)
			}
			if repo.called {
				t.Fatal("repository was called with an invalid request")
			}
		})
	}
}

// TestClaimRiskReminder_AcceptsEveryTier checks that all three tier names
// pass validation and return the repository's claim id.
func TestClaimRiskReminder_AcceptsEveryTier(t *testing.T) {
	for _, tier := range []string{"DUE_IN_15_DAYS", "DUE_IN_5_DAYS", "DUE_TODAY"} {
		t.Run(tier, func(t *testing.T) {
			repo := &stubRiskReminderRepo{}
			svc := NewRiskReminderService(repo)

			claimed, id, err := svc.ClaimRiskReminder(context.Background(), domain.ClaimRiskReminderRequest{
				RiskID: 42, ReminderType: tier, DueDateSnapshot: "2026-10-06",
			})
			if err != nil {
				t.Fatalf("ClaimRiskReminder: %v", err)
			}
			if !claimed || id != 1 {
				t.Fatalf("claimed=%v id=%d, want true/1", claimed, id)
			}
		})
	}
}

// TestReleaseRiskReminderClaim_RejectsNonPositiveID checks that a zero id is
// rejected before the repository is called.
func TestReleaseRiskReminderClaim_RejectsNonPositiveID(t *testing.T) {
	repo := &stubRiskReminderRepo{}
	svc := NewRiskReminderService(repo)

	var invalid *apierror.ValidationError
	if err := svc.ReleaseRiskReminderClaim(context.Background(), 0); !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want ValidationError", err)
	}
	if repo.called {
		t.Fatal("repository was called with an invalid id")
	}
}
