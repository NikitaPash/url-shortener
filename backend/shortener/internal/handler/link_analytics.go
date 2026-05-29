package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/NikitaPash/url-shortener/internal/analytics"
	"github.com/NikitaPash/url-shortener/internal/middleware"
	"github.com/NikitaPash/url-shortener/internal/service"
	"github.com/NikitaPash/url-shortener/internal/storage/postgres"
)

type LinkAnalyticsHandler struct {
	shortener *service.ShortenerService
	reader    *analytics.ClickHouseReader
}

func NewLinkAnalyticsHandler(s *service.ShortenerService, r *analytics.ClickHouseReader) *LinkAnalyticsHandler {
	return &LinkAnalyticsHandler{shortener: s, reader: r}
}

func (h *LinkAnalyticsHandler) GetLinkAnalytics(w http.ResponseWriter, r *http.Request) {
	if h.reader == nil {
		writeError(w, http.StatusServiceUnavailable, "analytics not configured")
		return
	}

	shortID := chi.URLParam(r, "id")
	userID := middleware.GetUserID(r.Context())

	link, err := h.shortener.GetLink(r.Context(), shortID)
	if err != nil {
		if errors.Is(err, postgres.ErrLinkNotFound) {
			writeError(w, http.StatusNotFound, "link not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if link.UserID != userID {
		writeError(w, http.StatusNotFound, "link not found")
		return
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	var from, to time.Time

	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	if fromStr != "" && toStr != "" {
		var err1, err2 error
		from, err1 = time.Parse("2006-01-02", fromStr)
		to, err2 = time.Parse("2006-01-02", toStr)
		if err1 != nil || err2 != nil || !from.Before(to) {
			writeError(w, http.StatusBadRequest, "invalid date range: from must be a valid date before to")
			return
		}
		to = to.Add(24 * time.Hour) // inclusive end of to-date
		if days := int(to.Sub(from).Hours() / 24); days > 365 {
			writeError(w, http.StatusBadRequest, "date range cannot exceed 365 days")
			return
		}
	} else {
		period := 7
		if p := r.URL.Query().Get("period"); p != "" {
			if v, err := strconv.Atoi(p); err == nil && v >= 1 && v <= 365 {
				period = v
			}
		}
		from = today.AddDate(0, 0, -period)
		to = today.Add(24 * time.Hour) // end of today — includes clicks made so far today
	}

	excludeBots := r.URL.Query().Get("exclude_bots") == "true"

	stats, err := h.reader.GetLinkStats(r.Context(), shortID, from, to, excludeBots)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "analytics unavailable")
		return
	}

	writeJSON(w, http.StatusOK, stats)
}
