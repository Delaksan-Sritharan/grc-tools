-- Updated 02/10/2026 (v1.0.2)
--
-- Register Templates (RISK_MODULE_DESIGN.md §14): lets a register carry the
-- WSO2 Cloud ("Aggregated") or Managed Services field set instead of the
-- original one. Run after v1.0.1.
--
-- Additive only:
--   • one new column on risk_team, register_template, defaulting to
--     STANDARD — every existing register and team reads as STANDARD the
--     moment it lands, with no UPDATE. risk_team is a small table.
--   • eight new tables. Nothing is created in them; admins add the lookup
--     values through the Admin Console.
--   • four new values on admin_activity_log.entity_type (a shared.sql
--     table), so changes to those values are recorded in the Admin Console's
--     activity log. Appended at the end of the ENUM, which MySQL 8 applies as
--     a metadata-only change; no existing row is touched.
-- No existing row is rewritten and the risk table is not altered: existing
-- risks are STANDARD, which has no rows in any of the new tables.
--
-- Every statement below is already in risk_schema.sql (the full, current
-- schema); this file is the standalone copy for a database that already has
-- every other Risk module table. Re-running it is a safe no-op: the column
-- add is guarded on information_schema (MySQL has no ADD COLUMN IF NOT
-- EXISTS) and the tables use CREATE TABLE IF NOT EXISTS.
--
-- Rollback, safe only before any register uses a non-STANDARD template (once
-- one does, it discards those risks' customer/product/platform/environment
-- values, and MS risk codes would no longer match any customer):
--   DROP TABLE risk_customer_sequence, risk_environment_reference,
--              risk_product_reference, risk_platform_reference,
--              risk_managed_service_detail, risk_deployment_type,
--              risk_product, risk_customer, risk_platform;
--   ALTER TABLE risk_team DROP COLUMN register_template;
--   (and, only once no log row uses the four new values:)
--   ALTER TABLE admin_activity_log MODIFY entity_type
--     ENUM('USER','GRANT','RISK_TEAM','RISK_CATEGORY','COMPLIANCE_REFERENCE',
--          'RISK_SCORE','AUDIT_TEAM') NOT NULL;

USE grc_platform;

-- -----------------------------------------------------------------------------
-- risk_team.register_template
-- -----------------------------------------------------------------------------
SET @risk_team_has_template = (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'risk_team'
    AND COLUMN_NAME = 'register_template'
);
SET @add_risk_team_template_sql = IF(@risk_team_has_template = 0,
  'ALTER TABLE risk_team '
  'ADD COLUMN register_template ENUM(''STANDARD'',''AGGREGATED'',''MANAGED_SERVICES'') '
  'NOT NULL DEFAULT ''STANDARD'' AFTER team_type',
  'SELECT 1');
PREPARE add_risk_team_template_stmt FROM @add_risk_team_template_sql;
EXECUTE add_risk_team_template_stmt;
DEALLOCATE PREPARE add_risk_team_template_stmt;

-- -----------------------------------------------------------------------------
-- admin_activity_log.entity_type: four new values for the lookups below.
-- Guarded the same way: MODIFY only when the new values are missing.
-- -----------------------------------------------------------------------------
SET @activity_log_has_lookups = (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'admin_activity_log'
    AND COLUMN_NAME = 'entity_type' AND COLUMN_TYPE LIKE '%''RISK_DEPLOYMENT_TYPE''%'
);
SET @extend_activity_log_sql = IF(@activity_log_has_lookups = 0,
  'ALTER TABLE admin_activity_log MODIFY entity_type '
  'ENUM(''USER'',''GRANT'',''RISK_TEAM'',''RISK_CATEGORY'',''COMPLIANCE_REFERENCE'',''RISK_SCORE'',''AUDIT_TEAM'','
  '''RISK_PLATFORM'',''RISK_CUSTOMER'',''RISK_PRODUCT'',''RISK_DEPLOYMENT_TYPE'') NOT NULL',
  'SELECT 1');
PREPARE extend_activity_log_stmt FROM @extend_activity_log_sql;
EXECUTE extend_activity_log_stmt;
DEALLOCATE PREPARE extend_activity_log_stmt;

-- -----------------------------------------------------------------------------
-- Lookups — deactivated, never deleted once used; every FK to them RESTRICT.
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS risk_platform (
  id          INT          NOT NULL AUTO_INCREMENT,
  name        VARCHAR(255) NOT NULL,
  status      ENUM('ACTIVE','INACTIVE') NOT NULL DEFAULT 'ACTIVE',
  created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  created_by  VARCHAR(255) NULL,
  updated_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  updated_by  VARCHAR(255) NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_risk_platform_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- code is embedded in risk codes and frozen once used (application rule).
-- REGEXP_LIKE 'c' = case-sensitive; the column collation is not.
CREATE TABLE IF NOT EXISTS risk_customer (
  id          INT          NOT NULL AUTO_INCREMENT,
  name        VARCHAR(255) NOT NULL,
  code        VARCHAR(12)  NOT NULL COMMENT 'A-Z/0-9, embedded in risk codes; frozen once used',
  status      ENUM('ACTIVE','INACTIVE') NOT NULL DEFAULT 'ACTIVE',
  created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  created_by  VARCHAR(255) NULL,
  updated_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  updated_by  VARCHAR(255) NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_risk_customer_name (name),
  UNIQUE KEY uq_risk_customer_code (code),
  CONSTRAINT chk_risk_customer_code CHECK (REGEXP_LIKE(code, '^[A-Z0-9]{1,12}$', 'c'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS risk_product (
  id          INT          NOT NULL AUTO_INCREMENT,
  name        VARCHAR(255) NOT NULL,
  status      ENUM('ACTIVE','INACTIVE') NOT NULL DEFAULT 'ACTIVE',
  created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  created_by  VARCHAR(255) NULL,
  updated_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  updated_by  VARCHAR(255) NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_risk_product_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS risk_deployment_type (
  id          INT          NOT NULL AUTO_INCREMENT,
  name        VARCHAR(255) NOT NULL,
  status      ENUM('ACTIVE','INACTIVE') NOT NULL DEFAULT 'ACTIVE',
  created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  created_by  VARCHAR(255) NULL,
  updated_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  updated_by  VARCHAR(255) NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_risk_deployment_type_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- -----------------------------------------------------------------------------
-- Per-risk template data — FK to risk CASCADE, to lookups RESTRICT.
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS risk_managed_service_detail (
  risk_id            INT          NOT NULL,
  customer_id        INT          NOT NULL,
  deployment_type_id INT          NOT NULL,
  created_at         DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  created_by         VARCHAR(255) NULL,
  updated_at         DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  updated_by         VARCHAR(255) NULL,
  PRIMARY KEY (risk_id),
  KEY idx_rmsd_customer        (customer_id),
  KEY idx_rmsd_deployment_type (deployment_type_id),
  CONSTRAINT fk_rmsd_risk            FOREIGN KEY (risk_id)            REFERENCES risk(id)                 ON DELETE CASCADE,
  CONSTRAINT fk_rmsd_customer        FOREIGN KEY (customer_id)        REFERENCES risk_customer(id)        ON DELETE RESTRICT,
  CONSTRAINT fk_rmsd_deployment_type FOREIGN KEY (deployment_type_id) REFERENCES risk_deployment_type(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS risk_platform_reference (
  risk_id     INT      NOT NULL,
  platform_id INT      NOT NULL,
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (risk_id, platform_id),
  KEY idx_rplat_platform (platform_id),
  CONSTRAINT fk_rplat_risk     FOREIGN KEY (risk_id)     REFERENCES risk(id)          ON DELETE CASCADE,
  CONSTRAINT fk_rplat_platform FOREIGN KEY (platform_id) REFERENCES risk_platform(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS risk_product_reference (
  risk_id     INT      NOT NULL,
  product_id  INT      NOT NULL,
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (risk_id, product_id),
  KEY idx_rprod_product (product_id),
  CONSTRAINT fk_rprod_risk    FOREIGN KEY (risk_id)    REFERENCES risk(id)         ON DELETE CASCADE,
  CONSTRAINT fk_rprod_product FOREIGN KEY (product_id) REFERENCES risk_product(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS risk_environment_reference (
  risk_id     INT      NOT NULL,
  environment ENUM('PRODUCTION','NON_PRODUCTION','DR') NOT NULL,
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (risk_id, environment),
  KEY idx_renv_environment (environment),
  CONSTRAINT fk_renv_risk FOREIGN KEY (risk_id) REFERENCES risk(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Per-(register, customer) risk-code counter for MANAGED_SERVICES registers.
CREATE TABLE IF NOT EXISTS risk_customer_sequence (
  risk_team_id         INT NOT NULL COMMENT 'FK to risk_team (source register)',
  customer_id          INT NOT NULL,
  last_sequence_number INT NOT NULL DEFAULT 0,
  PRIMARY KEY (risk_team_id, customer_id),
  KEY idx_rcs_customer (customer_id),
  CONSTRAINT fk_rcs_team     FOREIGN KEY (risk_team_id) REFERENCES risk_team(id)     ON DELETE RESTRICT,
  CONSTRAINT fk_rcs_customer FOREIGN KEY (customer_id)  REFERENCES risk_customer(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
