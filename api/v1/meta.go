package handler

import (
	"haven.local/backend/vercelhandler"
	"net/http"
)

func Handler(w http.ResponseWriter, r *http.Request) {
	vercelhandler.Handler().ServeHTTP(w, r)
}
