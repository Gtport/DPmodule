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

# API_BACKEND — «хост:порт» Go-бэкенда, куда уходит /api. Порт должен совпадать
# с http.port бэкенда (config.docker.yaml стенда: 8580). Имя по умолчанию —
# api-backend; в нашем deployments/docker-compose.yml сервис называется app,
# поэтому там переменная переопределяется.
ENV API_BACKEND=api-backend:8580

# Подстановку ограничиваем ДВУМЯ именами: в конфиге полно собственных
# переменных nginx ($uri, $status, $request_id…), и envsubst без фильтра
# затёр бы их пустыми строками.
ENV NGINX_ENVSUBST_FILTER="^(API_BACKEND|NGINX_LOCAL_RESOLVERS)$"

# Просит entrypoint вычитать DNS-серверы из /etc/resolv.conf и положить их в
# NGINX_LOCAL_RESOLVERS — без этого переменная пустая, а `resolver ;` в конфиге
# роняет nginx. Нужна для ленивого резолва адреса бэкенда (см. nginx.conf).
ENV NGINX_ENTRYPOINT_LOCAL_RESOLVERS=1

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1/ >/dev/null 2>&1 || exit 1

CMD ["nginx", "-g", "daemon off;"]
