package settings

import (
	"encoding/json"
	"net/http"

	"open-ima/internal/httpx"
)

func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, s.Get())
	})
	mux.HandleFunc("PUT /api/settings", func(w http.ResponseWriter, r *http.Request) {
		var values Values
		if err := json.NewDecoder(r.Body).Decode(&values); err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid settings payload")
			return
		}
		updated, err := s.Update(r.Context(), values)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		httpx.JSON(w, http.StatusOK, updated)
	})
}
