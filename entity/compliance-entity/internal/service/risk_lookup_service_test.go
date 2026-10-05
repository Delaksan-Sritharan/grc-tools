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
	"strings"
	"testing"

	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/apierror"
	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/domain"
	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/repository"
)

// fakeLookupRepo records whether a write reached the repository. Validation
// failures must be caught before that.
type fakeLookupRepo struct {
	repository.RiskLookupRepository
	created *domain.CreateRiskLookupRequest
}

func (f *fakeLookupRepo) CreateRiskLookup(_ context.Context, req domain.CreateRiskLookupRequest) (*domain.RiskLookup, error) {
	f.created = &req
	return &domain.RiskLookup{ID: 1, Name: req.Name, Code: req.Code, Status: "ACTIVE"}, nil
}

func ptr(s string) *string { return &s }

func TestCreateRiskLookup_CustomerCodeRules(t *testing.T) {
	cases := []struct {
		name    string
		code    *string
		wantErr bool
	}{
		{"valid", ptr("BANKONESUB"), false},
		{"twelve chars", ptr("ABCDEFGHIJ12"), false},
		{"missing", nil, true},
		{"lowercase", ptr("bankonesub"), true},
		{"hyphen", ptr("BANK-ONE"), true},
		{"space", ptr("BANK ONE"), true},
		{"thirteen chars", ptr("ABCDEFGHIJ123"), true},
		{"empty", ptr(""), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeLookupRepo{}
			svc := NewRiskLookupService(repo, repository.LookupCustomer)
			_, err := svc.CreateRiskLookup(context.Background(),
				domain.CreateRiskLookupRequest{Name: "Bank One Sub", Code: tc.code, CreatedBy: "admin"})
			if tc.wantErr {
				var ve *apierror.ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("err = %v, want ValidationError", err)
				}
				if repo.created != nil {
					t.Error("invalid input reached the repository")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
}

// A code on a lookup that has none is a client mistake, not something to drop
// silently.
func TestCreateRiskLookup_CodeRejectedForNonCustomer(t *testing.T) {
	repo := &fakeLookupRepo{}
	svc := NewRiskLookupService(repo, repository.LookupProduct)
	_, err := svc.CreateRiskLookup(context.Background(),
		domain.CreateRiskLookupRequest{Name: "API Manager", Code: ptr("APIM"), CreatedBy: "admin"})
	var ve *apierror.ValidationError
	if !errors.As(err, &ve) || repo.created != nil {
		t.Fatalf("err = %v, created = %v; want ValidationError and no write", err, repo.created)
	}
}

func TestCreateRiskLookup_NameTrimmedAndRequired(t *testing.T) {
	repo := &fakeLookupRepo{}
	svc := NewRiskLookupService(repo, repository.LookupPlatform)

	if _, err := svc.CreateRiskLookup(context.Background(),
		domain.CreateRiskLookupRequest{Name: "   ", CreatedBy: "admin"}); err == nil {
		t.Fatal("blank name accepted")
	}
	got, err := svc.CreateRiskLookup(context.Background(),
		domain.CreateRiskLookupRequest{Name: "  Choreo  ", CreatedBy: "admin"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Name != "Choreo" {
		t.Errorf("name = %q, want trimmed %q", got.Name, "Choreo")
	}
}

// A name longer than the column would otherwise reach MySQL and come back as a
// 500; it must be a 400, on create and on rename, counted in characters.
func TestRiskLookupNameLength(t *testing.T) {
	long := strings.Repeat("é", maxLookupNameLen+1) // multi-byte: counts characters, not bytes
	fits := strings.Repeat("é", maxLookupNameLen)
	var ve *apierror.ValidationError

	repo := &fakeLookupRepo{}
	svc := NewRiskLookupService(repo, repository.LookupPlatform)
	if _, err := svc.CreateRiskLookup(context.Background(), domain.CreateRiskLookupRequest{Name: long, CreatedBy: "a"}); !errors.As(err, &ve) {
		t.Fatalf("create with %d chars: err = %v, want ValidationError", maxLookupNameLen+1, err)
	}
	if repo.created != nil {
		t.Error("an over-long name reached the repository")
	}
	if _, err := svc.CreateRiskLookup(context.Background(), domain.CreateRiskLookupRequest{Name: fits, CreatedBy: "a"}); err != nil {
		t.Errorf("create with exactly %d chars: %v", maxLookupNameLen, err)
	}
	if _, err := svc.UpdateRiskLookup(context.Background(), 1, domain.UpdateRiskLookupRequest{Name: &long, UpdatedBy: "a"}); !errors.As(err, &ve) {
		t.Errorf("rename to %d chars: err = %v, want ValidationError", maxLookupNameLen+1, err)
	}
}
