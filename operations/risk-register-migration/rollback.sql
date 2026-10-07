-- =============================================================================
-- risk-register-import — ROLLBACK
--
-- Deletes everything the importer wrote, identified by the marker
--   created_by = 'risk-sheet-migration'
-- Run by hand, against the same database the import targeted, by someone with
-- DB access (they are inside the network already). The importer itself never
-- deletes.
--
-- Order matters: children before parents (FKs are RESTRICT on the history
-- tables). `user` rows are intentionally NOT deleted — other data may reference
-- them and re-provisioning them is a no-op.
--
-- Managed Services template rows (risk_managed_service_detail and the product /
-- environment junctions) are removed with their risk: their risk_id FKs are
-- ON DELETE CASCADE. The per-customer sequence counters are NOT: risk_customer_
-- sequence carries no created_by and is never deleted by cascade, so without the
-- reset below the next risk raised for a customer would resume after the numbers
-- the rolled-back import used. The reset sets each affected counter to the
-- highest number still in use (0 when the customer has no risk left).
--
-- Review the counts from the first block before running the DELETEs.
-- =============================================================================

USE grc_platform;

-- ── What would be removed ────────────────────────────────────────────────────
SELECT 'risk'                    AS table_name, COUNT(*) AS rows_to_delete FROM risk                    WHERE created_by = 'risk-sheet-migration'
UNION ALL SELECT 'risk_assessment',        COUNT(*) FROM risk_assessment        WHERE created_by = 'risk-sheet-migration'
UNION ALL SELECT 'risk_escalation',        COUNT(*) FROM risk_escalation        WHERE created_by = 'risk-sheet-migration'
UNION ALL SELECT 'risk_change_log',        COUNT(*) FROM risk_change_log        WHERE created_by = 'risk-sheet-migration'
UNION ALL SELECT 'risk_action_step',       COUNT(*) FROM risk_action_step step
          JOIN risk_action_plan plan ON plan.id = step.plan_id
          WHERE plan.created_by = 'risk-sheet-migration'
UNION ALL SELECT 'risk_action_plan',       COUNT(*) FROM risk_action_plan       WHERE created_by = 'risk-sheet-migration'
UNION ALL SELECT 'user_role_grant',        COUNT(*) FROM user_role_grant        WHERE created_by = 'risk-sheet-migration'
UNION ALL SELECT 'risk_managed_service_detail (cascade)', COUNT(*) FROM risk_managed_service_detail WHERE created_by = 'risk-sheet-migration'
UNION ALL SELECT 'risk_product_reference (cascade)',      COUNT(*) FROM risk_product_reference ref
          JOIN risk r ON r.id = ref.risk_id WHERE r.created_by = 'risk-sheet-migration'
UNION ALL SELECT 'risk_environment_reference (cascade)',  COUNT(*) FROM risk_environment_reference ref
          JOIN risk r ON r.id = ref.risk_id WHERE r.created_by = 'risk-sheet-migration';

-- ── Counters that will need resetting (register, customer, current last number) ──
SELECT s.risk_team_id, s.customer_id, s.last_sequence_number AS current_last_number
FROM risk_customer_sequence s
WHERE (s.risk_team_id, s.customer_id) IN (
  SELECT r.source_register_id, d.customer_id
  FROM risk r JOIN risk_managed_service_detail d ON d.risk_id = r.id
  WHERE r.created_by = 'risk-sheet-migration'
);

-- ── DELETE (uncomment to run) ───────────────────────────────────────────────
-- START TRANSACTION;
--
-- -- Remember which (register, customer) counters the import touched. Must run
-- -- BEFORE the risk rows are deleted: afterwards nothing says which they were.
-- -- (A temporary table does not end the transaction.)
-- CREATE TEMPORARY TABLE rollback_ms_counters AS
--   SELECT DISTINCT r.source_register_id AS team_id, d.customer_id
--   FROM risk r JOIN risk_managed_service_detail d ON d.risk_id = r.id
--   WHERE r.created_by = 'risk-sheet-migration';
--
-- DELETE FROM risk_assessment  WHERE created_by = 'risk-sheet-migration';
-- DELETE FROM risk_escalation  WHERE created_by = 'risk-sheet-migration';
-- DELETE FROM risk_change_log  WHERE created_by = 'risk-sheet-migration';
--
-- DELETE step FROM risk_action_step step
--   JOIN risk_action_plan plan ON plan.id = step.plan_id
--   WHERE plan.created_by = 'risk-sheet-migration';
--
-- -- risk_category_reference / risk_compliance_reference carry no created_by;
-- -- they cascade when their risk row goes.
-- DELETE FROM risk_action_plan WHERE created_by = 'risk-sheet-migration';
-- DELETE FROM risk            WHERE created_by = 'risk-sheet-migration';
--
-- DELETE FROM user_role_grant WHERE created_by = 'risk-sheet-migration';
--
-- -- Reset each touched counter to the highest number still in use. A risk code
-- -- ends in its number (YEAR-TEAM-CUSTOMER-QUARTER-NNNN), so the number is the
-- -- part after the last '-'. A customer left with no risk goes back to 0, so
-- -- the next risk raised for it is 0001 again.
-- UPDATE risk_customer_sequence s
--   JOIN rollback_ms_counters c ON c.team_id = s.risk_team_id AND c.customer_id = s.customer_id
--   LEFT JOIN (
--     SELECT r.source_register_id AS team_id, d.customer_id,
--            MAX(CAST(SUBSTRING_INDEX(r.risk_code, '-', -1) AS UNSIGNED)) AS max_number
--     FROM risk r JOIN risk_managed_service_detail d ON d.risk_id = r.id
--     GROUP BY r.source_register_id, d.customer_id
--   ) m ON m.team_id = s.risk_team_id AND m.customer_id = s.customer_id
--   SET s.last_sequence_number = COALESCE(m.max_number, 0);
--
-- DROP TEMPORARY TABLE rollback_ms_counters;
--
-- COMMIT;

-- ── Verify (expect zero) ───────────────────────────────────────────────────
-- SELECT COUNT(*) FROM risk WHERE created_by = 'risk-sheet-migration';
-- SELECT COUNT(*) FROM risk_managed_service_detail WHERE created_by = 'risk-sheet-migration';
-- -- Every counter must equal the highest number still in use (compare the two
-- -- columns; a customer with no risk left must show 0):
-- SELECT s.risk_team_id, s.customer_id, s.last_sequence_number,
--        MAX(CAST(SUBSTRING_INDEX(r.risk_code, '-', -1) AS UNSIGNED)) AS highest_in_use
-- FROM risk_customer_sequence s
-- LEFT JOIN risk_managed_service_detail d ON d.customer_id = s.customer_id
-- LEFT JOIN risk r ON r.id = d.risk_id AND r.source_register_id = s.risk_team_id
-- GROUP BY s.risk_team_id, s.customer_id, s.last_sequence_number;
