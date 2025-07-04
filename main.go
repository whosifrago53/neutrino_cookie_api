package main

import (
	"log"
	"net/http"

	"github.com/gorilla/mux"
)

func main() {
	// Load configuration
	config := LoadConfig()

	// Initialize Redis
	InitRedis(config)

	// Setup routes
	r := mux.NewRouter()

	// API routes with authentication
	api := r.PathPrefix("/api/v3").Subrouter()
	api.HandleFunc("/cookies/{cookie_type}/{user_id}", Authenticate(HandleCookieOperations)).Methods("POST")
	api.HandleFunc("/cookies/{cookie_type}/{user_id}", Authenticate(GetCookies)).Methods("GET")
	api.HandleFunc("/cookies/{cookie_type}/{user_id}/stats", Authenticate(GetStats)).Methods("GET")

	// Start server
	log.Printf("Cookie API Server starting on :%s", config.Port)
	log.Fatal(http.ListenAndServe(":"+config.Port, r))
}
