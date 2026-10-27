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
	"strings"
	"time"

	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/apierror"
	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/domain"
	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/repository"
)

type riskChangeLogService struct {
	repo repository.RiskChangeLogRepository
}

// NewRiskChangeLogService constructs a RiskChangeLogService.
func NewRiskChangeLogService(repo repository.RiskChangeLogRepository) RiskChangeLogService {
	return &riskChangeLogService{repo: repo}
}

// validChangeLogActions mirrors risk_change_log.action in risk_schema.sql —
// the field-diff actions plus the workflow events that make up a risk's
// history. Keep in sync with the schema; an unknown action is rejected rather
// than written, since MySQL would silently truncate it to ”.
var validChangeLogActions = map[string]bool{
	"CREATE": true, "UPDATE": true, "DELETE": true,
	"SUBMIT": true, "APPROVE": true, "REJECT": true, "ESCALATE": true,
	"COMMENT": true, "ASSESS": true, "COMPLETE": true, "CLOSE": true, "CANCEL": true,
}

func (s *riskChangeLogService) CreateRiskChangeLog(ctx context.Context, riskID int, req domain.CreateRiskChangeLogRequest) (domain.RiskChangeLog, error) {
	if riskID <= 0 {
		return domain.RiskChangeLog{}, &apierror.ValidationError{Msg: "riskId must be a positive integer"}
	}
	if req.CreatedBy == "" {
		return domain.RiskChangeLog{}, &apierror.ValidationError{Msg: "createdBy is required"}
	}
	req.Action = strings.ToUpper(req.Action)
	if !validChangeLogActions[req.Action] {
		return domain.RiskChangeLog{}, &apierror.ValidationError{Msg: "invalid action: " + req.Action}
	}
	if err := validJSONField("oldValue", req.OldValue); err != nil {
		return domain.RiskChangeLog{}, err
	}
	if err := validJSONField("newValue", req.NewValue); err != nil {
		return domain.RiskChangeLog{}, err
	}
	e, err := s.repo.CreateRiskChangeLog(ctx, riskID, req)
	if err != nil {
		return domain.RiskChangeLog{}, err
	}
	return *e, nil
}

func (s *riskChangeLogService) ListRiskChangeLog(ctx context.Context, riskID int, limit, offset int) (domain.ListRiskChangeLogResponse, error) {
	if riskID <= 0 {
		return domain.ListRiskChangeLogResponse{}, &apierror.ValidationError{Msg: "riskId must be a positive integer"}
	}
	p := domain.Pagination{Limit: limit, Offset: offset}
	normalizePagination(&p)
	entries, total, err := s.repo.ListRiskChangeLog(ctx, riskID, p.Limit, p.Offset)
	if err != nil {
		return domain.ListRiskChangeLogResponse{}, err
	}
	if entries == nil {
		entries = []domain.RiskChangeLog{}
	}
	return domain.ListRiskChangeLogResponse{Changes: entries, Total: total, Limit: p.Limit, Offset: p.Offset}, nil
}

func (s *riskChangeLogService) SearchRiskChangeLog(ctx context.Context, req domain.SearchRiskChangeLogRequest) (domain.SearchRiskChangeLogResponse, error) {
	for i, a := range req.Actions {
		req.Actions[i] = strings.ToUpper(a)
		if !validChangeLogActions[req.Actions[i]] {
			return domain.SearchRiskChangeLogResponse{}, &apierror.ValidationError{Msg: "invalid action: " + a}
		}
	}
	for field, v := range map[string]string{"from": req.From, "to": req.To} {
		if v == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", v); err != nil {
			return domain.SearchRiskChangeLogResponse{}, &apierror.ValidationError{Msg: field + " must be YYYY-MM-DD"}
		}
	}
	p := domain.Pagination{Limit: req.Limit, Offset: req.Offset}
	normalizePagination(&p)
	req.Limit, req.Offset = p.Limit, p.Offset
	items, total, err := s.repo.SearchRiskChangeLog(ctx, req)
	if err != nil {
		return domain.SearchRiskChangeLogResponse{}, err
	}
	return domain.SearchRiskChangeLogResponse{Items: items, Total: total, Limit: p.Limit, Offset: p.Offset}, nil
}
