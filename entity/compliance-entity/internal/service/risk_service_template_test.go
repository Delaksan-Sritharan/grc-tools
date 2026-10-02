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
	"errors"
	"testing"

	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/apierror"
	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/domain"
)

func TestNormalizeTemplateFields(t *testing.T) {
	ok := domain.CreateRiskRequest{
		PlatformIDs:  []int{1, 2},
		ProductIDs:   []int{3},
		Environments: []string{"production", "Dr"},
	}
	if err := normalizeTemplateInput(ok.PlatformIDs, ok.ProductIDs, ok.CustomerID, ok.DeploymentTypeID, ok.Environments); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ok.Environments[0] != "PRODUCTION" || ok.Environments[1] != "DR" {
		t.Errorf("environments not upper-cased: %v", ok.Environments)
	}

	zero := 0
	for name, req := range map[string]domain.CreateRiskRequest{
		"duplicate platform":    {PlatformIDs: []int{1, 1}},
		"zero product":          {ProductIDs: []int{0}},
		"zero customer":         {CustomerID: &zero},
		"zero deployment type":  {DeploymentTypeID: &zero},
		"unknown environment":   {Environments: []string{"STAGING"}},
		"duplicate environment": {Environments: []string{"DR", "dr"}},
	} {
		t.Run(name, func(t *testing.T) {
			var ve *apierror.ValidationError
			if err := normalizeTemplateInput(req.PlatformIDs, req.ProductIDs, req.CustomerID, req.DeploymentTypeID, req.Environments); !errors.As(err, &ve) {
				t.Errorf("err = %v, want ValidationError", err)
			}
		})
	}
}
