package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// Request/Response structures
type SaveCookieRequest struct {
	Operation string `json:"operation"`
	Details   struct {
		Category string `json:"category"`
		Cookie   string `json:"cookie"`
	} `json:"details"`
}

type RemoveCookieRequest struct {
	Operation string `json:"operation"`
	Details   struct {
		Cookie string `json:"cookie"`
	} `json:"details"`
}

type SuccessResponse struct {
	Success bool `json:"success"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type StatsResponse struct {
	Details map[string]int `json:"details"`
}

type CookieData struct {
	Cookie    string `json:"cookie"`
	Category  string `json:"category"`
	Timestamp int64  `json:"timestamp"`
}

// Helper function to send error response
func sendErrorResponse(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(ErrorResponse{Error: message})
}

// Helper function to send success response
func sendSuccessResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SuccessResponse{Success: true})
}

// Middleware for API key authentication
func Authenticate(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		config := LoadConfig()
		apiKey := r.Header.Get("x-api-key")
		if apiKey != config.APIKey {
			sendErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	}
}

// Helper function to generate Redis key
func generateCookieKey(userID, cookieType, category string) string {
	return fmt.Sprintf("cookies:%s:%s:%s", userID, cookieType, category)
}

// Helper function to generate user stats key
func generateStatsKey(userID, cookieType string) string {
	return fmt.Sprintf("stats:%s:%s", userID, cookieType)
}

// Save cookie handler
func SaveCookie(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user_id"]
	cookieType := vars["cookie_type"]

	var req SaveCookieRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.Operation != "saveCookie" || req.Details.Cookie == "" {
		sendErrorResponse(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Set default category if not provided
	category := req.Details.Category
	if category == "" {
		category = "default"
	}

	ctx := context.Background()
	timestamp := time.Now().Unix()

	// Create cookie data
	cookieData := CookieData{
		Cookie:    req.Details.Cookie,
		Category:  category,
		Timestamp: timestamp,
	}

	cookieJSON, err := json.Marshal(cookieData)
	if err != nil {
		sendErrorResponse(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Save cookie to Redis Hash
	cookieKey := generateCookieKey(userID, cookieType, category)
	err = GetRedisClient().HSet(ctx, cookieKey, req.Details.Cookie, string(cookieJSON)).Err()
	if err != nil {
		sendErrorResponse(w, "Failed to save cookie", http.StatusInternalServerError)
		return
	}

	// Update stats
	statsKey := generateStatsKey(userID, cookieType)
	err = GetRedisClient().HIncrBy(ctx, statsKey, category, 1).Err()
	if err != nil {
		// Log error but don't fail the request
		fmt.Printf("Failed to update stats: %v\n", err)
	}

	// Return success response
	sendSuccessResponse(w)
}

// Remove cookie handler
func RemoveCookie(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user_id"]
	cookieType := vars["cookie_type"]

	var req RemoveCookieRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.Operation != "removeCookie" || req.Details.Cookie == "" {
		sendErrorResponse(w, "Invalid request", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	rdb := GetRedisClient()

	// Find and remove cookie from all categories
	pattern := fmt.Sprintf("cookies:%s:%s:*", userID, cookieType)
	keys, err := rdb.Keys(ctx, pattern).Result()
	if err != nil {
		sendErrorResponse(w, "Failed to search cookies", http.StatusInternalServerError)
		return
	}

	var removedCategory string
	for _, key := range keys {
		// Check if cookie exists in this category
		exists, err := rdb.HExists(ctx, key, req.Details.Cookie).Result()
		if err != nil {
			continue
		}

		if exists {
			// Get cookie data to extract category for stats update
			cookieDataStr, err := rdb.HGet(ctx, key, req.Details.Cookie).Result()
			if err == nil {
				var cookieData CookieData
				if json.Unmarshal([]byte(cookieDataStr), &cookieData) == nil {
					removedCategory = cookieData.Category
				}
			}

			// Remove the cookie
			err = rdb.HDel(ctx, key, req.Details.Cookie).Err()
			if err != nil {
				sendErrorResponse(w, "Failed to remove cookie", http.StatusInternalServerError)
				return
			}
			break
		}
	}

	// Update stats if cookie was found and removed
	if removedCategory != "" {
		statsKey := generateStatsKey(userID, cookieType)
		err = rdb.HIncrBy(ctx, statsKey, removedCategory, -1).Err()
		if err != nil {
			fmt.Printf("Failed to update stats: %v\n", err)
		}

		// Remove category from stats if count reaches 0
		count, err := rdb.HGet(ctx, statsKey, removedCategory).Int()
		if err == nil && count <= 0 {
			rdb.HDel(ctx, statsKey, removedCategory)
		}
	}

	// Return success response
	sendSuccessResponse(w)
}

// Get cookies handler
func GetCookies(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user_id"]
	cookieType := vars["cookie_type"]

	// Parse query parameters
	qtyStr := r.URL.Query().Get("qty")
	randomStr := r.URL.Query().Get("random")
	category := r.URL.Query().Get("category")

	qty := 0
	if qtyStr != "" {
		var err error
		qty, err = strconv.Atoi(qtyStr)
		if err != nil || qty <= 0 {
			sendErrorResponse(w, "Invalid qty parameter", http.StatusBadRequest)
			return
		}
	}

	isRandom := randomStr == "true"
	ctx := context.Background()
	rdb := GetRedisClient()

	var cookies []CookieData
	var pattern string

	// Build pattern based on category filter
	if category != "" {
		pattern = fmt.Sprintf("cookies:%s:%s:%s", userID, cookieType, category)
	} else {
		pattern = fmt.Sprintf("cookies:%s:%s:*", userID, cookieType)
	}

	// Get all matching keys
	keys, err := rdb.Keys(ctx, pattern).Result()
	if err != nil {
		sendErrorResponse(w, "Failed to retrieve cookies", http.StatusInternalServerError)
		return
	}

	// Collect all cookies
	for _, key := range keys {
		cookieMap, err := rdb.HGetAll(ctx, key).Result()
		if err != nil {
			continue
		}

		for _, cookieDataStr := range cookieMap {
			var cookieData CookieData
			if json.Unmarshal([]byte(cookieDataStr), &cookieData) == nil {
				cookies = append(cookies, cookieData)
			}
		}
	}

	// Apply random shuffle if requested
	if isRandom && len(cookies) > 0 {
		rand.Seed(time.Now().UnixNano())
		rand.Shuffle(len(cookies), func(i, j int) {
			cookies[i], cookies[j] = cookies[j], cookies[i]
		})
	}

	// Apply quantity limit
	if qty > 0 && len(cookies) > qty {
		cookies = cookies[:qty]
	}

	// Return cookies
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cookies)
}

// Get stats handler
func GetStats(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user_id"]
	cookieType := vars["cookie_type"]

	ctx := context.Background()
	statsKey := generateStatsKey(userID, cookieType)

	// Get all stats
	stats, err := GetRedisClient().HGetAll(ctx, statsKey).Result()
	if err != nil {
		sendErrorResponse(w, "Failed to retrieve stats", http.StatusInternalServerError)
		return
	}

	// Convert string values to integers
	details := make(map[string]int)
	for category, countStr := range stats {
		if count, err := strconv.Atoi(countStr); err == nil && count > 0 {
			details[category] = count
		}
	}

	// Return stats
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(StatsResponse{Details: details})
}

// Combined handler for POST operations
func HandleCookieOperations(w http.ResponseWriter, r *http.Request) {
	// Parse request to determine operation
	var operationCheck struct {
		Operation string `json:"operation"`
	}

	// Create a copy of the request body to preserve it
	body := make([]byte, r.ContentLength)
	r.Body.Read(body)

	// Reset request body for the actual handlers
	r.Body = io.NopCloser(strings.NewReader(string(body)))

	// Parse operation
	if err := json.Unmarshal(body, &operationCheck); err != nil {
		sendErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Reset request body again
	r.Body = io.NopCloser(strings.NewReader(string(body)))

	// Route to appropriate handler
	switch operationCheck.Operation {
	case "saveCookie":
		SaveCookie(w, r)
	case "removeCookie":
		RemoveCookie(w, r)
	default:
		sendErrorResponse(w, "Invalid operation", http.StatusBadRequest)
	}
}
