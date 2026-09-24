# Бипланы

Браузерная дуэль двух бипланов по мотивам Biplanes Revival. Чистые HTML/CSS/JS, без сборки и зависимостей
(только шрифты с Google Fonts). Графика и звук генерируются кодом.

- `index.html` — разметка
- `style.css` — стили, экранное управление, полноэкранный режим
- `game.js` — физика, бот, отрисовка, ввод

Локально: открыть `index.html` в браузере (или `python3 -m http.server` в этой папке).

## Деплой

Живёт на https://biplanes.one-way.dev — отдельный nginx server block, статика из `/var/www/biplanes`.
Сертификат — общий wildcard `*.one-way.dev` (`/etc/ssl/one-way/`), DNS — wildcard `*.one-way.dev → 155.212.223.222`.

Обновить файлы (из папки `biplanes/`):

```bash
rsync -avz --delete --exclude README.md --exclude deploy ./ beget-vds:/var/www/biplanes/
```

Конфиг nginx — `deploy/biplanes.one-way.dev`. Если меняешь:

```bash
scp deploy/biplanes.one-way.dev beget-vds:/etc/nginx/sites-available/biplanes.one-way.dev
ssh beget-vds 'nginx -t && systemctl reload nginx'
```
