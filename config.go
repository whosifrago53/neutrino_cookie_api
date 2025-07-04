package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/joho/godotenv"
)

// Configuration structure
type Config struct {
	APIKey        string
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	Port          string
}

// Global Redis client
var rdb *redis.Client

// Load configuration from environment variables
func LoadConfig() *Config {
	// Load .env file if it exists
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, using environment variables or defaults")
	}

	config := &Config{
		APIKey:        getEnv("API_KEY", ""), // No default - must be set
		RedisAddr:     getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       0,
		Port:          getEnv("PORT", "8080"),
	}

	// Validate required configuration
	if config.APIKey == "" {
		log.Fatal("API_KEY environment variable is required")
	}

	return config
}

// Helper function to get environment variable with default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Initialize Redis connection with high-performance settings
func InitRedis(config *Config) {
	log.Printf("Connecting to Redis at %s...", config.RedisAddr)

	rdb = redis.NewClient(&redis.Options{
		Addr:         config.RedisAddr,
		Password:     config.RedisPassword,
		DB:           config.RedisDB,
		PoolSize:     200,               // Increased for higher concurrency
		MinIdleConns: 20,                // More idle connections
		MaxRetries:   3,                 // Retry failed operations
		DialTimeout:  10 * time.Second,  // Increased dial timeout
		ReadTimeout:  5 * time.Second,   // Increased read timeout
		WriteTimeout: 5 * time.Second,   // Increased write timeout
		IdleTimeout:  300 * time.Second, // 5 minute idle timeout
	})

	// Test connection with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	log.Printf("Attempting to connect to Redis at %s...", config.RedisAddr)
	pong, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Failed to connect to Redis at %s: %v", config.RedisAddr, err)
	}
	log.Printf("Connected to Redis successfully: %s", pong)
}

// Get Redis client instance
func GetRedisClient() *redis.Client {
	return rdb
}
