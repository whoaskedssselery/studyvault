# Контекст сборки: каталог backend/
FROM golang:1.27-alpine AS build
WORKDIR /src

# Зависимости отдельным слоем: пока go.mod и go.sum не менялись, они берутся из кэша.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO выключен: бинарники статические и запускаются в минимальном образе без libc.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/reindex ./cmd/reindex \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/goose github.com/pressly/goose/v3/cmd/goose

# В рантайме нет ни shell, ни пакетного менеджера, и работает он не от root.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /out/reindex /out/goose /
COPY migrations /migrations
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/api"]
