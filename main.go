package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

type AskRequest struct {
	Message string `json:"message"`
}

type AskResponse struct {
	Reply string `json:"reply,omitempty"`
	Error string `json:"error,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "barista-bot"})
	})

	mux.HandleFunc("POST /ask", func(w http.ResponseWriter, r *http.Request) {
		var req AskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
			writeJSON(w, http.StatusBadRequest, AskResponse{Error: "field 'message' required"})
			return
		}

		reply, err := runAgent(r.Context(), req.Message)
		if err != nil {
			log.Printf("agent error: %v", err)
			writeJSON(w, http.StatusInternalServerError, AskResponse{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, AskResponse{Reply: reply})
	})

	log.Printf("barista-bot listening on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
