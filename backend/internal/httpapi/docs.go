package httpapi

import (
	"net/http"

	spec "github.com/whoaskedssselery/studyvault/backend/api"
)

// Swagger UI подгружается с CDN с проверкой целостности (SRI): подменённый файл браузер не выполнит.
// Версия и хеши зафиксированы, поэтому при смене версии хеши надо пересчитать. Токен не сохраняется
// в localStorage. Спецификацию отдаёт сам сервер: так API можно
// пощупать из браузера (кнопка Authorize принимает access-токен, Try it out шлёт запросы).
const docsPage = `<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="light only">
  <style>html, body { background: #fff; color-scheme: light; }</style>
  <title>studyvault API</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui.css"
        integrity="sha384-wxLW6kwyHktdDGr6Pv1zgm/VGJh99lfUbzSn6HNHBENZlCN7W602k9VkGdxuFvPn" crossorigin="anonymous">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui-bundle.js"
          integrity="sha384-wmyclcVGX/WhUkdkATwhaK1X1JtiNrr2EoYJ+diV3vj4v6OC5yCeSu+yW13SYJep" crossorigin="anonymous"></script>
  <script>
    window.ui = SwaggerUIBundle({
      url: '/openapi.yaml',
      dom_id: '#swagger-ui',
      persistAuthorization: false,
      tryItOutEnabled: true,
    });
  </script>
</body>
</html>
`

func serveDocs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(docsPage))
}

func serveSpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(spec.OpenAPI)
}
