DPmodule — новый сервис по образцу gtlogic (стек Go backend + Angular frontend + PostgreSQL).
Домен app.gtport.ru уже настроен: DNS резолвится на 147.45.216.229, SSL-сертификат
Let's Encrypt выпущен и автопродлевается. Сейчас домен отдаёт статическую заглушку;
задача деплоя — заменить заглушку на проксирование к запущенному приложению.

## Сетевые параметры

- Сервер (хост): 147.45.216.229
- Домен приложения: app.gtport.ru (HTTPS готов)
- Пользователь: alex
- Рабочая директория проекта: /home/alex/projects/DPmodule
- Эталон-образец: /home/alex/projects/gtlogic (НЕ менять, только как референс)

## Распределение портов (ЗАНЯТЫЕ — не использовать)

- 80, 443 — nginx
- 3000 (127.0.0.1) — Open WebUI (gtport.ru), НЕ ТРОГАТЬ
- 22 — SSH
- 5432 — PostgreSQL (если будет установлен локально)

## Рекомендуемые порты для DPmodule

- Backend (Go): 8080 (слушать на 127.0.0.1:8080, наружу только через nginx)
- Frontend: собирается в статику и отдаётся nginx напрямую ИЛИ
  dev-сервер на 127.0.0.1:3001 (только для разработки)

Важно: приложение слушает ТОЛЬКО на 127.0.0.1 (localhost), не на 0.0.0.0.
Внешний доступ обеспечивает nginx. Порт приложения не открывать в интернет напрямую.

## Файлы nginx

- Конфиг приложения: /etc/nginx/sites-available/app.gtport.ru
- Симлинк активации: /etc/nginx/sites-enabled/app.gtport.ru
- SSL-сертификат: /etc/letsencrypt/live/app.gtport.ru/fullchain.pem
- SSL-ключ: /etc/letsencrypt/live/app.gtport.ru/privkey.pem

## ТЕКУЩЕЕ состояние конфига (заглушка)

Сейчас блок location отдаёт статику из /var/www/app-placeholder.
Certbot добавил в файл listen 443 и пути к SSL.

ВАЖНО: НЕ редактировать строки listen 443 ssl, ssl_certificate*, редирект с 80 —
их поставил certbot. Менять только содержимое блоков location.

## Проверка и применение nginx (строго в таком порядке)
sudo nginx -t
sudo systemctl reload nginx

