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

package aivalidation

import (
	"context"
	"sync"
	"testing"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/audit/model"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/llm"
)

type fakeControlSource struct{}

func (fakeControlSource) GetByID(context.Context, int, int) (*model.AuditControl, error) {
	return &model.AuditControl{}, nil
}

type fakeLogRepo struct {
	mu      sync.Mutex
	results []string
}

func (r *fakeLogRepo) ListByEvidence(context.Context, int) ([]*model.AIValidationLog, error) {
	return nil, nil
}

func (r *fakeLogRepo) ListByPopulation(context.Context, int) ([]*model.AIValidationLog, error) {
	return nil, nil
}

func (r *fakeLogRepo) CreateForEvidence(_ context.Context, _ int, req model.CreateAIValidationLogRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.results = append(r.results, req.Result)
	return nil
}

func (r *fakeLogRepo) CreateForPopulation(ctx context.Context, id int, req model.CreateAIValidationLogRequest) error {
	return r.CreateForEvidence(ctx, id, req)
}

// A panic while building a job's input (e.g. parsing a malformed upload)
// must not escape the goroutine: the job ends with an ERROR row, and the
// worker slot and dedupe claim are released for the next job.
func TestRun_RecoversPanicAndWritesError(t *testing.T) {
	repo := &fakeLogRepo{}
	s := NewService(nil, repo, fakeControlSource{}, nil, nil, nil, true)
	ref := evidenceRef(1, 1)
	panicking := func(context.Context, *model.AuditControl) ([]llm.Block, string, string, error) {
		panic("malformed upload")
	}

	s.run(1, ref, "tester", false, submissionEvidence, panicking)

	if got := repo.results; len(got) != 2 || got[0] != "PENDING" || got[1] != "ERROR" {
		t.Fatalf("got rows %v, want [PENDING ERROR]", got)
	}
	if len(s.sem) != 0 {
		t.Errorf("worker slot not released: %d held", len(s.sem))
	}
	if !s.dedup.claim(ref) {
		t.Errorf("dedupe claim not released after a panic")
	}
}
