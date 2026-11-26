ALTER TABLE categorization_rules
    DROP CONSTRAINT IF EXISTS categorization_rules_match_type_check;

ALTER TABLE categorization_rules
    ADD CONSTRAINT categorization_rules_match_type_check
    CHECK (match_type IN ('CONTAINS', 'STARTS_WITH', 'ENDS_WITH', 'EXACT', 'REGEX'));
