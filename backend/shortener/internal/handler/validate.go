package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-playground/validator/v10"
)

// maxBodyBytes caps request body size to protect against memory-exhaustion from
// oversized payloads. 1 MiB is far above any legitimate JSON request here.
const maxBodyBytes = 1 << 20

// validate is shared across handlers — it caches struct reflection metadata, so
// a single instance should be reused rather than constructed per handler.
var validate = validator.New()

// decodeJSON limits the body size, decodes JSON into dst, and validates it.
// On failure it writes the error response and returns false. Validation details
// are logged server-side rather than returned, to avoid leaking internals.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}

	if err := validate.Struct(dst); err != nil {
		slog.Debug("request validation failed", "error", err)
		writeError(w, http.StatusBadRequest, "invalid request parameters")
		return false
	}

	return true
}
