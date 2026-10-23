-- Updated 23/10/2026 (v1.0.5)
--
-- Makes risk.management_approver_id nullable: the Management Approver is
-- required only for a HIGH risk now. Run after v1.0.4, before deploying the new
-- compliance-entity, backend and webapp.
--
-- Safe to run first: it only relaxes NOT NULL, so no row or the foreign key
-- changes, and the current code still sets an approver on every risk. If MySQL
-- cannot do it online (ALGORITHM=INPLACE, LOCK=NONE) it errors instead of
-- locking the table, and nothing is changed.
--
-- Rollback: redeploy the previous code, give every risk listed by the first
-- query below an approver, and only when it returns 0 rows run the last
-- statement. The previous code breaks on a NULL approver.
--   SELECT id, risk_code FROM risk WHERE management_approver_id IS NULL;
--   ALTER TABLE risk MODIFY COLUMN management_approver_id INT NOT NULL;

USE grc_platform;

ALTER TABLE risk
  MODIFY COLUMN management_approver_id INT NULL
    COMMENT 'Required when the risk is HIGH (whatever the treatment), NULL otherwise. Named by the Risk Assigner; approves PENDING_MANAGEMENT_APPROVAL and PENDING_MANAGEMENT_CLOSURE_APPROVAL, which every HIGH risk passes through',
  ALGORITHM=INPLACE, LOCK=NONE;

-- Expect Null = YES.
SHOW COLUMNS FROM risk LIKE 'management_approver_id';
