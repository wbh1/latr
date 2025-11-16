// ABOUTME: Mock Linode API HTTP server for e2e testing
// ABOUTME: Maintains in-memory token state and implements subset of Linode API v4
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Token struct {
	ID      int       `json:"id"`
	Label   string    `json:"label"`
	Token   string    `json:"token,omitempty"`
	Scopes  string    `json:"scopes"`
	Created time.Time `json:"created"`
	Expiry  time.Time `json:"expiry"`
}

type TokensResponse struct {
	Data []Token `json:"data"`
}

type TokenResponse struct {
	ID      int       `json:"id"`
	Label   string    `json:"label"`
	Token   string    `json:"token"`
	Scopes  string    `json:"scopes"`
	Created time.Time `json:"created"`
	Expiry  time.Time `json:"expiry"`
}

type CreateTokenRequest struct {
	Label  string `json:"label"`
	Scopes string `json:"scopes"`
	Expiry string `json:"expiry"`
}

var (
	tokens   = make(map[int]Token)
	mu       sync.RWMutex
	nextID   = 1000
	seedInit sync.Once
)

func init() {
	seedInit.Do(func() {
		rand.Seed(time.Now().UnixNano())
	})
}

func generateToken() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 64)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func listTokensHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	mu.RLock()
	defer mu.RUnlock()

	tokenList := make([]Token, 0, len(tokens))
	for _, t := range tokens {
		// Don't include token value in list
		tokenCopy := t
		tokenCopy.Token = ""
		tokenList = append(tokenList, tokenCopy)
	}

	resp := TokensResponse{Data: tokenList}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func createTokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CreateTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	expiry, err := time.Parse(time.RFC3339, req.Expiry)
	if err != nil {
		http.Error(w, "Invalid expiry format", http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	id := nextID
	nextID++

	tokenValue := generateToken()
	token := Token{
		ID:      id,
		Label:   req.Label,
		Token:   tokenValue,
		Scopes:  req.Scopes,
		Created: time.Now(),
		Expiry:  expiry,
	}

	tokens[id] = token

	log.Printf("Created token: ID=%d, Label=%s, Expiry=%s", id, req.Label, expiry.Format(time.RFC3339))

	resp := TokenResponse{
		ID:      token.ID,
		Label:   token.Label,
		Token:   token.Token,
		Scopes:  token.Scopes,
		Created: token.Created,
		Expiry:  token.Expiry,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func getTokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract ID from path: /v4/profile/tokens/{id}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(parts[4])
	if err != nil {
		http.Error(w, "Invalid token ID", http.StatusBadRequest)
		return
	}

	mu.RLock()
	defer mu.RUnlock()

	token, exists := tokens[id]
	if !exists {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	// Don't include token value in GET
	resp := TokenResponse{
		ID:      token.ID,
		Label:   token.Label,
		Scopes:  token.Scopes,
		Created: token.Created,
		Expiry:  token.Expiry,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func deleteTokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract ID from path: /v4/profile/tokens/{id}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(parts[4])
	if err != nil {
		http.Error(w, "Invalid token ID", http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	if _, exists := tokens[id]; !exists {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	delete(tokens, id)
	log.Printf("Deleted token: ID=%d", id)

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{}"))
}

func resetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	tokens = make(map[int]Token)
	nextID = 1000
	log.Println("Reset: cleared all tokens")

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"reset"}`))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy"}`))
}

func main() {
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/reset", resetHandler)
	http.HandleFunc("/v4/profile/tokens", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			listTokensHandler(w, r)
		case http.MethodPost:
			createTokenHandler(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Handle /v4/profile/tokens/{id}
	http.HandleFunc("/v4/profile/tokens/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			getTokenHandler(w, r)
		case http.MethodDelete:
			deleteTokenHandler(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	port := "8080"
	log.Printf("Mock Linode API server starting on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
