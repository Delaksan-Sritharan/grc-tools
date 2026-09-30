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

import "testing"

// The schedule, stated as a table: how much warning each level earns.
func TestReminderTierFor(t *testing.T) {
	cases := []struct {
		level     string
		daysUntil int
		want      string
	}{
		{"HIGH", 15, ReminderDueIn15Days},
		{"HIGH", 5, ReminderDueIn5Days},
		{"HIGH", 0, ReminderDueToday},
		{"MEDIUM", 15, ""},
		{"MEDIUM", 5, ReminderDueIn5Days},
		{"MEDIUM", 0, ReminderDueToday},
		{"LOW", 15, ""},
		{"LOW", 5, ""},
		{"LOW", 0, ReminderDueToday},

		// Nothing fires between the tiers.
		{"HIGH", 16, ""},
		{"HIGH", 14, ""},
		{"HIGH", 6, ""},
		{"HIGH", 4, ""},
		{"HIGH", 1, ""},

		// Overdue is the escalation job's business, never a reminder's.
		{"HIGH", -1, ""},
		{"MEDIUM", -5, ""},
		{"LOW", -100, ""},

		// An unrateable risk gets nothing rather than being assumed LOW.
		{"", 0, ""},
		{"UNKNOWN", 0, ""},
		{"high", 0, ""}, // levels are stored uppercase; no case-folding here
	}

	for _, c := range cases {
		if got := ReminderTierFor(c.level, c.daysUntil); got != c.want {
			t.Errorf("ReminderTierFor(%q, %d) = %q, want %q", c.level, c.daysUntil, got, c.want)
		}
	}
}

// MaxReminderLeadDays decides how far ahead the daily sweep looks for
// candidate risks, so a tier further out than it would never be queried and
// would silently never fire.
func TestMaxReminderLeadDaysCoversEveryTier(t *testing.T) {
	for tier, lead := range ReminderLeadDays {
		if lead > MaxReminderLeadDays {
			t.Errorf("tier %s fires %d days out, beyond MaxReminderLeadDays (%d) — the sweep would never see it",
				tier, lead, MaxReminderLeadDays)
		}
	}
}

// Every tier a level earns must have a lead time, or ReminderTierFor would
// match it against the zero value and fire it on the due date.
func TestEveryScheduledTierHasALeadTime(t *testing.T) {
	for level, tiers := range reminderTiersByLevel {
		for _, tier := range tiers {
			if _, ok := ReminderLeadDays[tier]; !ok {
				t.Errorf("level %s schedules tier %q, which has no lead time", level, tier)
			}
		}
	}
}
