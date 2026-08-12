#!/usr/bin/env bash
# ============================================================================
# anb_backfill.sh — разовый прогон истории инстанса АНБ из папки выгрузок ЛК.
#
# Что делает: грузит xlsx-выгрузки ЛК (формат SPV4664, имя «114_ДД.ММ.ГГГГ ЧЧ_ММ»)
# В ХРОНОЛОГИЧЕСКОМ порядке через штатный двухшаговый приём (upload → process)
# инстанса 2 — база наполняется так, как будто система работала все эти дни:
# снимки сменяют друг друга, carry-over ведёт рейсы, прибытия/выгрузки ложатся
# в vagon_history, считаются прогнозы.
#
# Гард устаревания снимет и вернёт сам: на время прогона max_staleness_minutes
# поднимается до 100000000 (файлы прошлых дней иначе отвергаются), после —
# возвращается 720; бэкенд перезапускается (пороги читаются в RAM на старте).
# Гард «не старее текущего» не трогается — файлы идут по возрастанию меток.
#
# Запуск с машины разработчика (WSL), нужен ssh-доступ к VPS (DPM_VPS) и
# пароль пользователя боевого Keycloak (запросит, нигде не сохраняет):
#     ./scripts/anb_backfill.sh [папка с xlsx]   # дефолт ~/projects/DP/LK ANB
# ============================================================================
set -euo pipefail

DIR="${1:-$HOME/projects/DP/LK ANB}"
BASE="${ANB_BASE:-https://anb-port.duckdns.org}"
KC="${ANB_KC:-https://95850.koara.live}"
VPS="${DPM_VPS:?export DPM_VPS=user@host}"
KC_USER="${ANB_USER:-disp}"

[[ -d "$DIR" ]] || { echo "нет папки: $DIR" >&2; exit 1; }

read -rsp "Пароль пользователя '$KC_USER' (боевой Keycloak): " KC_PASS; echo

token() {
  curl -sf -X POST "$KC/realms/iqport/protocol/openid-connect/token" \
    -d client_id=iqport-dpport -d grant_type=password \
    --data-urlencode "username=$KC_USER" --data-urlencode "password=$KC_PASS" \
    | python3 -c "import sys,json; print(json.load(sys.stdin)['access_token'])"
}
T=$(token) || { echo "вход в Keycloak не удался" >&2; exit 1; }
echo "вход в Keycloak: ок"

guard() {  # guard <минуты> — порог устаревания + рестарт бэкенда инстанса 2
  ssh "$VPS" 'set -a; . ~/projects/DPmodule2/.env; set +a; export PGPASSWORD="$PG_PASSWORD"
    psql -h localhost -U gtport_app -d dpport_river -qc \
      "UPDATE dpport.client_settings SET ingest_policy = jsonb_set(ingest_policy,
         '"'"'{dislocation,max_staleness_minutes}'"'"', '"'"''"$1"''"'"'), updated_at = now() WHERE id = 1"
    systemctl --user restart dpmodule2-backend && sleep 4
    curl -s -o /dev/null -w "backend after guard='"$1"': health=%{http_code}\n" http://127.0.0.1:8081/health'
}

restore() { echo; echo "== возвращаю порог 720 =="; guard 720; }
trap restore EXIT

echo "== поднимаю порог устаревания на время прогона =="
guard 100000000

# Имена «114_ДД.ММ.ГГГГ ЧЧ_ММ.xlsx» (бывает суффикс « (1)» от повторного
# скачивания браузером) → сортировка по ГГГГММДД ЧЧММ. Имя, не подошедшее под
# шаблон, роняет прогон заранее — молча пропускать файлы нельзя.
mapfile -t KEYED < <(
  find "$DIR" -maxdepth 1 -name '*.xlsx' -printf '%f\n' |
  sed -E 's/^(.+)_([0-9]{2})\.([0-9]{2})\.([0-9]{4}) ([0-9]{2})_([0-9]{2})( \([0-9]+\))?\.xlsx$/\4\3\2\5\6\t&/' |
  sort
)
FILES=()
for line in "${KEYED[@]}"; do
  if [[ "$line" =~ ^[0-9]{12}$'\t'(.+)$ ]]; then
    FILES+=("${BASH_REMATCH[1]}")
  else
    echo "имя вне шаблона даты — прогон остановлен: $line" >&2; exit 1
  fi
done
echo "файлов к загрузке: ${#FILES[@]}"

n=0; t0=$(date +%s)
for f in "${FILES[@]}"; do
  n=$((n+1))
  # токен живёт ~5 минут — обновляем каждые 20 файлов
  (( n % 20 == 1 )) && T=$(token)
  up=$(curl -sf -X POST "$BASE/api/v1/dislocation/lk/upload" \
        -H "Authorization: Bearer $T" -F "file=@$DIR/$f") \
    || { echo "СТОП: upload '$f' не прошёл" >&2; exit 1; }
  res=$(curl -s -w '\n%{http_code}' -X POST "$BASE/api/v1/dislocation/lk/process" \
        -H "Authorization: Bearer $T")
  code=${res##*$'\n'}; body=${res%$'\n'*}
  if [[ "$code" != 200 ]]; then
    echo "СТОП на файле '$f': HTTP $code — $body" >&2; exit 1
  fi
  cnt=$(python3 -c "import sys,json; r=json.loads(sys.argv[1]); print(f\"ваг {r.get('count')} прибыло {r.get('status9_inserted',0)} carry {r.get('carry_matched',0)}\")" "$body" 2>/dev/null || echo '?')
  printf '[%3d/%d] %-28s %s\n' "$n" "${#FILES[@]}" "$f" "$cnt"
done

echo "готово за $(( ($(date +%s)-t0)/60 )) мин"
# restore выполнится trap'ом
