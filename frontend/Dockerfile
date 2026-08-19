# --- Build stage ---
# Node 24 — как на машине разработчика и на VPS (Angular 21 требует
# ^20.19 || ^22.12 || >=24). Держим одну версию везде: расхождение вылезает
# не ошибкой сборки, а разным поведением в рантайме.
FROM node:24-alpine AS build
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .

# ⚠️ КАКОЙ Keycloak попадёт в бандл, решает ЭТА переменная — адрес зашивается
# при сборке, на деплое его уже не поменять.
#   production → environment.production.ts → https://uport1.ru (Keycloak
#   корпоративного контура; верно для стенда из config.docker.yaml).
# Для стенда со СВОИМ Keycloak (наш VPS 95850.koara.live отдаёт его тем же
# nginx, same-origin) production-сборка ЛОМАЕТ вход: логин уходит на чужой
# сервер, который вдобавок отвечает 403 по гео. Разбор — docs/KEYCLOAK_HANDOVER.md
# §5.2; выбор окружения для такого стенда за владельцем, файлы environments
# трогать самовольно нельзя.
ARG NG_CONFIG=production
RUN npm run build -- --configuration "$NG_CONFIG"

# --- Runtime stage ---
FROM nginx:alpine

# nginx.conf — ШАБЛОН: entrypoint образа прогоняет его через envsubst и кладёт
# результат в /etc/nginx/conf.d/default.conf. Так адрес бэкенда задаётся на
# стенде переменной, без пересборки образа.
COPY nginx.conf /etc/nginx/templates/default.conf.template
COPY --from=build /app/dist/app/browser /usr/share/nginx/html

# Серт-ЗАГЛУШКА для https-порта 8543: с ней контейнер поднимается и БЕЗ
# примонтированного сертификата (стенды за ingress, локальная проверка) —
# https-порт там просто не публикуют. Боевой сертификат монтируется поверх
# заглушки (раскладка /etc/certs на хосте — как у соседей по платформе):
#   -v /etc/certs/dpport-frontend-production.crt:/etc/nginx/certs/tls.crt:ro
#   -v /etc/certs/dpport-frontend-production.key:/etc/nginx/certs/tls.key:ro
# Зачем https вообще: вход в модуль (PKCE) требует Web Crypto API, а браузер
# даёт его только в безопасном контексте — https или localhost. На http://IP
# приложение падает до логина («Web Crypto API is not available», 18.08.2026).
RUN apk add --no-cache openssl \
 && mkdir -p /etc/nginx/certs \
 && openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
      -subj "/CN=dpport-placeholder" \
      -keyout /etc/nginx/certs/tls.key \
      -out /etc/nginx/certs/tls.crt

# API_BACKEND — «хост:порт» Go-бэкенда, куда уходит /api. Порт совпадает с
# http.port бэкенда (config.docker.yaml стенда: 8580).
# Дефолт — АДРЕС ХОСТА стенда: там фронт и бэк подняты разными compose-проектами,
# общей сети между ними нет. Внутри одной сети (локальная проверка через
# deployments/docker-compose.yml) переменная переопределяется именем сервиса.
ENV API_BACKEND=176.53.160.9:8580

# Подстановку ограничиваем ДВУМЯ именами: в конфиге полно собственных
# переменных nginx ($uri, $status, $request_id…), и envsubst без фильтра
# затёр бы их пустыми строками.
ENV NGINX_ENVSUBST_FILTER="^(API_BACKEND|NGINX_LOCAL_RESOLVERS)$"

# Просит entrypoint вычитать DNS-серверы из /etc/resolv.conf и положить их в
# NGINX_LOCAL_RESOLVERS — без этого переменная пустая, а `resolver ;` в конфиге
# роняет nginx. Нужна для ленивого резолва адреса бэкенда (см. nginx.conf).
ENV NGINX_ENTRYPOINT_LOCAL_RESOLVERS=1

# Порты должны совпадать с listen в nginx.conf: 8581 — SPA по http,
# 8543 — то же SPA по https (нужен примонтированный сертификат, см. выше),
# 9581 — метрики. Раскладка модуля на стенде: бэкенд 8580 / его метрики 9580.
# За ingress наружу торчит 8581; без ingress пользователи ходят на 8543 —
# вход работает только по https.
EXPOSE 8581 8543 9581

# Healthcheck: nginx отдаёт SPA — проверяем, что index.html доступен.
# wget есть в nginx:alpine (busybox), curl ставить не нужно.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8581/ >/dev/null 2>&1 || exit 1

CMD ["nginx", "-g", "daemon off;"]
