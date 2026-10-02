USE grc_platform;
ALTER TABLE audit_ai_validation_log
  MODIFY COLUMN evidence_id INT NULL,
  ADD COLUMN population_id INT NULL AFTER evidence_id,
  MODIFY COLUMN result ENUM('PASS','FAIL','UNCERTAIN','PENDING','ERROR','SKIPPED') NOT NULL,
  DROP COLUMN feedback,
  DROP COLUMN confidence_score,
  ADD KEY idx_ai_population (population_id),
  ADD CONSTRAINT fk_ai_population FOREIGN KEY (population_id) REFERENCES audit_population(id) ON DELETE CASCADE,
  ADD CONSTRAINT chk_ai_owner CHECK ((evidence_id IS NOT NULL) <> (population_id IS NOT NULL)); 