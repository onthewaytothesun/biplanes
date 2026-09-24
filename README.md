# Бипланы

Браузерная дуэль двух бипланов по мотивам Biplanes Revival: https://biplanes.one-way.dev
Бот и Mini App: [@BiplanesGameBot](https://t.me/BiplanesGameBot)

## Устройство

| Путь | Что это |
|---|---|
| `index.html`, `style.css`, `game.js` | игра: чистые HTML/CSS/JS, без сборки |
| `account.js` | вход через Telegram, отправка матчей, рейтинг и профиль |
| `server/` | бэкенд на Go + SQLite (`modernc.org/sqlite`, без CGO) |
| `deploy/` | nginx vhost, systemd-юнит, пример env |

Пользователь идентифицируется Telegram ID (`users.tg_id` — первичный ключ). Вход двумя путями:

1. **Сайт → бот.** `POST /api/auth/login` выдаёт одноразовый `nonce` и `secret`. Сайт открывает
   `t.me/BiplanesGameBot?start=login_<nonce>`, бот просит подтвердить вход кнопкой, сайт опрашивает
   `POST /api/auth/login/poll {nonce, secret}` и получает токен сессии. Запрос живёт 5 минут и срабатывает один раз.
2. **Mini App.** `POST /api/auth/webapp {initData}`: подпись проверяется HMAC-ом токена бота, `auth_date` не старше 24 часов.

Сессия — Bearer-токен (в БД хранится его SHA-256), срок 90 дней.
Рейтинг — Эло против бота с фиксированным рейтингом 1200, старт 1000, K = 32 × (очки до победы / 10).
Матчи «вдвоём» сохраняются в историю без изменения рейтинга.
Результат присылает клиент; сервер отсекает невозможные счёт и длительность, но подделку результата это не исключает.

## API

| Метод | Путь | |
|---|---|---|
| POST | `/api/auth/webapp` | вход из Mini App |
| POST | `/api/auth/login`, `/api/auth/login/poll` | вход через бота |
| POST | `/api/auth/logout` | выход |
| GET | `/api/me` | профиль, статистика, место, последние матчи |
| POST | `/api/matches` | сохранить завершённый матч |
| GET | `/api/leaderboard` | топ-50 |
| POST | `/tg/webhook` | апдейты бота (заголовок `X-Telegram-Bot-Api-Secret-Token`) |

## Локальный запуск

```bash
cd server && go test ./...
go build -o ../dist/biplanes-dev .
# BOT_TOKEN берётся из окружения; без PUBLIC_URL webhook не ставится
cd .. && LISTEN_ADDR=127.0.0.1:18093 DB_PATH=/tmp/biplanes.db STATIC_DIR=$PWD ./dist/biplanes-dev
```

Сайт и API будут на http://127.0.0.1:18093. Вход через бота локально не заработает: webhook смотрит на прод.

## Деплой

См. `CLAUDE.md` (локальный, не в git) или кратко:

```bash
# статика
rsync -avz --delete --exclude README.md --exclude CLAUDE.md --exclude deploy --exclude server --exclude dist --exclude .git --exclude .gitignore ./ beget-vds:/var/www/biplanes/
# бэкенд
(cd server && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o ../dist/biplanes-server .)
scp dist/biplanes-server beget-vds:/opt/biplanes/biplanes-server.new
ssh beget-vds 'cd /opt/biplanes && mv biplanes-server.new biplanes-server && chown biplanes:biplanes biplanes-server && systemctl restart biplanes'
```
