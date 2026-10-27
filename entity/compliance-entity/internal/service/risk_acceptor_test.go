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
	"testing"

	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/domain"
	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/repository"
)

// acceptorRepo records the request that reaches the repository; any other
// RiskRepository method panics on the nil embedded interface.
type acceptorRepo struct {
	repository.RiskRepository
	created *domain.CreateRiskRequest
	updated *domain.UpdateRiskRequest
}

func (r *acceptorRepo) CreateRisk(_ context.Context, req domain.CreateRiskRequest) (*domain.Risk, error) {
	r.created = &req
	return &domain.Risk{}, nil
}

func (r *acceptorRepo) UpdateRisk(_ context.Context, _ int, req domain.UpdateRiskRequest) (*domain.Risk, error) {
	r.updated = &req
	return &domain.Risk{}, nil
}

func validCreateRequest(acceptor *string) domain.CreateRiskRequest {
	return domain.CreateRiskRequest{
		RiskTitle: "t", SourceRegisterID: 1, AssignmentTeamID: 1, AssignerID: 1, OwnerID: 1,
		RiskYear: 2026, RiskQuarter: "Q4", Likelihood: 1, Impact: 1, CreatedBy: "u",
		ActionSteps:  []domain.ActionStepInput{{Description: "step"}},
		RiskAcceptor: acceptor,
	}
}

func strPtr(s string) *string { return &s }

func TestCreateRiskAcceptor(t *testing.T) {
	t.Run("omitted is accepted and stays nil", func(t *testing.T) {
		repo := &acceptorRepo{}
		if _, err := NewRiskService(repo).CreateRisk(context.Background(), validCreateRequest(nil)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if repo.created.RiskAcceptor != nil {
			t.Errorf("RiskAcceptor = %q, want nil", *repo.created.RiskAcceptor)
		}
	})
	t.Run("lower case is normalised", func(t *testing.T) {
		repo := &acceptorRepo{}
		if _, err := NewRiskService(repo).CreateRisk(context.Background(), validCreateRequest(strPtr("customer"))); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := *repo.created.RiskAcceptor; got != "CUSTOMER" {
			t.Errorf("RiskAcceptor = %q, want CUSTOMER", got)
		}
	})
	t.Run("unknown value is rejected before the repository", func(t *testing.T) {
		repo := &acceptorRepo{}
		if _, err := NewRiskService(repo).CreateRisk(context.Background(), validCreateRequest(strPtr("PARTNER"))); err == nil {
			t.Fatal("expected a validation error")
		}
		if repo.created != nil {
			t.Error("repository was called")
		}
	})
}

func TestUpdateRiskAcceptor(t *testing.T) {
	t.Run("valid value is normalised and passed on", func(t *testing.T) {
		repo := &acceptorRepo{}
		if _, err := NewRiskService(repo).UpdateRisk(context.Background(), 1, domain.UpdateRiskRequest{RiskAcceptor: strPtr("wso2"), UpdatedBy: "u"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := *repo.updated.RiskAcceptor; got != "WSO2" {
			t.Errorf("RiskAcceptor = %q, want WSO2", got)
		}
	})
	t.Run("unknown value is rejected before the repository", func(t *testing.T) {
		repo := &acceptorRepo{}
		if _, err := NewRiskService(repo).UpdateRisk(context.Background(), 1, domain.UpdateRiskRequest{RiskAcceptor: strPtr("PARTNER"), UpdatedBy: "u"}); err == nil {
			t.Fatal("expected a validation error")
		}
		if repo.updated != nil {
			t.Error("repository was called")
		}
	})
}
