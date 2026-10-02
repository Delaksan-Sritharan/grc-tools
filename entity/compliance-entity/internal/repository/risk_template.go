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
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/apierror"
	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/domain"
)

// Register templates (RISK_MODULE_DESIGN.md §14), as stored in
// risk_team.register_template.
const (
	TemplateStandard        = "STANDARD"
	TemplateAggregated      = "AGGREGATED"
	TemplateManagedServices = "MANAGED_SERVICES"
)

// checkTemplateFields reports whether req carries exactly the template fields
// its source register's template allows and requires. It only looks at what
// was sent; whether the referenced values exist and are ACTIVE is checked
// against the database separately.
func checkTemplateFields(template string, req domain.CreateRiskRequest) error {
	var bad []string
	reject := func(present bool, field string) {
		if present {
			bad = append(bad, field)
		}
	}
	require := func(present bool, msg string) error {
		if !present {
			return &apierror.ValidationError{Msg: msg}
		}
		return nil
	}

	hasPlatforms := len(req.PlatformIDs) > 0
	hasCustomer := req.CustomerID != nil
	hasDeployment := req.DeploymentTypeID != nil
	hasProducts := len(req.ProductIDs) > 0
	hasEnvironments := len(req.Environments) > 0

	switch template {
	case TemplateStandard:
		reject(hasPlatforms, "platformIds")
		reject(hasCustomer, "customerId")
		reject(hasDeployment, "deploymentTypeId")
		reject(hasProducts, "productIds")
		reject(hasEnvironments, "environments")
	case TemplateAggregated:
		if err := require(hasPlatforms, "at least one platform is required for this register"); err != nil {
			return err
		}
		reject(hasCustomer, "customerId")
		reject(hasDeployment, "deploymentTypeId")
		reject(hasProducts, "productIds")
		reject(hasEnvironments, "environments")
	case TemplateManagedServices:
		for _, check := range []struct {
			present bool
			msg     string
		}{
			{hasCustomer, "customerId is required for this register"},
			{hasDeployment, "deploymentTypeId is required for this register"},
			{hasProducts, "at least one product is required for this register"},
			{hasEnvironments, "at least one environment is required for this register"},
		} {
			if err := require(check.present, check.msg); err != nil {
				return err
			}
		}
		reject(hasPlatforms, "platformIds")
		reject(len(req.ComplianceReferenceIDs) > 0, "complianceReferenceIds")
	default:
		// The column is an ENUM, so this means the code is behind the schema.
		return fmt.Errorf("unknown register template %q", template)
	}
	if len(bad) > 0 {
		return &apierror.ValidationError{
			Msg: fmt.Sprintf("%s not allowed for a register on the %s template", strings.Join(bad, ", "), template)}
	}
	return nil
}

// assignmentTeamFits reports whether an assignment team on teamTemplate may
// be picked on a register on registerTemplate: Managed Services registers
// take only Managed Services teams (the SRE teams), and every other register
// takes every team except those.
func assignmentTeamFits(registerTemplate, teamTemplate string) bool {
	return (registerTemplate == TemplateManagedServices) == (teamTemplate == TemplateManagedServices)
}

// checkAssignmentTeam verifies, inside tx, that the assignment team exists
// and its template fits the source register's.
func checkAssignmentTeam(ctx context.Context, tx *sql.Tx, registerTemplate string, teamID int) error {
	var teamTemplate string
	if err := tx.QueryRowContext(ctx,
		"SELECT register_template FROM risk_team WHERE id = ?", teamID).Scan(&teamTemplate); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &apierror.ValidationError{Msg: fmt.Sprintf("assignment team %d not found", teamID)}
		}
		return fmt.Errorf("risk.Create assignment team template: %w", err)
	}
	if !assignmentTeamFits(registerTemplate, teamTemplate) {
		return &apierror.ValidationError{
			Msg: "this assignment team cannot be used for a risk in this register"}
	}
	return nil
}

// requireActive verifies, inside tx, that every id names an ACTIVE row of
// kind's table. The rows are read FOR SHARE so an admin cannot deactivate or
// delete one between this check and the insert that references it.
func requireActive(ctx context.Context, tx *sql.Tx, kind LookupKind, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	ph := strings.Repeat("?,", len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	var active int
	if err := tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM "+kind.table+" WHERE status = 'ACTIVE' AND id IN ("+ph[:len(ph)-1]+") FOR SHARE", // #nosec G202 -- table is fixed per kind
		args...).Scan(&active); err != nil {
		return fmt.Errorf("risk.Create check %s: %w", kind.table, err)
	}
	// The service has already rejected duplicate ids, so a short count means
	// at least one id is missing or INACTIVE.
	if active != len(ids) {
		return &apierror.ValidationError{Msg: fmt.Sprintf("a selected %s does not exist or is no longer active", kind.noun)}
	}
	return nil
}

// checkTemplateValues verifies every referenced lookup value is ACTIVE.
func checkTemplateValues(ctx context.Context, tx *sql.Tx, req domain.CreateRiskRequest) error {
	single := func(id *int) []int {
		if id == nil {
			return nil
		}
		return []int{*id}
	}
	for _, c := range []struct {
		kind LookupKind
		ids  []int
	}{
		{LookupPlatform, req.PlatformIDs},
		{LookupProduct, req.ProductIDs},
		{LookupCustomer, single(req.CustomerID)},
		{LookupDeploymentType, single(req.DeploymentTypeID)},
	} {
		if err := requireActive(ctx, tx, c.kind, c.ids); err != nil {
			return err
		}
	}
	return nil
}

// reserveCustomerSequence takes the next per-(register, customer) sequence
// number for a Managed Services risk, with the same INSERT IGNORE + SELECT
// FOR UPDATE + UPDATE pattern CreateRisk uses for risk_register_sequence.
func reserveCustomerSequence(ctx context.Context, tx *sql.Tx, registerID, customerID int) (int, error) {
	if _, err := tx.ExecContext(ctx,
		"INSERT IGNORE INTO risk_customer_sequence (risk_team_id, customer_id, last_sequence_number) VALUES (?, ?, 0)",
		registerID, customerID); err != nil {
		return 0, fmt.Errorf("risk.Create ensure customer sequence: %w", err)
	}
	var last int
	if err := tx.QueryRowContext(ctx,
		"SELECT last_sequence_number FROM risk_customer_sequence WHERE risk_team_id = ? AND customer_id = ? FOR UPDATE",
		registerID, customerID).Scan(&last); err != nil {
		return 0, fmt.Errorf("risk.Create lock customer sequence: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE risk_customer_sequence SET last_sequence_number = ? WHERE risk_team_id = ? AND customer_id = ?",
		last+1, registerID, customerID); err != nil {
		return 0, fmt.Errorf("risk.Create bump customer sequence: %w", err)
	}
	return last + 1, nil
}

// writeTemplateData inserts riskID's detail and junction rows. checkTemplateFields
// has already made sure only the current template's fields are set, so this
// writes whatever is present.
func writeTemplateData(ctx context.Context, tx *sql.Tx, riskID int, req domain.CreateRiskRequest) error {
	if req.CustomerID != nil {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO risk_managed_service_detail (risk_id, customer_id, deployment_type_id, created_by, updated_by)
			 VALUES (?, ?, ?, ?, ?)`,
			riskID, *req.CustomerID, *req.DeploymentTypeID, req.CreatedBy, req.CreatedBy); err != nil {
			return fmt.Errorf("risk.Create managed service detail: %w", err)
		}
	}
	for _, id := range req.PlatformIDs {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO risk_platform_reference (risk_id, platform_id) VALUES (?, ?)", riskID, id); err != nil {
			return fmt.Errorf("risk.Create platform %d: %w", id, err)
		}
	}
	for _, id := range req.ProductIDs {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO risk_product_reference (risk_id, product_id) VALUES (?, ?)", riskID, id); err != nil {
			return fmt.Errorf("risk.Create product %d: %w", id, err)
		}
	}
	for _, env := range req.Environments {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO risk_environment_reference (risk_id, environment) VALUES (?, ?)", riskID, env); err != nil {
			return fmt.Errorf("risk.Create environment %s: %w", env, err)
		}
	}
	return nil
}
