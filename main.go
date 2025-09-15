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
	 "errors"
	 "github.com/bootdotdev/learn-http-servers/internal/auth"
)

type apiConfig struct {
	fileserverHits atomic.Int32
    db *database.Queries	
	platform string
}

type chirpRequest struct {
	Body string `json:"body"`
	User_id string `json:"user_id"`
}

type loginRequest struct {
	Password string `json:"password"`
	Email string `json:"email"`
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
	Password string     `json:"password"`
	Email     string    `json:"email"`
}

type chirpResponse struct {
    ID        uuid.UUID `json:"id"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
    Body      string    `json:"body"`
    UserID    uuid.UUID `json:"user_id"`
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
//	mux.HandleFunc("POST /api/validate_chirp", apiCfg.handlerValidateChirp)
	
	// 6. Get user
	mux.HandleFunc("POST /api/users", apiCfg.handlerUsersCreate)

	// 7. Handle create
	mux.HandleFunc("POST /api/chirps", apiCfg.handlerChirps)

	// 8. Handle consult all
	mux.HandleFunc("GET /api/chirps", apiCfg.handlerGetChirps)

	// 9. Get chirp by id
	mux.HandleFunc("GET /api/chirps/{chirpID}", apiCfg.handlerGetChirpById)

	// 10. Login
	mux.HandleFunc("POST /api/login", apiCfg.handlerLogin)
	
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

	if strings.TrimSpace(req.Password) == ""{
		respondWithError(w, http.StatusBadRequest, "password required")
        return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "error hashing password")
        return
	}

	params := database.CreateUserParams{
		HashedPassword:  hash ,
		Email: req.Email, 
	}

    dbUser, err := cfg.db.CreateUser(r.Context(), params)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "could not create user")
        return
    }

	res = User{ID: dbUser.ID, 
			    CreatedAt: dbUser.CreatedAt, 
				UpdatedAt: dbUser.UpdatedAt, 
				Email: dbUser.Email }

	respondWithJSON(w, http.StatusCreated, res)

}


// Handler para /api/chirps
func (cfg *apiConfig) handlerChirps(w http.ResponseWriter, r *http.Request) {
	var req chirpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Something went wrong")
		return
	}

	if len(req.Body) > 140 {
		respondWithError(w, http.StatusBadRequest, "Chirp is too long")
		return
	}

	uid, err := uuid.Parse(req.User_id)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid user_id")
		return
	}

	params := database.CreateChirpParams{
		Body:   cleanChirp(req.Body),
		UserID: uid, 
	}

	dbChirp, err := cfg.db.CreateChirp(r.Context(), params)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "could not create chirp")
        return
    }

	// after dbChirp is created:
	resp := chirpResponse{
		ID: dbChirp.ID,
		CreatedAt: dbChirp.CreatedAt,
		UpdatedAt: dbChirp.UpdatedAt,
		Body: dbChirp.Body,
		UserID: dbChirp.UserID,
	}


	respondWithJSON(w, http.StatusCreated, resp)
}

func (cfg *apiConfig) handlerGetChirps(w http.ResponseWriter, r *http.Request) {

	dbChirps, err := cfg.db.GetChirps(r.Context())
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "could get chirps")
        return
    }

	out := make([]chirpResponse, 0, len(dbChirps))
		for _, c := range dbChirps {
			out = append(out, chirpResponse{
				ID:        c.ID,
				CreatedAt: c.CreatedAt,
				UpdatedAt: c.UpdatedAt,
				Body:      c.Body,
				UserID:    c.UserID,
			})
		}


	respondWithJSON(w, http.StatusOK, out)
}

func (cfg *apiConfig) handlerGetChirpById(w http.ResponseWriter, r *http.Request) {

	chirpID := r.PathValue("chirpID")
	if len(chirpID) > 0 {
		uid, err := uuid.Parse(chirpID)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "invalid chirpID")
			return
		}

		dbChirp, err := cfg.db.GetChirp(r.Context(), uid)
		if err != nil { 
			if errors.Is(err, sql.ErrNoRows) { // si el error es que no encontro nada
				respondWithError(w, http.StatusNotFound, "chirp not found")
				return
			}
			// If it's an error, but not sql.ErrNoRows, then it's a different kind of server error
			respondWithError(w, http.StatusInternalServerError, "could not get chirp")
			return
		}

		// after dbChirp is retrieved:
		resp := chirpResponse{
			ID: dbChirp.ID,
			CreatedAt: dbChirp.CreatedAt,
			UpdatedAt: dbChirp.UpdatedAt,
			Body: dbChirp.Body,
			UserID: dbChirp.UserID,
		}

		respondWithJSON(w, http.StatusOK, resp)
	} 
}

func (cfg *apiConfig) handlerLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	var res User
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Something went wrong")
		return
	}

	dbUser, err := cfg.db.GetUserEmail(r.Context(), req.Email)
	if err != nil { 
		if errors.Is(err, sql.ErrNoRows) { // si el error es que no encontro nada
			respondWithError(w, http.StatusUnauthorized, "Incorrect email or password")
			return
		}
		// If it's an error, but not sql.ErrNoRows, then it's a different kind of server error
		respondWithError(w, http.StatusInternalServerError, "Error retrieving user")
		return
	}	


	err = auth.CheckPasswordHash(req.Password, dbUser.HashedPassword)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Incorrect email or password")
        return
	}

	res = User{ID: dbUser.ID, 
			   CreatedAt: dbUser.CreatedAt, 
			   UpdatedAt: dbUser.UpdatedAt, 
			   Email: dbUser.Email}

	respondWithJSON(w, http.StatusOK, res)
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
