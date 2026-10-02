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

package repository

import (
	"errors"
	"testing"

	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/apierror"
	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/domain"
)

// msFields is a complete, valid set of Managed Services fields.
func msFields() domain.CreateRiskRequest {
	return domain.CreateRiskRequest{
		CustomerID:       intPtr(1),
		DeploymentTypeID: intPtr(2),
		ProductIDs:       []int{3},
		Environments:     []string{"PRODUCTION"},
	}
}

func TestCheckTemplateFields(t *testing.T) {
	cases := []struct {
		name     string
		template string
		req      func() domain.CreateRiskRequest
		wantErr  bool
	}{
		{"standard, nothing extra", TemplateStandard, func() domain.CreateRiskRequest {
			return domain.CreateRiskRequest{ComplianceReferenceIDs: []int{1}}
		}, false},
		{"standard rejects platform", TemplateStandard, func() domain.CreateRiskRequest {
			return domain.CreateRiskRequest{PlatformIDs: []int{1}}
		}, true},
		{"standard rejects customer", TemplateStandard, func() domain.CreateRiskRequest {
			return domain.CreateRiskRequest{CustomerID: intPtr(1)}
		}, true},

		{"aggregated with platforms and compliance refs", TemplateAggregated, func() domain.CreateRiskRequest {
			return domain.CreateRiskRequest{PlatformIDs: []int{1, 2}, ComplianceReferenceIDs: []int{1}}
		}, false},
		{"aggregated requires a platform", TemplateAggregated, func() domain.CreateRiskRequest {
			return domain.CreateRiskRequest{}
		}, true},
		{"aggregated rejects MS fields", TemplateAggregated, func() domain.CreateRiskRequest {
			r := msFields()
			r.PlatformIDs = []int{1}
			return r
		}, true},

		{"managed services complete", TemplateManagedServices, msFields, false},
		{"managed services requires customer", TemplateManagedServices, func() domain.CreateRiskRequest {
			r := msFields()
			r.CustomerID = nil
			return r
		}, true},
		{"managed services requires deployment type", TemplateManagedServices, func() domain.CreateRiskRequest {
			r := msFields()
			r.DeploymentTypeID = nil
			return r
		}, true},
		{"managed services requires a product", TemplateManagedServices, func() domain.CreateRiskRequest {
			r := msFields()
			r.ProductIDs = nil
			return r
		}, true},
		{"managed services requires an environment", TemplateManagedServices, func() domain.CreateRiskRequest {
			r := msFields()
			r.Environments = nil
			return r
		}, true},
		{"managed services rejects compliance refs", TemplateManagedServices, func() domain.CreateRiskRequest {
			r := msFields()
			r.ComplianceReferenceIDs = []int{1}
			return r
		}, true},
		{"managed services rejects platforms", TemplateManagedServices, func() domain.CreateRiskRequest {
			r := msFields()
			r.PlatformIDs = []int{1}
			return r
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkTemplateFields(tc.template, tc.req())
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				return
			}
			var ve *apierror.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("err = %v, want ValidationError", err)
			}
		})
	}
}

// An unknown template is a code/schema mismatch, not a client error: it must
// not come back as a 400 the caller could "fix".
func TestCheckTemplateFields_UnknownTemplateIsInternal(t *testing.T) {
	err := checkTemplateFields("PLATFORM", domain.CreateRiskRequest{})
	var ve *apierror.ValidationError
	if err == nil || errors.As(err, &ve) {
		t.Fatalf("err = %v, want a non-validation error", err)
	}
}

func TestAssignmentTeamFits(t *testing.T) {
	cases := []struct {
		register, team string
		want           bool
	}{
		{TemplateManagedServices, TemplateManagedServices, true},
		{TemplateManagedServices, TemplateStandard, false},
		{TemplateManagedServices, TemplateAggregated, false},
		{TemplateStandard, TemplateStandard, true},
		{TemplateStandard, TemplateAggregated, true},
		{TemplateStandard, TemplateManagedServices, false},
		{TemplateAggregated, TemplateStandard, true},
		{TemplateAggregated, TemplateManagedServices, false},
	}
	for _, tc := range cases {
		if got := assignmentTeamFits(tc.register, tc.team); got != tc.want {
			t.Errorf("assignmentTeamFits(%s, %s) = %v, want %v", tc.register, tc.team, got, tc.want)
		}
	}
}
