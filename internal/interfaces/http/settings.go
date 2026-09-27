package httpapi

import (
	"encoding/json"
	"net/http"

	settingsapp "open-ima/internal/application/settings"
	settingsdom "open-ima/internal/domain/settings"
)

type settingsHandler struct{ service *settingsapp.Service }

func (h *settingsHandler) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, h.service.Get())
	})
	mux.HandleFunc("PUT /api/settings", func(w http.ResponseWriter, r *http.Request) {
		var values settingsdom.Values
		if err := json.NewDecoder(r.Body).Decode(&values); err != nil {
			writeError(w, http.StatusBadRequest, "invalid settings payload")
			return
		}
		updated, err := h.service.Update(r.Context(), values)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, updated)
	})
}
