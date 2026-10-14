# Файлы для сдачи: роль Frontend

Для задания достаточно **index.html + mock.json** рядом. HTML содержит собранный React-интерфейс, CSS и JavaScript; mock.json — отдельные фейковые данные. serve.mjs — локальный сервер с прокси к API.

```powershell
cd D:\spo2\studyvault\deliverables\frontend
node serve.mjs --port 4174
```

Откройте http://localhost:4174/. По умолчанию это учебный режим без API: данные читаются из mock.json, действия сохраняются в localStorage.

Для того же интерфейса с настоящим сервером откройте http://localhost:4174/?mode=api#/. Для возврата — http://localhost:4174/?mode=demo#/. Выбор сохраняется между перезагрузками. Backend запускается отдельно по инструкции docs/frontend.md; serve.mjs проксирует /backend-api/* в http://127.0.0.1:8080/v1/* и переписывает Path refresh-cookie. Для другого сервера задайте STUDYVAULT_API_TARGET.

Hash-маршруты #/… позволяют разместить два сдаваемых файла на обычном статическом хостинге. Для режима API нужен прокси; для просмотра через file:// браузер блокирует JSON, поэтому используйте HTTP-сервер.

Пересборка из frontend/: `npm run package:submission`. Исходники, автоматические проверки и отчёт находятся в frontend/ и docs/assignment-2/. Документ аналитика и материалы остальных участников готовятся отдельно.
