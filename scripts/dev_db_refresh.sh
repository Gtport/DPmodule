#!/usr/bin/env bash
# Обновление ЛОКАЛЬНОЙ (WSL) копии боевой базы dpport.
#
# Запускается НА ЛОКАЛЬНОЙ машине. Дамп снимается на VPS (pg_dump там же, где
# база — так быстрее и не нужен туннель для самой выгрузки), едет по ssh и
# заливается в локальный Postgres ПОВЕРХ существующей копии.
#
#   scripts/dev_db_refresh.sh                # снимок без тела vagon_operation (~15 МБ)
#   scripts/dev_db_refresh.sh --full         # всё, включая историю продвижения (~35 МБ)
#   scripts/dev_db_refresh.sh --schema-only  # только схема, без данных
#   scripts/dev_db_refresh.sh --file d.dump  # залить уже скачанный дамп, без ssh
#
# ⚠️ Боевую базу скрипт только ЧИТАЕТ. Удаляется и пересоздаётся ЛОКАЛЬНАЯ.
set -euo pipefail

# ---- настройки (переопределяются переменными окружения) ----
VPS="${DPM_VPS:-}"                                # user@host боевого сервера
VPS_PATH="${DPM_VPS_PATH:-~/projects/DPmodule}"   # путь к клону на сервере (там лежит .env)
DB="${DPM_DB:-dpport}"                            # имя локальной базы
DB_USER="${DPM_DB_USER:-gtport_app}"              # роль приложения (как в config)
DB_SCHEMA="${DPM_DB_SCHEMA:-dpport}"              # схема (search_path базы)
SUPER="${DPM_PG_SUPER:-sudo -u postgres psql}"    # как ходить в локальный Postgres админом

MODE=snapshot
DUMP_FILE=""
KEEP=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --full)        MODE=full ;;
    --schema-only) MODE=schema ;;
    --file)        DUMP_FILE="${2:?--file требует путь}"; shift ;;
    --keep)        KEEP=1 ;;
    -h|--help)     sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "неизвестный аргумент: $1" >&2; exit 2 ;;
  esac
  shift
done

# ---- предохранитель: не запускать на боевом сервере ----
# Скрипт делает DROP DATABASE локальной базы. На VPS «локальная» — это боевая
# база, с которой прямо сейчас работает диспетчер. Признак боевого сервера —
# работающий user-юнит бэкенда.
if systemctl --user is-active --quiet dpmodule-backend 2>/dev/null && [[ "${DPM_FORCE:-0}" != "1" ]]; then
  echo "ОТКАЗ: похоже, это боевой сервер (юнит dpmodule-backend запущен)." >&2
  echo "Скрипт удаляет и пересоздаёт базу — запускать его нужно в WSL, а не здесь." >&2
  echo "Если вы точно понимаете, что делаете: DPM_FORCE=1 $0 …" >&2
  exit 1
fi

command -v pg_restore >/dev/null || { echo "нет pg_restore — поставь postgresql-client-16" >&2; exit 1; }
LOCAL_MAJOR=$(pg_restore --version | grep -oE '[0-9]+' | head -1)
[[ "$LOCAL_MAJOR" -ge 16 ]] || { echo "pg_restore версии $LOCAL_MAJOR — нужен 16+ (на сервере PostgreSQL 16)" >&2; exit 1; }

# Пароль локальной роли берём из локального .env — тот же, что в config.local.yaml.
if [[ -f .env ]]; then set -a; . ./.env; set +a; fi
: "${PG_PASSWORD:?в ./.env нет PG_PASSWORD — он же пароль локальной роли $DB_USER}"

# ---- 1. дамп ----
if [[ -z "$DUMP_FILE" ]]; then
  : "${VPS:?укажи адрес сервера: export DPM_VPS=user@host}"
  DUMP_FILE=$(mktemp -t dpport-XXXXXX.dump)
  [[ $KEEP -eq 1 ]] || trap 'rm -f "$DUMP_FILE"' EXIT

  case "$MODE" in
    full)     PGDUMP_ARGS="" ;;
    schema)   PGDUMP_ARGS="--schema-only" ;;
    snapshot) PGDUMP_ARGS="--exclude-table-data=${DB_SCHEMA}.vagon_operation" ;;
  esac

  echo "→ снимаю дамп на $VPS (режим: $MODE)…"
  # shellcheck disable=SC2029  # переменные должны раскрыться на той стороне
  ssh "$VPS" "set -a; . $VPS_PATH/.env; set +a; \
      PGPASSWORD=\$PG_PASSWORD pg_dump -h localhost -U $DB_USER -d $DB -Fc $PGDUMP_ARGS" > "$DUMP_FILE"
  echo "  получено: $(du -h "$DUMP_FILE" | cut -f1)"
fi

[[ -s "$DUMP_FILE" ]] || { echo "дамп пустой — забор не удался" >&2; exit 1; }

# ---- 2. пересоздание локальной базы ----
echo "→ пересоздаю локальную базу $DB…"
$SUPER -v ON_ERROR_STOP=1 -q <<SQL
SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '$DB' AND pid <> pg_backend_pid();
DROP DATABASE IF EXISTS $DB;
DO \$\$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '$DB_USER') THEN
    CREATE ROLE $DB_USER LOGIN PASSWORD '$PG_PASSWORD';
  ELSE
    ALTER ROLE $DB_USER LOGIN PASSWORD '$PG_PASSWORD';
  END IF;
END \$\$;
CREATE DATABASE $DB OWNER $DB_USER;
-- как на сервере: search_path задан на уровне базы, приложение ходит без указания схемы
ALTER DATABASE $DB SET search_path = $DB_SCHEMA, public;
SQL

# ---- 3. заливка ----
echo "→ заливаю дамп…"
# COMMENT ON COLUMN не исключаем: из них Админ берёт русские подписи полей справочников.
PGPASSWORD="$PG_PASSWORD" pg_restore -h localhost -U "$DB_USER" -d "$DB" \
  --no-owner --no-privileges "$DUMP_FILE" || {
    echo "⚠️  pg_restore завершился с ошибками (часть выше). Обычно это безобидные ALTER OWNER;" >&2
    echo "    проверь таблицы командой ниже." >&2
  }

# ---- 4. проверка ----
CNT=$(PGPASSWORD="$PG_PASSWORD" psql -h localhost -U "$DB_USER" -d "$DB" -Atc \
      "select count(*) from information_schema.tables where table_schema='$DB_SCHEMA'")
DISL=$(PGPASSWORD="$PG_PASSWORD" psql -h localhost -U "$DB_USER" -d "$DB" -Atc \
      "select count(*) from dislocation" 2>/dev/null || echo '?')
echo "✓ готово: таблиц в схеме $DB_SCHEMA — $CNT, строк в dislocation — $DISL"
[[ "$MODE" == "snapshot" ]] && echo "  (vagon_operation перенесён без данных — для истории продвижения гоняй --full)"
exit 0
