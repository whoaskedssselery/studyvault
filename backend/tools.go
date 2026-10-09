//go:build tools

package tools

import (
	_ "github.com/go-chi/chi/v5"
	_ "github.com/go-chi/cors"
	_ "github.com/golang-jwt/jwt/v5"
	_ "github.com/google/uuid"
	_ "github.com/jackc/pgx/v5"
	_ "github.com/joho/godotenv"
	_ "github.com/meilisearch/meilisearch-go"
	_ "github.com/minio/minio-go/v7"
	_ "github.com/oapi-codegen/runtime"
	_ "golang.org/x/crypto/bcrypt"
)
