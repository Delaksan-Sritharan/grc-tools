-- Updated 24/10/2026 (v1.0.6)
--
-- Adds risk.risk_acceptor: who accepted the risk, CUSTOMER or WSO2. Run after
-- v1.0.5, at any time: before, with or after the new compliance-entity and
-- backend, and without the webapp being live.
--
-- Additive only: one nullable column, no default, no backfill. Every existing
-- risk reads NULL ("not set") until it is edited. No row, index or foreign key
-- is touched. If MySQL cannot add it online (ALGORITHM=INPLACE, LOCK=NONE) it
-- errors instead of locking the table, and nothing is changed.
--
-- Already in risk_schema.sql; this is the standalone copy for an existing
-- database. Re-running is a safe no-op: the add is guarded on
-- information_schema.
--
-- Rollback (discards every acceptor recorded so far):
--   ALTER TABLE risk DROP COLUMN risk_acceptor;

USE grc_platform;

SET @risk_has_acceptor = (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'risk'
    AND COLUMN_NAME = 'risk_acceptor'
);
SET @add_risk_acceptor_sql = IF(@risk_has_acceptor = 0,
  'ALTER TABLE risk '
  'ADD COLUMN risk_acceptor ENUM(''CUSTOMER'',''WSO2'') NULL '
  'COMMENT ''Who accepted the risk. NULL until recorded: risks that predate the column carry NULL'' '
  'AFTER treatment_strategy, ALGORITHM=INPLACE, LOCK=NONE',
  'SELECT 1');
PREPARE add_risk_acceptor_stmt FROM @add_risk_acceptor_sql;
EXECUTE add_risk_acceptor_stmt;
DEALLOCATE PREPARE add_risk_acceptor_stmt;

-- Expect Type enum('CUSTOMER','WSO2'), Null = YES, Default = NULL.
SHOW COLUMNS FROM risk LIKE 'risk_acceptor';

-- Expect acceptor_set = 0 right after this runs.
SELECT COUNT(*) AS total_risks, COUNT(risk_acceptor) AS acceptor_set FROM risk;
