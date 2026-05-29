package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/NikitaPash/url-shortener/internal/event"
	"github.com/NikitaPash/url-shortener/internal/geo"
	"github.com/NikitaPash/url-shortener/internal/service"
	"github.com/NikitaPash/url-shortener/internal/storage/postgres"
	"github.com/NikitaPash/url-shortener/internal/telemetry"
)

// LinkResolver is the subset of ShortenerService used by RedirectHandler.
type LinkResolver interface {
	Resolve(ctx context.Context, id string) (*service.ResolvedLink, error)
}

type RedirectHandler struct {
	shortener LinkResolver
	producer  *event.Producer
	geoIP     *geo.Resolver
	metrics   *telemetry.Metrics
}

func NewRedirectHandler(
	s LinkResolver,
	p *event.Producer,
	g *geo.Resolver,
	metrics *telemetry.Metrics,
) *RedirectHandler {
	if metrics == nil {
		metrics = telemetry.NewNoopMetrics()
	}
	return &RedirectHandler{shortener: s, producer: p, geoIP: g, metrics: metrics}
}

func (h *RedirectHandler) Redirect(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing link ID")
		return
	}

	resolved, err := h.shortener.Resolve(r.Context(), id)
	if err != nil {
		if errors.Is(err, postgres.ErrLinkNotFound) {
			http.Redirect(w, r, "/app/not-found", http.StatusFound)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	clickEvent := event.ClickEvent{
		Timestamp: time.Now().UTC(),
		ShortID:   id,
		UserID:    resolved.UserID,
		IP:        r.RemoteAddr,
		UserAgent: r.Header.Get("User-Agent"),
		Referrer:  r.Header.Get("Referer"),
		Country:   h.geoIP.Country(r.RemoteAddr),
	}

	// WithoutCancel detaches from the request's cancellation (which fires when the
	// response is written and would abort the in-flight Kafka write) while keeping
	// the trace context, so the publish span still links to the request's trace.
	h.producer.PublishClickAsync(context.WithoutCancel(r.Context()), clickEvent)

	h.metrics.Redirect.Add(r.Context(), 1)
	http.Redirect(w, r, resolved.OriginalURL, http.StatusFound)
}
