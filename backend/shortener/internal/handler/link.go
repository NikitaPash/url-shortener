package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/NikitaPash/url-shortener/internal/middleware"
	"github.com/NikitaPash/url-shortener/internal/service"
	"github.com/NikitaPash/url-shortener/internal/storage/postgres"
)

const (
	defaultListLimit = 20
	maxListLimit     = 100
)

type LinkHandler struct {
	shortener *service.ShortenerService
	baseURL   string
}

func NewLinkHandler(s *service.ShortenerService, baseURL string) *LinkHandler {
	return &LinkHandler{
		shortener: s,
		baseURL:   baseURL,
	}
}

type shortenRequest struct {
	// http_url restricts the scheme to http/https; max caps stored URL length.
	URL         string `json:"url"          validate:"required,http_url,max=2048"`
	CustomAlias string `json:"custom_alias"` // optional; empty = auto-generate
}

func (h *LinkHandler) Shorten(w http.ResponseWriter, r *http.Request) {
	var req shortenRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	userID := middleware.GetUserID(r.Context())

	link, err := h.shortener.Shorten(r.Context(), userID, req.URL, nil, req.CustomAlias)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidAlias):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, postgres.ErrIDTaken):
			writeError(w, http.StatusConflict, "alias already taken")
		default:
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":           link.ID,
		"short_url":    h.baseURL + "/" + link.ID,
		"original_url": link.OriginalURL,
		"created_at":   link.CreatedAt,
	})
}

func (h *LinkHandler) ListLinks(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())

	limit := parseIntParam(r, "limit", defaultListLimit)
	if limit < 1 {
		limit = 1
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	offset := parseIntParam(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}

	links, total, err := h.shortener.ListLinks(r.Context(), userID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	type linkItem struct {
		ID          string     `json:"id"`
		ShortURL    string     `json:"short_url"`
		OriginalURL string     `json:"original_url"`
		IsActive    bool       `json:"is_active"`
		CreatedAt   time.Time  `json:"created_at"`
		ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	}

	items := make([]linkItem, 0, len(links))
	for _, l := range links {
		items = append(items, linkItem{
			ID:          l.ID,
			ShortURL:    h.baseURL + "/" + l.ID,
			OriginalURL: l.OriginalURL,
			IsActive:    l.IsActive,
			CreatedAt:   l.CreatedAt,
			ExpiresAt:   l.ExpiresAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"links":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func parseIntParam(r *http.Request, name string, defaultVal int) int {
	s := r.URL.Query().Get(name)
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return v
}
