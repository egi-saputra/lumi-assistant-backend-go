-- +goose Up
-- +goose NO TRANSACTION
-- +goose StatementBegin
CREATE PROCEDURE migrate_tenant_codes_to_six_digits()
BEGIN
    DECLARE finished BOOLEAN DEFAULT FALSE;
    DECLARE tenant_id CHAR(26);
    DECLARE candidate_code CHAR(6);
    DECLARE code_exists INT DEFAULT 0;
    DECLARE tenant_cursor CURSOR FOR
        SELECT id FROM tenants
        WHERE code NOT REGEXP '^[0-9]{6}$'
        ORDER BY created_at, id;
    DECLARE CONTINUE HANDLER FOR NOT FOUND SET finished = TRUE;

    OPEN tenant_cursor;
    tenant_loop: LOOP
        FETCH tenant_cursor INTO tenant_id;
        IF finished THEN
            LEAVE tenant_loop;
        END IF;

        REPEAT
            SET candidate_code = LPAD(FLOOR(100000 + RAND() * 900000), 6, '0');
            SELECT COUNT(*) INTO code_exists
            FROM tenants
            WHERE code = candidate_code;
        UNTIL code_exists = 0
        END REPEAT;

        UPDATE tenants SET code = candidate_code WHERE id = tenant_id;
    END LOOP;
    CLOSE tenant_cursor;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CALL migrate_tenant_codes_to_six_digits();
-- +goose StatementEnd

DROP PROCEDURE migrate_tenant_codes_to_six_digits;
