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

package model

// The due-date reminder tiers, matching risk_reminder's ENUM and the three
// emailer events. Lives in model, not in internal/risk/job, because the job
// decides WHICH tier fires today while internal/risk/handler decides who it
// goes to and which email it is — neither package may import the other.
const (
	ReminderDueIn15Days = "DUE_IN_15_DAYS"
	ReminderDueIn5Days  = "DUE_IN_5_DAYS"
	ReminderDueToday    = "DUE_TODAY"
)

// ReminderLeadDays is how many days before the implementation date each tier
// fires. A tier fires on that exact day and is never sent late: a reminder is
// a warning with time to act on it, and a late one is just noise arriving
// after the escalation it was meant to prevent.
var ReminderLeadDays = map[string]int{
	ReminderDueIn15Days: 15,
	ReminderDueIn5Days:  5,
	ReminderDueToday:    0,
}

// reminderTiersByLevel is the schedule: how much warning a risk earns depends
// on how bad it is. Keyed by the risk's EFFECTIVE (residual) level — the one
// the register table shows and the one escalation reads — so a risk reassessed
// down stops collecting the reminders its original level would have earned.
var reminderTiersByLevel = map[string][]string{
	"HIGH":   {ReminderDueIn15Days, ReminderDueIn5Days, ReminderDueToday},
	"MEDIUM": {ReminderDueIn5Days, ReminderDueToday},
	"LOW":    {ReminderDueToday},
}

// ReminderTierFor returns the tier due today for a risk at the given level
// whose implementation date is daysUntil days away, or "" when none is.
//
// daysUntil is a whole-day count: 0 is the due date itself, negative is
// overdue. Overdue never yields a tier — that case belongs to the escalation
// job, which flips the risk to ESCALATED and emails a wider list. An
// unrecognised level also yields "", so a risk with no score is skipped rather
// than silently treated as LOW.
func ReminderTierFor(level string, daysUntil int) string {
	for _, tier := range reminderTiersByLevel[level] {
		if ReminderLeadDays[tier] == daysUntil {
			return tier
		}
	}
	return ""
}

// MaxReminderLeadDays is the furthest out any tier fires, which is how far
// ahead the daily sweep needs to look for candidate risks.
const MaxReminderLeadDays = 15
