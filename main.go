package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	 _ "github.com/lib/pq"	
	 "github.com/google/uuid"
	 "github.com/joho/godotenv"
	 "database/sql"
	 "os"
	 "github.com/bootdotdev/learn-http-servers/internal/database"
	 "time"
)

type apiConfig struct {
	fileserverHits atomic.Int32
    db *database.Queries	
	platform string
}

type chirpRequest struct {
	Body string `json:"body"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type cleanedResponse struct {
	CleanedBody string `json:"cleaned_body"`
}

type User struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
}

type reqCreateUser struct {
	Email     string    `json:"email"`
}

func main() {
	// BD
	// Se carga el ENV
	if err := godotenv.Load(); err != nil {
		log.Fatal(err)
	}	
	// Se busca la string para la DB
	dbURL := os.Getenv("DB_URL")
	// Se abre la DB
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("error connecting to db: %v", err)
	}
	defer db.Close()
	// Se crean las queries
	dbQueries := database.New(db)

		// after creating dbQueries:
	apiCfg := &apiConfig{
		db:       dbQueries,
		platform: os.Getenv("PLATFORM"),
	}

//	apiCfg := &apiConfig{}

	const port = "8080"

	mux := http.NewServeMux()

	// 1. Readiness endpoint (/api/healthz)
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// 2. FileServer under /app/
	fs := http.FileServer(http.Dir("."))
	handler := http.StripPrefix("/app/", fs)
	mux.Handle("/app/", apiCfg.middlewareMetricsInc(handler))

	// 3. Admin metrics (HTML)
	mux.HandleFunc("GET /admin/metrics", apiCfg.handlerAdminMetrics)

	// 4. Admin reset
	mux.HandleFunc("POST /admin/reset", apiCfg.handlerAdminReset)

	// 5. Chirp validation + cleaning
	mux.HandleFunc("POST /api/validate_chirp", apiCfg.handlerValidateChirp)
	
	// 6. Get user
	mux.HandleFunc("POST /api/users", apiCfg.handlerUsersCreate)

	// Server setup
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	log.Printf("Serving on port: %s\n", port)
	log.Fatal(srv.ListenAndServe())
}

// Middleware para contar hits
func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

// Handler para /admin/metrics
func (cfg *apiConfig) handlerAdminMetrics(w http.ResponseWriter, r *http.Request) {
	count := cfg.fileserverHits.Load()

	html := fmt.Sprintf(`
<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
    <p>Chirpy has been visited %d times!</p>
  </body>
</html>`, count)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html))
}

// Handler para /admin/reset
func (cfg *apiConfig) handlerAdminReset(w http.ResponseWriter, r *http.Request) {
	// in the reset handler:
	if cfg.platform != "dev" {
		respondWithError(w, http.StatusForbidden, "forbidden")
		return
	}

	err := cfg.db.DeleteUsers(r.Context())
  	if err != nil {
        respondWithError(w, http.StatusInternalServerError, "could not delete users")
        return
	} 	

	cfg.fileserverHits.Store(0)
	w.WriteHeader(http.StatusOK)

}

// Handler para /api/validate_chirp
func (cfg *apiConfig) handlerValidateChirp(w http.ResponseWriter, r *http.Request) {
	var req chirpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Something went wrong")
		return
	}

	if len(req.Body) > 140 {
		respondWithError(w, http.StatusBadRequest, "Chirp is too long")
		return
	}

	cleaned := cleanChirp(req.Body)
	respondWithJSON(w, http.StatusOK, cleanedResponse{CleanedBody: cleaned})
}

// Handler para /api/users
func (cfg *apiConfig) handlerUsersCreate(w http.ResponseWriter, r *http.Request) {
	var req reqCreateUser
	var res User

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	if strings.TrimSpace(req.Email) == ""{
		respondWithError(w, http.StatusBadRequest, "email required")
        return
	}

    dbUser, err := cfg.db.CreateUser(r.Context(), req.Email)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "could not create user")
        return
    }

	res = User{ID: dbUser.ID, 
			    CreatedAt: dbUser.CreatedAt, 
				UpdatedAt: dbUser.UpdatedAt, 
				Email: dbUser.Email}

	respondWithJSON(w, http.StatusCreated, res)

}

// --- helpers ---

func respondWithError(w http.ResponseWriter, code int, msg string) {
	respondWithJSON(w, code, errorResponse{Error: msg})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	data, _ := json.Marshal(payload)
	w.Write(data)
}

func cleanChirp(body string) string {
	profane := map[string]struct{}{
		"kerfuffle": {},
		"sharbert":  {},
		"fornax":    {},
	}

	words := strings.Split(body, " ")
	for i, w := range words {
		if _, found := profane[strings.ToLower(w)]; found {
			words[i] = "****"
		}
	}
	return strings.Join(words, " ")
}
