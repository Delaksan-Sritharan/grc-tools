-- =============================================================================
-- risk-register-import — ROLLBACK (Managed Services import)
--
-- Deletes what the Managed Services import wrote. Run by hand, against the same
-- database the import targeted, by someone with DB access (they are inside the
-- network already). The importer itself never deletes.
--
-- SCOPE. The importer's marker (created_by = 'risk-sheet-migration') is NOT
-- unique to this import: the original register migration wrote its risks and
-- grants under the same marker, in the same database. Deleting "everything with
-- the marker" would delete that earlier migration's risks too. So the marker
-- alone selects nothing here. What is deleted is:
--   * risks that carry the marker AND a Managed Services detail row
--     (risk_managed_service_detail) — i.e. this import's risks — and their
--     children, selected by risk id;
--   * RISK_TEAM grants with the marker, only on teams that NO OTHER risk uses
--     (neither as source register nor as assignment team).
-- Marker risks without a detail row (the original migration's) are counted in
-- the preview and never touched.
--
-- GRANTS ARE NOT OWNED BY ONE MIGRATION. The entity creates a grant with
-- ON DUPLICATE KEY, which never changes created_by: when the original migration
-- and this import grant the same person the same role on the same team, there is
-- ONE row, still carrying the marker. Deleting it by team would take access away
-- from the original migration's risks. So a grant on a team that any other risk
-- uses is never deleted here; the preview lists it for you to decide.
--
-- NOT deleted, review by hand:
--   * Marker RISK_TEAM grants on a team another risk also uses (listed, with a
--     flag, in the second preview query).
--   * GLOBAL grants with the marker. The management-approver grant is GLOBAL and
--     nothing says which migration wrote it; the preview lists them.
--   * `user` rows — other data may reference them and re-provisioning is a no-op.
--
-- Managed Services template rows (risk_managed_service_detail and the product /
-- environment junctions) go with their risk: their risk_id FKs are ON DELETE
-- CASCADE.
--
-- SEQUENCE COUNTERS ARE LEFT ALONE. risk_customer_sequence carries no created_by
-- and is not deleted by cascade. By default this script does not touch it: a
-- risk code is a permanent identifier (it goes out in Git issues, emails and the
-- sheet owner's records), the counter never moves backward (RISK_MODULE_DESIGN.md
-- §12), and lowering it would let the next risk reuse the code of a risk this
-- script just deleted. The cost is a gap in the numbering after a re-import.
-- Where no code was ever released (a staging rehearsal), a separate OPTIONAL
-- block below resets each affected counter to the highest number still in use.
--
-- Order matters: children before parents (several FKs are RESTRICT).
-- Review the preview before running the DELETEs.
-- =============================================================================

USE grc_platform;

-- ── What would be removed ────────────────────────────────────────────────────
-- A risk is "this import's" when it has the marker and a Managed Services detail row.
SELECT 'risk'                    AS table_name, COUNT(*) AS rows_to_delete FROM risk r
          WHERE r.created_by = 'risk-sheet-migration'
            AND EXISTS (SELECT 1 FROM risk_managed_service_detail d WHERE d.risk_id = r.id)
UNION ALL SELECT 'risk_assessment',        COUNT(*) FROM risk_assessment x
          WHERE x.risk_id IN (SELECT r.id FROM risk r WHERE r.created_by = 'risk-sheet-migration'
                              AND EXISTS (SELECT 1 FROM risk_managed_service_detail d WHERE d.risk_id = r.id))
UNION ALL SELECT 'risk_escalation',        COUNT(*) FROM risk_escalation x
          WHERE x.risk_id IN (SELECT r.id FROM risk r WHERE r.created_by = 'risk-sheet-migration'
                              AND EXISTS (SELECT 1 FROM risk_managed_service_detail d WHERE d.risk_id = r.id))
UNION ALL SELECT 'risk_change_log',        COUNT(*) FROM risk_change_log x
          WHERE x.risk_id IN (SELECT r.id FROM risk r WHERE r.created_by = 'risk-sheet-migration'
                              AND EXISTS (SELECT 1 FROM risk_managed_service_detail d WHERE d.risk_id = r.id))
UNION ALL SELECT 'risk_action_step',       COUNT(*) FROM risk_action_step step
          JOIN risk_action_plan plan ON plan.id = step.plan_id
          WHERE plan.risk_id IN (SELECT r.id FROM risk r WHERE r.created_by = 'risk-sheet-migration'
                                 AND EXISTS (SELECT 1 FROM risk_managed_service_detail d WHERE d.risk_id = r.id))
UNION ALL SELECT 'risk_action_plan',       COUNT(*) FROM risk_action_plan x
          WHERE x.risk_id IN (SELECT r.id FROM risk r WHERE r.created_by = 'risk-sheet-migration'
                              AND EXISTS (SELECT 1 FROM risk_managed_service_detail d WHERE d.risk_id = r.id))
UNION ALL SELECT 'risk_managed_service_detail (cascade)', COUNT(*) FROM risk_managed_service_detail WHERE created_by = 'risk-sheet-migration'
UNION ALL SELECT 'risk_product_reference (cascade)',      COUNT(*) FROM risk_product_reference ref
          JOIN risk r ON r.id = ref.risk_id WHERE r.created_by = 'risk-sheet-migration'
UNION ALL SELECT 'risk_environment_reference (cascade)',  COUNT(*) FROM risk_environment_reference ref
          JOIN risk r ON r.id = ref.risk_id WHERE r.created_by = 'risk-sheet-migration'
UNION ALL SELECT 'user_role_grant (RISK_TEAM, teams no other risk uses)', COUNT(*) FROM user_role_grant g
          WHERE g.created_by = 'risk-sheet-migration' AND g.scope_type = 'RISK_TEAM'
            AND g.scope_id IN (
              SELECT r.source_register_id FROM risk r WHERE r.created_by = 'risk-sheet-migration'
                AND EXISTS (SELECT 1 FROM risk_managed_service_detail d WHERE d.risk_id = r.id)
              UNION
              SELECT r.assignment_team_id FROM risk r WHERE r.created_by = 'risk-sheet-migration'
                AND EXISTS (SELECT 1 FROM risk_managed_service_detail d WHERE d.risk_id = r.id))
            AND NOT EXISTS (
              SELECT 1 FROM risk o
              WHERE (o.source_register_id = g.scope_id OR o.assignment_team_id = g.scope_id)
                AND NOT (o.created_by = 'risk-sheet-migration'
                         AND EXISTS (SELECT 1 FROM risk_managed_service_detail d2 WHERE d2.risk_id = o.id)));

-- Marker RISK_TEAM grants on the teams this import's risks use, for review.
-- used_by_other_risks = 1: another risk (e.g. one of the original migration's)
-- also uses the team, so the grant may be shared and is NEVER deleted below.
-- used_by_other_risks = 0: nothing else uses the team; the delete below removes it.
SELECT g.user_id, g.role_id, g.scope_id AS team_id, g.status,
       EXISTS (
         SELECT 1 FROM risk o
         WHERE (o.source_register_id = g.scope_id OR o.assignment_team_id = g.scope_id)
           AND NOT (o.created_by = 'risk-sheet-migration'
                    AND EXISTS (SELECT 1 FROM risk_managed_service_detail d2 WHERE d2.risk_id = o.id))
       ) AS used_by_other_risks
FROM user_role_grant g
WHERE g.created_by = 'risk-sheet-migration' AND g.scope_type = 'RISK_TEAM'
  AND g.scope_id IN (
    SELECT r.source_register_id FROM risk r WHERE r.created_by = 'risk-sheet-migration'
      AND EXISTS (SELECT 1 FROM risk_managed_service_detail d WHERE d.risk_id = r.id)
    UNION
    SELECT r.assignment_team_id FROM risk r WHERE r.created_by = 'risk-sheet-migration'
      AND EXISTS (SELECT 1 FROM risk_managed_service_detail d WHERE d.risk_id = r.id))
ORDER BY g.scope_id, g.user_id, g.role_id;

-- Marker risks this script will NOT touch (the original migration's). Expect the
-- count of the earlier migration's risks here; if it is unexpectedly 0 on a
-- database that had the earlier migration, check before running the DELETEs.
SELECT COUNT(*) AS marker_risks_left_alone FROM risk r
WHERE r.created_by = 'risk-sheet-migration'
  AND NOT EXISTS (SELECT 1 FROM risk_managed_service_detail d WHERE d.risk_id = r.id);

-- GLOBAL grants with the marker: NOT deleted (cannot be attributed to one
-- migration). Review by hand; remove only those you know this import created.
SELECT user_id, role_id, scope_type, scope_id FROM user_role_grant
WHERE created_by = 'risk-sheet-migration' AND scope_type = 'GLOBAL';

-- Counters for the customers this import touched (register, customer, current last
-- number). Left alone by default; see the OPTIONAL reset below.
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
-- -- Remember what this import created. Must run BEFORE the risk rows are
-- -- deleted: afterwards nothing says which risks, teams or counters they were.
-- -- (A temporary table does not end the transaction.)
-- CREATE TEMPORARY TABLE rollback_ms_risks AS
--   SELECT r.id AS risk_id, r.source_register_id, r.assignment_team_id, d.customer_id
--   FROM risk r JOIN risk_managed_service_detail d ON d.risk_id = r.id
--   WHERE r.created_by = 'risk-sheet-migration';
--
-- CREATE TEMPORARY TABLE rollback_ms_counters AS
--   SELECT DISTINCT source_register_id AS team_id, customer_id FROM rollback_ms_risks;
--
-- -- (Two statements: MySQL cannot read one temporary table twice in a single
-- -- statement, so a UNION over rollback_ms_risks fails with "Can't reopen table".)
-- CREATE TEMPORARY TABLE rollback_ms_teams AS
--   SELECT source_register_id AS team_id FROM rollback_ms_risks;
-- INSERT INTO rollback_ms_teams (team_id)
--   SELECT assignment_team_id FROM rollback_ms_risks;
--
-- DELETE FROM risk_assessment WHERE risk_id IN (SELECT risk_id FROM rollback_ms_risks);
-- DELETE FROM risk_escalation WHERE risk_id IN (SELECT risk_id FROM rollback_ms_risks);
-- DELETE FROM risk_change_log WHERE risk_id IN (SELECT risk_id FROM rollback_ms_risks);
--
-- DELETE step FROM risk_action_step step
--   JOIN risk_action_plan plan ON plan.id = step.plan_id
--   WHERE plan.risk_id IN (SELECT risk_id FROM rollback_ms_risks);
--
-- -- risk_managed_service_detail, risk_product_reference, risk_environment_reference,
-- -- risk_category_reference, risk_evidence and risk_reminder carry no marker of
-- -- their own (or are written by other actors); they cascade when their risk goes.
-- DELETE FROM risk_action_plan WHERE risk_id IN (SELECT risk_id FROM rollback_ms_risks);
-- DELETE FROM risk WHERE id IN (SELECT risk_id FROM rollback_ms_risks);
--
-- -- RISK_TEAM marker grants, only on teams no OTHER risk uses. A team that
-- -- another risk uses (e.g. the original migration's) may hold a shared grant row
-- -- (see the header), so its grants stay; review them with the second preview
-- -- query and delete by hand if you are sure. Run after the risk delete above:
-- -- the NOT EXISTS ignores this import's own risks either way.
-- DELETE FROM user_role_grant
--   WHERE created_by = 'risk-sheet-migration' AND scope_type = 'RISK_TEAM'
--     AND scope_id IN (SELECT team_id FROM rollback_ms_teams)
--     AND NOT EXISTS (
--       SELECT 1 FROM risk o
--       WHERE (o.source_register_id = user_role_grant.scope_id OR o.assignment_team_id = user_role_grant.scope_id)
--         AND o.id NOT IN (SELECT risk_id FROM rollback_ms_risks));
--
-- COMMIT;

-- ── OPTIONAL: reset the sequence counters (uncomment ONLY if no code was released) ──
-- -- Run in the same session as the block above (it reads rollback_ms_counters).
-- -- Use it where the codes were never used anywhere: a staging rehearsal. On
-- -- production, or wherever risk_codes.csv has been sent to anyone, leave it
-- -- commented: it lets a later risk reuse the code of a risk deleted above.
-- -- A risk code ends in its number (YEAR-TEAM-CUSTOMER-QUARTER-NNNN), so the number
-- -- is the part after the last '-'. A customer left with no risk goes back to 0, so
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

-- ── Cleanup (uncomment with the block above; temporary tables also vanish when the session ends) ──
-- DROP TEMPORARY TABLE rollback_ms_teams;
-- DROP TEMPORARY TABLE rollback_ms_counters;
-- DROP TEMPORARY TABLE rollback_ms_risks;

-- ── Verify (expect zero, then the original migration's risks still present) ─
-- SELECT COUNT(*) AS ms_marker_risks_left FROM risk r
--   WHERE r.created_by = 'risk-sheet-migration'
--     AND EXISTS (SELECT 1 FROM risk_managed_service_detail d WHERE d.risk_id = r.id);
-- SELECT COUNT(*) AS marker_risks_left_alone FROM risk r
--   WHERE r.created_by = 'risk-sheet-migration'
--     AND NOT EXISTS (SELECT 1 FROM risk_managed_service_detail d WHERE d.risk_id = r.id);
-- -- Marker RISK_TEAM grants still present. After the rollback these should be only
-- -- the original migration's, or ones you chose to keep from the review list.
-- SELECT user_id, role_id, scope_id AS team_id FROM user_role_grant
--   WHERE created_by = 'risk-sheet-migration' AND scope_type = 'RISK_TEAM'
--   ORDER BY scope_id, user_id, role_id;
-- -- Counters are unchanged by default (they never move backward). If you ran the
-- -- OPTIONAL reset, each must equal the highest number still in use (compare the
-- -- two columns; a customer with no risk left must show 0):
-- SELECT s.risk_team_id, s.customer_id, s.last_sequence_number,
--        MAX(CAST(SUBSTRING_INDEX(r.risk_code, '-', -1) AS UNSIGNED)) AS highest_in_use
-- FROM risk_customer_sequence s
-- LEFT JOIN risk_managed_service_detail d ON d.customer_id = s.customer_id
-- LEFT JOIN risk r ON r.id = d.risk_id AND r.source_register_id = s.risk_team_id
-- GROUP BY s.risk_team_id, s.customer_id, s.last_sequence_number;
