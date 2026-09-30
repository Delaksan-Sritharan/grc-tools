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

package entity

import (
	"context"
	"fmt"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/risk/repository"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/entityclient"
)

type reminderRepository struct{ c *entityclient.Client }

// NewReminderRepository creates a Compliance Entity-backed
// repository.ReminderRepository — the due-date reminder sweep's claim/release
// gate. Counterpart of the audit module's entity.NewNotificationRepository.
func NewReminderRepository(c *entityclient.Client) repository.ReminderRepository {
	return &reminderRepository{c: c}
}

// Claim asks the entity to insert the claim row. A 200 with claimed=false is
// the normal "another replica got there first" answer, not an error — only a
// transport or server failure comes back as one, and the sweep fails closed on
// those (it can't tell whether it holds the claim, so it must not send).
func (r *reminderRepository) Claim(ctx context.Context, riskID int, reminderType, dueDateSnapshot string) (bool, int64, error) {
	var resp struct {
		Claimed bool  `json:"claimed"`
		ID      int64 `json:"id"`
	}
	body := map[string]any{
		"riskId":          riskID,
		"reminderType":    reminderType,
		"dueDateSnapshot": dueDateSnapshot,
	}
	if err := r.c.Post(ctx, "/risk/reminders/claim", body, &resp); err != nil {
		return false, 0, err
	}
	return resp.Claimed, resp.ID, nil
}

// ReleaseClaim asks the entity to delete a claim row whose email failed, so a
// later run the same day retries that reminder.
func (r *reminderRepository) ReleaseClaim(ctx context.Context, reminderID int64) error {
	return r.c.Delete(ctx, fmt.Sprintf("/risk/reminders/%d/claim", reminderID))
}
