-- ============================================================================
--  000037_owner_field — perestanovka → owner («чей вагон»).
--
--  Поле perestanovka не было задействовано (нигде не писалось, всегда '').
--  Переименовываем в owner: имя конторы, которая фактически распоряжается
--  вагоном. Приоритет (решение владельца, сверено с паспортами ЛК РЖД
--  SPV4659): оператор по доверенности (car_trusted) → арендатор (car_tenant)
--  → собственник (car_owner). Владелец наследуется carry-over'ом из прошлого
--  снимка; если там пусто — вычисляется заново из car_*-полей.
--
--  Затрагиваются таблицы с раскладкой LIKE dislocation: dislocation,
--  dislocation_new (staging, может отсутствовать), status6, status9.
--  В vagon_history колонки perestanovka не было — owner добавляется.
--  Бэкфилл: заполняем owner из car_*-полей везде, где он пуст (архивные
--  записи status6/status9 через конвейер больше не проходят).
--  Идемпотентно: rename только если есть perestanovka и ещё нет owner.
-- ============================================================================

SET search_path TO dpport;

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['dislocation','dislocation_new','status6','status9'] LOOP
        IF EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_schema = 'dpport' AND table_name = t AND column_name = 'perestanovka')
           AND NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_schema = 'dpport' AND table_name = t AND column_name = 'owner') THEN
            EXECUTE format('ALTER TABLE dpport.%I RENAME COLUMN perestanovka TO owner', t);
        END IF;
    END LOOP;
END $$;

ALTER TABLE dpport.vagon_history
    ADD COLUMN IF NOT EXISTS owner text NOT NULL DEFAULT '';

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['dislocation','dislocation_new','status6','status9','vagon_history'] LOOP
        IF EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_schema = 'dpport' AND table_name = t AND column_name = 'owner') THEN
            EXECUTE format($f$
                UPDATE dpport.%I
                   SET owner = COALESCE(NULLIF(car_trusted_name, ''),
                                        NULLIF(car_tenant_name, ''),
                                        car_owner_name)
                 WHERE owner = ''$f$, t);
        END IF;
    END LOOP;
END $$;
