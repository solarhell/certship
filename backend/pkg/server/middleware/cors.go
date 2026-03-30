package middleware

import (
	connectcors "connectrpc.com/cors"
	"github.com/rs/cors"
)

func CORS() *cors.Cors {
	return cors.New(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: connectcors.AllowedMethods(),
		AllowedHeaders: []string{"*"},
		ExposedHeaders: []string{"*"},
		MaxAge:         7200,
	})
}
