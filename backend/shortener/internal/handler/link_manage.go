package handler

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NikitaPash/url-shortener/internal/middleware"
	"github.com/NikitaPash/url-shortener/internal/service"
	"github.com/NikitaPash/url-shortener/internal/storage/postgres"
)

type LinkManageHandler struct {
	shortener *service.ShortenerService
}

func NewLinkManageHandler(s *service.ShortenerService) *LinkManageHandler {
	return &LinkManageHandler{shortener: s}
}

type patchLinkRequest struct {
	IsActive bool `json:"is_active"`
}

func (h *LinkManageHandler) SetActive(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID := middleware.GetUserID(r.Context())

	var req patchLinkRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if err := h.shortener.SetLinkActive(r.Context(), id, userID, req.IsActive); err != nil {
		switch {
		case errors.Is(err, postgres.ErrLinkNotFound), errors.Is(err, service.ErrForbidden):
			writeError(w, http.StatusNotFound, "link not found")
		default:
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *LinkManageHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID := middleware.GetUserID(r.Context())

	if err := h.shortener.DeleteLink(r.Context(), id, userID); err != nil {
		switch {
		case errors.Is(err, postgres.ErrLinkNotFound), errors.Is(err, service.ErrForbidden):
			writeError(w, http.StatusNotFound, "link not found")
		default:
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
