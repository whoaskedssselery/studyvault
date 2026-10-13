# studyvault

База знаний студента: конспекты, материалы, ссылки и заметки в одном месте.

## Статус

Бэкенд в разработке: Go, PostgreSQL, Meilisearch, S3-хранилище.

Фронтенд: React 19 + TypeScript + Vite. Библиотека, редактор, избранное, комментарии, оценки, рейтинг и модерация работают в локальной демоверсии независимо от API.

## Запуск фронтенда

```bash
cd frontend
npm ci
npm run dev
```

Откройте адрес из терминала, обычно `http://127.0.0.1:5173`.

[Возможности фронтенда и инструкция](docs/frontend.md) · [Скриншоты для отчёта](docs/screenshots)

## Запуск инфраструктуры бэкенда

```bash
cp .env.example .env
docker compose up -d
cd backend
go tool goose -dir migrations postgres "$DATABASE_URL" up
```

Postgres доступен на `localhost:5440`, Meilisearch на `7700`, S3 на `9000` (консоль `9001`).

## Идеи

- [ ] Конспекты по предметам и семестрам
- [ ] Теги и поиск
- [ ] Хранение ссылок и файлов
