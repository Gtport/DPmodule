-- ============================================================================
--  000027_cargo_directory — словарь грузов ЕТСНГ (обогащение Stage 1).
--
--  Универсальная идентичность груза: код → группа / краткое имя / метка.
--  Полный перечень ЕТСНГ (~5 тыс. строк) с наложенной ручной переработкой
--  (бизнес-группы УГОЛЬ/МЕТАЛЛ/ЧУГУН, краткие имена, метки cargo_sms).
--  Заполняется по CodeCargo КАЖДОМУ вагону сразу после фильтра по порту —
--  в отличие от marka, не зависит от отправителя (снимает «неопределённость»
--  при новом грузе у известного отправителя).
--
--  Данные: scripts/gen_cargo_seed.py → _reference/seed/cargo.csv (вне git,
--  ручные правки cargo_sms живут в CSV) → scripts/seed_directories.sql.
-- ============================================================================

CREATE TABLE dpport.cargo (
    cargo_kod    bigint PRIMARY KEY,           -- код груза ЕТСНГ (ключ поиска)
    name         text NOT NULL DEFAULT '',     -- полное имя груза
    cargo_group  text NOT NULL DEFAULT '',     -- группа → Dislocation.CargoGroup
    cargo_s      text NOT NULL DEFAULT '',     -- краткое имя → Dislocation.CargoS
    cargo_sms    text NOT NULL DEFAULT ''      -- метка груза → Dislocation.CargoSms
);
