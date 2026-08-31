package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	contract "github.com/tradelab/linkedin-profile-api/api"
	"github.com/tradelab/linkedin-profile-api/internal/linkedin"
	"github.com/tradelab/linkedin-profile-api/internal/profile"
	"github.com/tradelab/linkedin-profile-api/internal/service"
	frontend "github.com/tradelab/linkedin-profile-api/web"
)

const maxRequestBody = 16 << 10

type ProfileGetter interface {
	Get(context.Context, string, service.Session) (profile.Result, error)
}

type Options struct {
	RateLimitRPM   int
	RateLimitBurst int
	RequestTimeout time.Duration
}

type Server struct {
	profiles ProfileGetter
	logger   *slog.Logger
	options  Options
}

func NewHandler(profiles ProfileGetter, logger *slog.Logger, options Options) http.Handler {
	server := &Server{profiles: profiles, logger: logger, options: options}
	mux := http.NewServeMux()

	limiter := newClientLimiter(options.RateLimitRPM, options.RateLimitBurst)
	profileHandler := http.Handler(http.HandlerFunc(server.handleProfile))
	profileHandler = limiter.middleware(profileHandler)
	profileHandler = withTimeout(options.RequestTimeout, profileHandler)

	mux.Handle("POST /api/v1/profiles", profileHandler)
	mux.HandleFunc("GET /healthz", server.handleHealth)
	mux.HandleFunc("GET /openapi.yaml", server.handleOpenAPI)
	mux.Handle("GET /", http.FileServer(http.FS(frontend.Assets)))

	var handler http.Handler = mux
	handler = securityHeaders(handler)
	handler = accessLog(logger, handler)
	handler = recoverer(logger, handler)
	handler = requestID(handler)
	return handler
}

func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	defer r.Body.Close()

	var request struct {
		URL     string `json:"url"`
		Session struct {
			LIAT       string `json:"li_at"`
			JSESSIONID string `json:"jsession_id"`
		} `json:"linkedin_session"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Request body must be JSON containing a url field.", requestIDFrom(r.Context()))
		return
	}
	if err := ensureJSONEnd(decoder); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Request body must contain one JSON object.", requestIDFrom(r.Context()))
		return
	}
	if strings.TrimSpace(request.URL) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "The url field is required.", requestIDFrom(r.Context()))
		return
	}

	result, err := s.profiles.Get(r.Context(), request.URL, service.Session{
		LIAT:       request.Session.LIAT,
		JSESSIONID: request.Session.JSESSIONID,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	result.Meta.RequestID = requestIDFrom(r.Context())
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	body, err := contract.Files.ReadFile("openapi.yaml")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "API contract is unavailable.", requestIDFrom(r.Context()))
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	requestID := requestIDFrom(r.Context())
	switch {
	case errors.Is(err, service.ErrInvalidURL):
		writeError(w, http.StatusBadRequest, "invalid_profile_url", err.Error(), requestID)
	case errors.Is(err, service.ErrInvalidSession):
		writeError(w, http.StatusBadRequest, "invalid_linkedin_session", err.Error(), requestID)
	case errors.Is(err, service.ErrBusy):
		w.Header().Set("Retry-After", "10")
		writeError(w, http.StatusTooManyRequests, "at_capacity", "The profile service is busy. Try again shortly.", requestID)
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "timeout", "The profile request timed out.", requestID)
	case errors.Is(err, context.Canceled):
		writeError(w, http.StatusGatewayTimeout, "canceled", "The profile request was canceled.", requestID)
	default:
		switch linkedin.KindOf(err) {
		case linkedin.ErrAuthentication:
			writeError(w, http.StatusServiceUnavailable, "linkedin_session_expired", "The LinkedIn session is unavailable or expired.", requestID)
		case linkedin.ErrForbidden:
			writeError(w, http.StatusForbidden, "profile_unavailable", "The LinkedIn account cannot view this profile.", requestID)
		case linkedin.ErrNotFound:
			writeError(w, http.StatusNotFound, "profile_not_found", "The LinkedIn profile was not found.", requestID)
		case linkedin.ErrRateLimited, linkedin.ErrChallenge:
			w.Header().Set("Retry-After", "900")
			writeError(w, http.StatusServiceUnavailable, "linkedin_cooldown", "LinkedIn temporarily rejected profile requests.", requestID)
		case linkedin.ErrSchema:
			s.logger.Error("LinkedIn response schema error", "request_id", requestID, "error", err)
			writeError(w, http.StatusBadGateway, "upstream_changed", "LinkedIn returned an unsupported response.", requestID)
		default:
			s.logger.Error("LinkedIn upstream error", "request_id", requestID, "error", err)
			writeError(w, http.StatusBadGateway, "upstream_error", "LinkedIn could not complete the request.", requestID)
		}
	}
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("additional JSON value")
	}
	return err
}

func writeError(w http.ResponseWriter, status int, code, message, requestID string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":       code,
			"message":    message,
			"request_id": requestID,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
