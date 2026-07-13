#!/usr/bin/env bash
# ============================================================================
#  asu-cron.sh — триггер автозагрузки дислокации из АСУ-АСУ.
#
#  Ставится в системный cron (каждые 10 минут):
#     */10 * * * * /home/alex/projects/DPmodule/deployments/asu-cron.sh >> /var/log/dpmodule-asu.log 2>&1
#
#  Что делает: берёт JWT сервис-аккаунта в Keycloak (grant client_credentials) и
#  дёргает защищённую ручку POST /api/v1/dislocation/asu/pull на localhost. Бэкенд
#  сам тянет всех клиентов АСУ, сверяет метки формирования (порог из настроек) и
#  пересобирает снимок. Рассогласование меток → 409 (лог на стороне бэкенда), снимок
#  не трогается. АСУ недоступна → 502.
#
#  Секреты — из окружения (env-файл сервиса / Vault), НЕ в git:
#     KC_TOKEN_URL     — token endpoint реалма (…/protocol/openid-connect/token)
#     KC_CLIENT_ID     — confidential-клиент со Service Accounts
#     KC_CLIENT_SECRET — его секрет
#     APP_URL          — базовый URL бэкенда (по умолч. http://127.0.0.1:8080)
# ============================================================================
set -euo pipefail

: "${KC_TOKEN_URL:?KC_TOKEN_URL не задан}"
: "${KC_CLIENT_ID:?KC_CLIENT_ID не задан}"
: "${KC_CLIENT_SECRET:?KC_CLIENT_SECRET не задан}"
APP_URL="${APP_URL:-http://127.0.0.1:8080}"

# 1. Токен сервис-аккаунта.
TOKEN=$(curl -fsS -X POST "$KC_TOKEN_URL" \
  -d grant_type=client_credentials \
  -d client_id="$KC_CLIENT_ID" \
  -d client_secret="$KC_CLIENT_SECRET" \
  | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p')

if [ -z "$TOKEN" ]; then
  echo "$(date '+%F %T') ОШИБКА: не удалось получить токen Keycloak" >&2
  exit 1
fi

# 2. Триггер автозагрузки. Печатаем HTTP-код и тело для лога.
HTTP_CODE=$(curl -sS -o /tmp/asu-pull-body.json -w '%{http_code}' \
  -X POST "$APP_URL/api/v1/dislocation/asu/pull" \
  -H "Authorization: Bearer $TOKEN")

echo "$(date '+%F %T') asu/pull → HTTP $HTTP_CODE: $(cat /tmp/asu-pull-body.json)"

# 2xx — успех; иначе ненулевой код выхода (cron/мониторинг заметит).
case "$HTTP_CODE" in
  2*) exit 0 ;;
  *)  exit 1 ;;
esac
