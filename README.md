# studyvault

База знаний студента: конспекты, материалы, ссылки и заметки в одном месте.

## Статус

Бэкенд в разработке: Go, PostgreSQL, Meilisearch, S3-хранилище.

## Запуск

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
