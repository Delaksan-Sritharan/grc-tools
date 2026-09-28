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

package service

import (
	"reflect"
	"testing"

	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/domain"
)

func TestBuildLevelCounts(t *testing.T) {
	facts := []domain.OpenRiskFact{
		{RiskLevel: "LOW", ColorCode: "#00B050", Count: 2},
		{RiskLevel: "HIGH", ColorCode: "#FF0000", Count: 1},
		{RiskLevel: "LOW", ColorCode: "#00B050", Count: 3},
		{RiskLevel: "MEDIUM", ColorCode: "#FF9900", Count: 4},
	}

	got := buildLevelCounts(facts, []string{"HIGH", "MEDIUM", "LOW"})
	want := []domain.RiskLevelCount{
		{RiskLevel: "HIGH", ColorCode: "#FF0000", Count: 1},
		{RiskLevel: "MEDIUM", ColorCode: "#FF9900", Count: 4},
		{RiskLevel: "LOW", ColorCode: "#00B050", Count: 5},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildLevelCounts() = %+v, want %+v", got, want)
	}
}

func TestBuildLevelCountsEmpty(t *testing.T) {
	got := buildLevelCounts(nil, []string{"HIGH", "MEDIUM", "LOW"})
	if len(got) != 0 {
		t.Errorf("buildLevelCounts(nil) = %+v, want empty", got)
	}
}

func TestBuildHeatmap(t *testing.T) {
	facts := []domain.OpenRiskFact{
		{Likelihood: 3, Impact: 3, RiskLevel: "HIGH", ColorCode: "#FF0000", Count: 2},
		{Likelihood: 1, Impact: 1, RiskLevel: "LOW", ColorCode: "#00B050", Count: 1},
		{Likelihood: 3, Impact: 3, RiskLevel: "HIGH", ColorCode: "#FF0000", Count: 3},
	}

	got := buildHeatmap(facts)
	want := []domain.HeatmapCell{
		{Likelihood: 3, Impact: 3, RiskLevel: "HIGH", ColorCode: "#FF0000", Count: 5},
		{Likelihood: 1, Impact: 1, RiskLevel: "LOW", ColorCode: "#00B050", Count: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildHeatmap() = %+v, want %+v", got, want)
	}
}

func TestBuildCertDistribution(t *testing.T) {
	counts := []domain.RegisterCertCount{
		{RegisterName: "Choreo", CertName: "SOC2", Count: 3},
		{RegisterName: "Choreo", CertName: "ISO27001", Count: 1},
		{RegisterName: "Business", CertName: "SOC2", Count: 1},
	}

	got := buildCertDistribution(counts)
	want := []domain.RegisterCertShare{
		{RegisterName: "Choreo", CertName: "SOC2", Count: 3, Percentage: 75},
		{RegisterName: "Choreo", CertName: "ISO27001", Count: 1, Percentage: 25},
		{RegisterName: "Business", CertName: "SOC2", Count: 1, Percentage: 100},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildCertDistribution() = %+v, want %+v", got, want)
	}
}

func TestBuildTreatmentByRegister(t *testing.T) {
	facts := []domain.OpenRiskFact{
		{RegisterName: "Choreo", TreatmentStrategy: "REMEDIATE", Count: 2},
		{RegisterName: "Choreo", TreatmentStrategy: "ACCEPT", Count: 1},
		{RegisterName: "Choreo", TreatmentStrategy: "REMEDIATE", Count: 1},
	}

	got := buildTreatmentByRegister(facts)
	want := []domain.RegisterTreatmentCount{
		{RegisterName: "Choreo", TreatmentStrategy: "REMEDIATE", Count: 3},
		{RegisterName: "Choreo", TreatmentStrategy: "ACCEPT", Count: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildTreatmentByRegister() = %+v, want %+v", got, want)
	}
}

// Two distinct registers may share a display name — risk_team.name carries no
// UNIQUE constraint (only code does) — so grouping must key on RegisterID,
// never RegisterName, or one register's count and ID silently swallow the
// other's.
func TestBuildTreatmentByRegister_SameNameDifferentID(t *testing.T) {
	facts := []domain.OpenRiskFact{
		{RegisterID: 1, RegisterName: "Platform", TreatmentStrategy: "REMEDIATE", Count: 2},
		{RegisterID: 2, RegisterName: "Platform", TreatmentStrategy: "REMEDIATE", Count: 5},
	}

	got := buildTreatmentByRegister(facts)
	want := []domain.RegisterTreatmentCount{
		{RegisterID: 1, RegisterName: "Platform", TreatmentStrategy: "REMEDIATE", Count: 2},
		{RegisterID: 2, RegisterName: "Platform", TreatmentStrategy: "REMEDIATE", Count: 5},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildTreatmentByRegister() = %+v, want %+v", got, want)
	}
}

func TestBuildRegisterBlocks(t *testing.T) {
	facts := []domain.OpenRiskFact{
		{RegisterID: 1, RegisterName: "Choreo", Likelihood: 3, Impact: 3, RiskLevel: "HIGH", ColorCode: "#FF0000", TreatmentStrategy: "REMEDIATE", Count: 2},
		{RegisterID: 1, RegisterName: "Choreo", Likelihood: 1, Impact: 1, RiskLevel: "LOW", ColorCode: "#00B050", TreatmentStrategy: "ACCEPT", Count: 1},
		{RegisterID: 2, RegisterName: "Business", Likelihood: 2, Impact: 2, RiskLevel: "MEDIUM", ColorCode: "#FF9900", TreatmentStrategy: "TRANSFER", Count: 4},
	}
	statusFacts := []domain.RegisterStatusFact{
		{RegisterID: 1, RegisterName: "Choreo", RiskLevel: "HIGH", ColorCode: "#FF0000", Bucket: "REMEDIATE", Count: 2},
		{RegisterID: 1, RegisterName: "Choreo", RiskLevel: "LOW", ColorCode: "#00B050", Bucket: "ACCEPT", Count: 1},
		{RegisterID: 1, RegisterName: "Choreo", RiskLevel: "MEDIUM", ColorCode: "#FF9900", Bucket: "CLOSED", Count: 5},
		{RegisterID: 2, RegisterName: "Business", RiskLevel: "MEDIUM", ColorCode: "#FF9900", Bucket: "TRANSFER", Count: 4},
		{RegisterID: 3, RegisterName: "Ballerina", RiskLevel: "LOW", ColorCode: "#00B050", Bucket: "CLOSED", Count: 2},
	}

	got := buildRegisterBlocks(facts, statusFacts, []string{"HIGH", "MEDIUM", "LOW"})
	if len(got) != 3 {
		t.Fatalf("buildRegisterBlocks() returned %d blocks, want 3", len(got))
	}

	choreo := got[0]
	if choreo.RegisterID != 1 || choreo.RegisterName != "Choreo" {
		t.Errorf("block[0] = %+v, want register 1 (Choreo)", choreo)
	}
	if choreo.OpenCount != 3 {
		t.Errorf("choreo.OpenCount = %d, want 3", choreo.OpenCount)
	}
	if len(choreo.Heatmap) != 2 {
		t.Errorf("choreo.Heatmap has %d cells, want 2", len(choreo.Heatmap))
	}
	if len(choreo.StatusLevels) != 3 {
		t.Errorf("choreo.StatusLevels has %d entries, want 3", len(choreo.StatusLevels))
	}

	business := got[1]
	if business.RegisterID != 2 || business.OpenCount != 4 {
		t.Errorf("block[1] = %+v, want register 2 with OpenCount 4", business)
	}

	// Ballerina has only closed risks (no OpenRiskFact rows) but must still
	// get a section, since StatusLevels now covers closed risks too.
	ballerina := got[2]
	if ballerina.RegisterID != 3 || ballerina.OpenCount != 0 {
		t.Errorf("block[2] = %+v, want register 3 (Ballerina) with OpenCount 0", ballerina)
	}
	if len(ballerina.Heatmap) != 0 {
		t.Errorf("ballerina.Heatmap has %d cells, want 0 (no open risks)", len(ballerina.Heatmap))
	}
	if len(ballerina.StatusLevels) != 1 {
		t.Errorf("ballerina.StatusLevels has %d entries, want 1", len(ballerina.StatusLevels))
	}
}

func TestBuildStatusLevels(t *testing.T) {
	facts := []domain.RegisterStatusFact{
		{RiskLevel: "LOW", ColorCode: "#00B050", Bucket: "CLOSED", Count: 2},
		{RiskLevel: "HIGH", ColorCode: "#FF0000", Bucket: "REMEDIATE", Count: 1},
		{RiskLevel: "LOW", ColorCode: "#00B050", Bucket: "CLOSED", Count: 3},
		{RiskLevel: "MEDIUM", ColorCode: "#FF9900", Bucket: "ACCEPT", Count: 4},
	}

	got := buildStatusLevels(facts, []string{"HIGH", "MEDIUM", "LOW"})
	want := []domain.RegisterStatusLevelCount{
		{Bucket: "CLOSED", RiskLevel: "LOW", ColorCode: "#00B050", Count: 5},
		{Bucket: "REMEDIATE", RiskLevel: "HIGH", ColorCode: "#FF0000", Count: 1},
		{Bucket: "ACCEPT", RiskLevel: "MEDIUM", ColorCode: "#FF9900", Count: 4},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildStatusLevels() = %+v, want %+v", got, want)
	}
}

func TestBuildStatusLevelsEmpty(t *testing.T) {
	got := buildStatusLevels(nil, []string{"HIGH", "MEDIUM", "LOW"})
	if len(got) != 0 {
		t.Errorf("buildStatusLevels(nil) = %+v, want empty", got)
	}
}

func TestBuildRepeatedRisks(t *testing.T) {
	rows := []domain.RepeatedRiskRow{
		{RiskTitle: "Weak password policy", RegisterName: "Choreo", Status: "OPEN", RiskLevel: "HIGH", ColorCode: "#FF0000"},
		{RiskTitle: "Weak password policy", RegisterName: "Business", Status: "CLOSED", RiskLevel: "MEDIUM", ColorCode: "#FF9900"},
		{RiskTitle: "Missing MFA", RegisterName: "Choreo", Status: "OPEN", RiskLevel: "HIGH", ColorCode: "#FF0000"},
	}

	got := buildRepeatedRisks(rows)
	want := []domain.RepeatedComplianceRisk{
		{
			RiskTitle: "Weak password policy",
			Occurrences: []domain.RepeatedRiskOccurrence{
				{RegisterName: "Choreo", Status: "OPEN", RiskLevel: "HIGH", ColorCode: "#FF0000"},
				{RegisterName: "Business", Status: "CLOSED", RiskLevel: "MEDIUM", ColorCode: "#FF9900"},
			},
		},
		{
			RiskTitle: "Missing MFA",
			Occurrences: []domain.RepeatedRiskOccurrence{
				{RegisterName: "Choreo", Status: "OPEN", RiskLevel: "HIGH", ColorCode: "#FF0000"},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildRepeatedRisks() = %+v, want %+v", got, want)
	}
}

func TestBuildRepeatedCategories(t *testing.T) {
	const (
		asg, cho, dio   = 1, 2, 3
		access, eol, wf = 10, 20, 30
	)
	facts := []domain.CategoryRegisterFact{
		// Asgardeo · Access: 1 open ACCEPT + 1 open TRANSFER + 1 CLOSED → repeated.
		{RegisterID: asg, RegisterName: "Asgardeo", CategoryID: access, CategoryName: "Access", Bucket: domain.CategoryBucketOpenAccept, Count: 1},
		{RegisterID: asg, RegisterName: "Asgardeo", CategoryID: access, CategoryName: "Access", Bucket: domain.CategoryBucketOpenOther, Count: 1},
		{RegisterID: asg, RegisterName: "Asgardeo", CategoryID: access, CategoryName: "Access", Bucket: domain.CategoryBucketClosed, Count: 1},
		// Asgardeo · WAF: 3 open REMEDIATE → repeated, sorts above Access (more open).
		{RegisterID: asg, RegisterName: "Asgardeo", CategoryID: wf, CategoryName: "WAF", Bucket: domain.CategoryBucketOpenRemediate, Count: 3},
		// Choreo · EOL: a single risk → not repeated.
		{RegisterID: cho, RegisterName: "Choreo", CategoryID: eol, CategoryName: "EOL", Bucket: domain.CategoryBucketOpenAccept, Count: 1},
		// Digi Ops · EOL: all CLOSED → still repeated.
		{RegisterID: dio, RegisterName: "Digi Ops", CategoryID: eol, CategoryName: "EOL", Bucket: domain.CategoryBucketClosed, Count: 5},
	}

	got := buildRepeatedCategories(facts)
	want := []domain.RepeatedCategory{
		{RegisterID: asg, RegisterName: "Asgardeo", CategoryID: wf, CategoryName: "WAF",
			CategoryCounts: domain.CategoryCounts{Open: 3, Remediate: 3}},
		{RegisterID: asg, RegisterName: "Asgardeo", CategoryID: access, CategoryName: "Access",
			CategoryCounts: domain.CategoryCounts{Open: 2, Accept: 1, Closed: 1}},
		{RegisterID: dio, RegisterName: "Digi Ops", CategoryID: eol, CategoryName: "EOL",
			CategoryCounts: domain.CategoryCounts{Closed: 5}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildRepeatedCategories() = %+v, want %+v", got, want)
	}
}

func TestBuildRepeatedCategoriesEmpty(t *testing.T) {
	got := buildRepeatedCategories(nil)
	if got == nil || len(got) != 0 {
		t.Errorf("buildRepeatedCategories(nil) = %#v, want non-nil empty", got)
	}
}

func TestBuildCommonOpenCategories(t *testing.T) {
	const (
		asg, bal, cho, dio = 1, 2, 3, 4
		access, eol, wf    = 10, 20, 30
	)
	facts := []domain.CategoryRegisterFact{
		// Access: open in Asgardeo and Choreo; Ballerina has it CLOSED only, so
		// Ballerina is not affected and its closed risks are not counted.
		{RegisterID: asg, RegisterName: "Asgardeo", CategoryID: access, CategoryName: "Access", Bucket: domain.CategoryBucketOpenAccept, Count: 2},
		{RegisterID: asg, RegisterName: "Asgardeo", CategoryID: access, CategoryName: "Access", Bucket: domain.CategoryBucketClosed, Count: 1},
		{RegisterID: bal, RegisterName: "Ballerina", CategoryID: access, CategoryName: "Access", Bucket: domain.CategoryBucketClosed, Count: 3},
		{RegisterID: cho, RegisterName: "Choreo", CategoryID: access, CategoryName: "Access", Bucket: domain.CategoryBucketOpenOther, Count: 1},
		// WAF: open in three registers → sorts first despite fewer open risks.
		{RegisterID: asg, RegisterName: "Asgardeo", CategoryID: wf, CategoryName: "WAF", Bucket: domain.CategoryBucketOpenRemediate, Count: 1},
		{RegisterID: bal, RegisterName: "Ballerina", CategoryID: wf, CategoryName: "WAF", Bucket: domain.CategoryBucketOpenRemediate, Count: 1},
		{RegisterID: dio, RegisterName: "Digi Ops", CategoryID: wf, CategoryName: "WAF", Bucket: domain.CategoryBucketOpenAccept, Count: 1},
		// EOL: open in one register only → not common, however many risks.
		{RegisterID: dio, RegisterName: "Digi Ops", CategoryID: eol, CategoryName: "EOL", Bucket: domain.CategoryBucketOpenAccept, Count: 5},
	}

	got := buildCommonOpenCategories(facts)
	want := []domain.CommonOpenCategory{
		{CategoryID: wf, CategoryName: "WAF", RegisterIDs: []int{asg, bal, dio},
			CategoryCounts: domain.CategoryCounts{Open: 3, Accept: 1, Remediate: 2}},
		{CategoryID: access, CategoryName: "Access", RegisterIDs: []int{asg, cho},
			CategoryCounts: domain.CategoryCounts{Open: 3, Accept: 2, Closed: 1}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildCommonOpenCategories() = %+v, want %+v", got, want)
	}
}

func TestBuildCommonOpenCategoriesEmpty(t *testing.T) {
	got := buildCommonOpenCategories(nil)
	if got == nil || len(got) != 0 {
		t.Errorf("buildCommonOpenCategories(nil) = %#v, want non-nil empty", got)
	}
}
