package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/argon2"

	imagehub "imagehub/internal"
)

//go:embed 001_init.sql
var migrationSQL string

type server struct {
	cfg             imagehub.Config
	db              *pgxpool.Pool
	redis           *redis.Client
	storage         imagehub.Storage
	localStorage    imagehub.Storage
	telegramStorage imagehub.Storage
	s3Storage       imagehub.Storage
	storageChannels map[string]imagehub.Storage
	storageBackends map[string]string
	defaultChannel  string
	log             *slog.Logger
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type user struct{ ID, Email, Role string }

type contextKey string

const userKey contextKey = "user"

func main() {
	cfg := imagehub.LoadConfig()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	s := &server{cfg: cfg, log: logger, storageChannels: map[string]imagehub.Storage{}, storageBackends: map[string]string{}, defaultChannel: "local"}
	local, err := imagehub.NewLocalStorage(cfg.StorageDir)
	if err != nil {
		logger.Error("storage init failed", "error", err)
		os.Exit(1)
	}
	s.storage = local
	s.localStorage = local
	if cfg.TelegramToken != "" && cfg.TelegramChatID != "" {
		s.telegramStorage = imagehub.NewTelegramStorage(cfg.TelegramToken, cfg.TelegramChatID)
		s.storage = s.telegramStorage
		logger.Info("telegram storage enabled")
	}
	ctx := context.Background()
	if cfg.DatabaseURL != "" {
		s.db, err = pgxpool.New(ctx, cfg.DatabaseURL)
		if err != nil {
			logger.Error("postgres init failed", "error", err)
			os.Exit(1)
		}
		if err = s.db.Ping(ctx); err != nil {
			logger.Error("postgres ping failed", "error", err)
			os.Exit(1)
		}
		if _, err = s.db.Exec(ctx, migrationSQL); err != nil {
			logger.Error("migration failed", "error", err)
			os.Exit(1)
		}
		if err = s.bootstrapAdmin(ctx); err != nil {
			logger.Error("admin bootstrap failed", "error", err)
			os.Exit(1)
		}
		if err = s.applyStorageSettings(ctx); err != nil {
			logger.Warn("configured storage unavailable; using local storage", "error", err)
		}
	}
	if cfg.RedisURL != "" {
		if opts, e := redis.ParseURL(cfg.RedisURL); e == nil {
			opts.DialTimeout = 2 * time.Second
			opts.ReadTimeout = 2 * time.Second
			opts.WriteTimeout = 2 * time.Second
			s.redis = redis.NewClient(opts)
			if e = s.redis.Ping(ctx).Err(); e != nil {
				logger.Warn("redis unavailable", "error", e)
			}
		}
	}
	if s.db != nil {
		go s.mediaWorker(ctx)
	}
	mux := http.NewServeMux()
	s.routes(mux)
	h := &http.Server{Addr: cfg.Addr, Handler: s.withMiddleware(mux), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 90 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 120 * time.Second}
	logger.Info("imagehub started", "addr", cfg.Addr)
	if err := h.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func (s *server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)
	mux.HandleFunc("GET /api/v1/site/config", s.publicSiteConfig)
	mux.HandleFunc("POST /api/v1/auth/register", s.register)
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("POST /api/v1/auth/logout", s.logout)
	mux.HandleFunc("GET /api/v1/auth/me", s.requireAuth(s.me))
	mux.HandleFunc("GET /api/v1/dashboard", s.requireAuth(s.dashboard))
	mux.HandleFunc("GET /api/v1/auth/verify-email", s.verifyEmail)
	mux.HandleFunc("POST /api/v1/auth/forgot-password", s.forgotPassword)
	mux.HandleFunc("POST /api/v1/auth/reset-password", s.resetPassword)
	mux.HandleFunc("GET /api/v1/auth/oidc/{id}/start", s.oidcStart)
	mux.HandleFunc("GET /api/v1/auth/oidc/{id}/callback", s.oidcCallback)
	mux.HandleFunc("GET /api/v1/auth/oidc/providers", s.publicOIDCProviders)
	mux.HandleFunc("GET /api/v1/admin/settings", s.requireAdmin(s.getSettings))
	mux.HandleFunc("PATCH /api/v1/admin/settings/{key}", s.requireAdmin(s.updateSetting))
	mux.HandleFunc("POST /api/v1/admin/storage/test", s.requireAdmin(s.testStorage))
	mux.HandleFunc("POST /api/v1/admin/email/test", s.requireAdmin(s.testEmail))
	mux.HandleFunc("GET /api/v1/admin/feature-status", s.requireAdmin(s.featureStatus))
	mux.HandleFunc("PATCH /api/v1/admin/users/{id}/policy", s.requireAdmin(s.updateUserPolicy))
	mux.HandleFunc("PATCH /api/v1/admin/users/{id}", s.requireAdmin(s.updateAdminUser))
	mux.HandleFunc("GET /api/v1/admin/users", s.requireAdmin(s.listAdminUsers))
	mux.HandleFunc("GET /api/v1/admin/images", s.requireAdmin(s.listAdminImages))
	mux.HandleFunc("DELETE /api/v1/admin/images/{id}", s.requireAdmin(s.forceDeleteImage))
	mux.HandleFunc("GET /api/v1/teams", s.requireAuth(s.listTeams))
	mux.HandleFunc("GET /api/v1/storage/channels", s.requireAuth(s.listStorageChannels))
	mux.HandleFunc("POST /api/v1/teams", s.requireAuth(s.createTeam))
	mux.HandleFunc("GET /api/v1/teams/{id}/members", s.requireAuth(s.listTeamMembers))
	mux.HandleFunc("GET /api/v1/teams/{id}/invitations", s.requireAuth(s.listTeamInvitations))
	mux.HandleFunc("POST /api/v1/teams/{id}/invitations", s.requireAuth(s.createTeamInvitation))
	mux.HandleFunc("POST /api/v1/team-invitations/{token}/accept", s.requireAuth(s.acceptTeamInvitation))
	mux.HandleFunc("GET /api/v1/billing/plans", s.requireAuth(s.listPlans))
	mux.HandleFunc("GET /api/v1/admin/plans", s.requireAdmin(s.listAdminPlans))
	mux.HandleFunc("PATCH /api/v1/admin/plans/{code}", s.requireAdmin(s.updateAdminPlan))
	mux.HandleFunc("GET /api/v1/teams/{id}/billing/subscription", s.requireAuth(s.getSubscription))
	mux.HandleFunc("PUT /api/v1/teams/{id}/billing/subscription", s.requireAuth(s.upsertSubscription))
	mux.HandleFunc("POST /api/v1/teams/{id}/billing/checkout", s.requireAuth(s.createCheckoutSession))
	mux.HandleFunc("POST /api/v1/billing/webhook", s.billingWebhook)
	mux.HandleFunc("GET /api/v1/domains", s.requireAuth(s.listDomains))
	mux.HandleFunc("POST /api/v1/domains", s.requireAuth(s.createDomain))
	mux.HandleFunc("POST /api/v1/domains/{id}/verify", s.requireAuth(s.verifyDomain))
	mux.HandleFunc("DELETE /api/v1/domains/{id}", s.requireAuth(s.deleteDomain))
	mux.HandleFunc("GET /api/v1/domains/tls-authorize", s.authorizeDomainTLS)
	mux.HandleFunc("GET /api/v1/oidc/providers", s.requireAdmin(s.listOIDCProviders))
	mux.HandleFunc("POST /api/v1/oidc/providers", s.requireAdmin(s.createOIDCProvider))
	mux.HandleFunc("PATCH /api/v1/oidc/providers/{id}", s.requireAdmin(s.updateOIDCProvider))
	mux.HandleFunc("DELETE /api/v1/oidc/providers/{id}", s.requireAdmin(s.deleteOIDCProvider))
	mux.HandleFunc("POST /api/v1/images/upload", s.requireAuth(s.upload))
	mux.HandleFunc("POST /api/v1/public/upload", s.guestUpload)
	mux.HandleFunc("POST /api/v1/images/import-url", s.requireAuth(s.importURL))
	mux.HandleFunc("GET /api/v1/images", s.requireAuth(s.listImages))
	mux.HandleFunc("PATCH /api/v1/images/{id}", s.requireAuth(s.updateImage))
	mux.HandleFunc("DELETE /api/v1/images/{id}", s.requireAuth(s.deleteImage))
	mux.HandleFunc("GET /media/{id}", s.media)
	mux.HandleFunc("GET /media/{id}/thumbnail", s.mediaThumbnail)
	mux.HandleFunc("GET /s/{code}", s.shortLink)
	mux.HandleFunc("GET /api/v1/public/config", s.publicConfig)
	mux.HandleFunc("GET /", s.frontend)
}

// publicSiteConfig exposes only non-sensitive branding values so the login and
// public pages can use the administrator's site name, logo and favicon.
func (s *server) publicSiteConfig(w http.ResponseWriter, r *http.Request) {
	value := s.settings(r.Context(), "site")
	result := map[string]any{
		"site_name":        strings.TrimSpace(fmt.Sprint(value["site_name"])),
		"logo_url":         strings.TrimSpace(fmt.Sprint(value["logo_url"])),
		"favicon_url":      strings.TrimSpace(fmt.Sprint(value["favicon_url"])),
		"default_language": strings.TrimSpace(fmt.Sprint(value["default_language"])),
	}
	if result["site_name"] == "" || result["site_name"] == "<nil>" {
		result["site_name"] = "ImageHub"
	}
	if result["default_language"] == "" || result["default_language"] == "<nil>" {
		result["default_language"] = "en-US"
	}
	if result["logo_url"] == "<nil>" {
		result["logo_url"] = ""
	}
	if result["favicon_url"] == "<nil>" {
		result["favicon_url"] = ""
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, result)
}

func (s *server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if limited := s.rateLimited(r); limited {
			writeErr(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests")
			return
		}
		w.Header().Set("X-Request-ID", requestID())
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			if origin := r.Header.Get("Origin"); origin != "" && !s.allowedOriginForRequest(r, origin) && r.Method != http.MethodOptions && r.Method != http.MethodGet {
				writeErr(w, http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "request origin is not allowed")
				return
			} else if origin != "" && s.allowedOriginForRequest(r, origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (s *server) rateLimited(r *http.Request) bool {
	if s.redis == nil {
		return false
	}
	limit := 120
	if strings.Contains(r.URL.Path, "/auth/") {
		limit = 20
	}
	if strings.Contains(r.URL.Path, "/images/") {
		limit = 60
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = "unknown"
	}
	key := "rate:" + ip + ":" + r.URL.Path + ":" + strconv.FormatInt(time.Now().Unix()/60, 10)
	count, err := s.redis.Incr(r.Context(), key).Result()
	if err != nil {
		return false
	}
	if count == 1 {
		_ = s.redis.Expire(r.Context(), key, 70*time.Second).Err()
	}
	return count > int64(limit)
}
func (s *server) allowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	if strings.TrimSpace(s.cfg.AllowedOrigins) != "" {
		for _, candidate := range strings.Split(s.cfg.AllowedOrigins, ",") {
			if strings.TrimSpace(candidate) == origin {
				return true
			}
		}
		return false
	}
	public, err := url.Parse(s.cfg.PublicURL)
	return err == nil && public.Scheme+"://"+public.Host == origin
}

// allowedOriginForRequest always permits a request coming from the same origin
// that served the page. This matters when PUBLIC_URL keeps its production
// default while the local compose file publishes the app on another port
// (for example http://localhost:18080). Same-origin requests do not need to be
// listed in CORS_ALLOWED_ORIGINS and should never be rejected as cross-origin.
func (s *server) allowedOriginForRequest(r *http.Request, origin string) bool {
	parsed, err := url.Parse(origin)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if parsed.Scheme == scheme && parsed.Host == r.Host {
			return true
		}
		// Local previews are often opened on :18080 while an API proxy points
		// at :8080. When no explicit allowlist is configured, permit loopback
		// ports to share the session cookie across those local origins.
		if strings.TrimSpace(s.cfg.AllowedOrigins) == "" && parsed.Scheme == scheme && isLoopbackHost(parsed.Hostname()) && isLoopbackHost(requestHost(r.Host)) {
			return true
		}
	}
	return s.allowedOrigin(origin)
}

func requestHost(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return host
	}
	return hostport
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
func requestID() string { b := make([]byte, 12); _, _ = rand.Read(b); return hex.EncodeToString(b) }

func (s *server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
func (s *server) readyz(w http.ResponseWriter, r *http.Request) {
	if s.db == nil || s.db.Ping(r.Context()) != nil {
		writeJSON(w, http.StatusServiceUnavailable, apiError{"DATABASE_UNAVAILABLE", "postgres is not ready"})
		return
	}
	if s.redis == nil || s.redis.Ping(r.Context()).Err() != nil {
		writeJSON(w, http.StatusServiceUnavailable, apiError{"REDIS_UNAVAILABLE", "redis is not ready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *server) register(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeErr(w, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", "database is not configured")
		return
	}
	settings := s.settings(r.Context(), "registration")
	if b, ok := settings["enabled"].(bool); ok && !b {
		writeErr(w, http.StatusForbidden, "REGISTRATION_DISABLED", "registration is disabled")
		return
	}
	var in struct {
		Email       string `json:"email"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if !decodeJSON(r, &in) || !strings.Contains(in.Email, "@") || len(in.Password) < 10 || (in.Username != "" && !regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,32}$`).MatchString(in.Username)) {
		writeErr(w, http.StatusBadRequest, "INVALID_INPUT", "email, optional username, and a password of at least 10 characters are required")
		return
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		writeErr(w, 500, "HASH_FAILED", "could not create account")
		return
	}
	status := "active"
	if b, ok := settings["require_email_verification"].(bool); ok && b {
		status = "pending"
	}
	var id string
	err = s.db.QueryRow(r.Context(), `INSERT INTO users(email,username,password_hash,display_name,status) VALUES($1,NULLIF($2,''),$3,$4,$5) RETURNING id`, strings.ToLower(strings.TrimSpace(in.Email)), strings.ToLower(strings.TrimSpace(in.Username)), hash, in.DisplayName, status).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") {
			writeErr(w, http.StatusConflict, "EMAIL_EXISTS", "email is already registered")
			return
		}
		writeErr(w, 500, "REGISTER_FAILED", "could not create account")
		return
	}
	result := map[string]any{"id": id, "status": status, "email_verification_required": status == "pending"}
	if status == "pending" {
		_, issueErr := s.issueEmailToken(r.Context(), id, strings.ToLower(strings.TrimSpace(in.Email)), "verify")
		if issueErr != nil {
			s.log.Warn("verification email could not be sent", "user_id", id, "error", issueErr)
		}
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeErr(w, 503, "DATABASE_UNAVAILABLE", "database is not configured")
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Remember bool   `json:"remember"`
	}
	if !decodeJSON(r, &in) {
		writeErr(w, 400, "INVALID_INPUT", "invalid request")
		return
	}
	var id, stored, role, status string
	err := s.db.QueryRow(r.Context(), `SELECT id,password_hash,role,status FROM users WHERE lower(email)=lower($1) OR lower(COALESCE(username,''))=lower($1) LIMIT 1`, strings.TrimSpace(in.Email)).Scan(&id, &stored, &role, &status)
	if err != nil || stored == "" || !verifyPassword(in.Password, stored) {
		writeErr(w, 401, "INVALID_CREDENTIALS", "email or password is incorrect")
		return
	}
	if status != "active" {
		if status == "pending" {
			writeErr(w, 403, "EMAIL_NOT_VERIFIED", "email verification is required before login")
			return
		}
		writeErr(w, 403, "ACCOUNT_DISABLED", "account is not active")
		return
	}
	token := requestID() + requestID()
	if s.redis == nil {
		writeErr(w, 503, "REDIS_UNAVAILABLE", "session service is unavailable")
		return
	}
	if err = s.redis.Set(r.Context(), "session:"+token, id, s.cfg.SessionTTL).Err(); err != nil {
		writeErr(w, 503, "SESSION_FAILED", "could not create session")
		return
	}
	maxAge := 0
	if in.Remember {
		maxAge = int(s.cfg.SessionTTL.Seconds())
	}
	http.SetCookie(w, &http.Cookie{Name: "ih_session", Value: token, Path: "/", HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
	writeJSON(w, 200, map[string]any{"user_id": id, "role": role})
}
func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("ih_session"); err == nil && s.redis != nil {
		_ = s.redis.Del(r.Context(), "session:"+c.Value).Err()
	}
	http.SetCookie(w, &http.Cookie{Name: "ih_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *server) me(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userKey).(user)
	var email, username, name, status string
	if err := s.db.QueryRow(r.Context(), `SELECT email,COALESCE(username,''),display_name,status FROM users WHERE id=$1`, u.ID).Scan(&email, &username, &name, &status); err != nil {
		writeErr(w, 500, "USER_LOOKUP_FAILED", "could not load user")
		return
	}
	writeJSON(w, 200, map[string]any{"id": u.ID, "email": email, "username": username, "display_name": name, "role": u.Role, "status": status})
}
func (s *server) dashboard(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userKey).(user)
	var images, teams int64
	var used, quota int64
	_ = s.db.QueryRow(r.Context(), `SELECT count(*),COALESCE(sum(size_bytes),0) FROM images WHERE (owner_id=$1 OR team_id IN (SELECT team_id FROM team_members WHERE user_id=$1)) AND deleted_at IS NULL`, u.ID).Scan(&images, &used)
	_ = s.db.QueryRow(r.Context(), `SELECT count(*) FROM team_members WHERE user_id=$1`, u.ID).Scan(&teams)
	_ = s.db.QueryRow(r.Context(), `SELECT quota_bytes FROM users WHERE id=$1`, u.ID).Scan(&quota)
	result := map[string]any{"images": images, "used_bytes": used, "quota_bytes": quota, "teams": teams}
	if u.Role == "admin" {
		var totalUsers, activeUsers, pendingUsers, disabledUsers, newUsers, totalFiles, physicalFiles, todayUploads, guestUploads, userUploads, imageCount, videoCount int64
		var todayBytes int64
		_ = s.db.QueryRow(r.Context(), `SELECT count(*),count(*) FILTER (WHERE status='active'),count(*) FILTER (WHERE status='pending'),count(*) FILTER (WHERE status='disabled') FROM users`).Scan(&totalUsers, &activeUsers, &pendingUsers, &disabledUsers)
		_ = s.db.QueryRow(r.Context(), `SELECT count(*) FROM users WHERE created_at >= CURRENT_DATE`).Scan(&newUsers)
		_ = s.db.QueryRow(r.Context(), `SELECT count(*) FROM images WHERE deleted_at IS NULL`).Scan(&totalFiles)
		_ = s.db.QueryRow(r.Context(), `SELECT count(DISTINCT object_key) FROM images WHERE deleted_at IS NULL`).Scan(&physicalFiles)
		_ = s.db.QueryRow(r.Context(), `SELECT count(*),COALESCE(sum(size_bytes),0) FROM images WHERE deleted_at IS NULL AND created_at >= CURRENT_DATE`).Scan(&todayUploads, &todayBytes)
		_ = s.db.QueryRow(r.Context(), `SELECT count(*) FROM images WHERE deleted_at IS NULL AND owner_id IS NULL`).Scan(&guestUploads)
		userUploads = totalFiles - guestUploads
		_ = s.db.QueryRow(r.Context(), `SELECT count(*) FROM images WHERE deleted_at IS NULL AND mime_type LIKE 'image/%'`).Scan(&imageCount)
		_ = s.db.QueryRow(r.Context(), `SELECT count(*) FROM images WHERE deleted_at IS NULL AND mime_type LIKE 'video/%'`).Scan(&videoCount)
		activeUserPercent, registeredUploadPercent := 0.0, 0.0
		if totalUsers > 0 {
			activeUserPercent = float64(activeUsers) / float64(totalUsers) * 100
		}
		if totalFiles > 0 {
			registeredUploadPercent = float64(userUploads) / float64(totalFiles) * 100
		}
		result["admin"] = map[string]any{"total_users": totalUsers, "active_users": activeUsers, "pending_users": pendingUsers, "disabled_users": disabledUsers, "active_user_percent": activeUserPercent, "new_users_today": newUsers, "total_files": totalFiles, "physical_files": physicalFiles, "today_uploads": todayUploads, "today_upload_bytes": todayBytes, "guest_uploads": guestUploads, "user_uploads": userUploads, "registered_upload_percent": registeredUploadPercent, "image_count": imageCount, "video_count": videoCount}
	}
	writeJSON(w, 200, result)
}
func (s *server) forgotPassword(w http.ResponseWriter, r *http.Request) {
	settings := s.settings(r.Context(), "registration")
	if b, ok := settings["password_reset_enabled"].(bool); ok && !b {
		writeErr(w, 403, "PASSWORD_RESET_DISABLED", "password reset is disabled")
		return
	}
	var in struct {
		Email string `json:"email"`
	}
	if !decodeJSON(r, &in) || !strings.Contains(in.Email, "@") {
		writeErr(w, http.StatusBadRequest, "INVALID_INPUT", "a valid email is required")
		return
	}
	// Always return the same response regardless of account existence.
	result := map[string]any{"message": "if the account exists, a reset email will be sent"}
	if s.emailRateLimited(r.Context(), "forgot", in.Email, 5, 10*time.Minute) {
		writeJSON(w, http.StatusAccepted, result)
		return
	}
	if s.db != nil {
		var id string
		var email string
		if s.db.QueryRow(r.Context(), `SELECT id,email FROM users WHERE email=$1`, strings.ToLower(strings.TrimSpace(in.Email))).Scan(&id, &email) == nil {
			_, issueErr := s.issueEmailToken(r.Context(), id, email, "reset")
			if issueErr != nil {
				s.log.Warn("password reset email could not be sent", "user_id", id, "error", issueErr)
			}
		}
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s *server) getSettings(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeErr(w, 503, "DATABASE_UNAVAILABLE", "database is not configured")
		return
	}
	rows, err := s.db.Query(r.Context(), `SELECT key,value FROM system_settings ORDER BY key`)
	if err != nil {
		writeErr(w, 500, "SETTINGS_FAILED", "could not load settings")
		return
	}
	defer rows.Close()
	result := map[string]any{}
	for rows.Next() {
		var key string
		var value []byte
		if rows.Scan(&key, &value) == nil {
			var parsed any
			if json.Unmarshal(value, &parsed) == nil {
				result[key] = redactSetting(key, parsed)
			}
		}
	}
	writeJSON(w, 200, result)
}
func (s *server) updateSetting(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	allowed := map[string]bool{"site": true, "registration": true, "upload": true, "email": true, "storage": true, "oidc": true, "billing": true, "cdn": true}
	if !allowed[key] {
		writeErr(w, 404, "SETTING_NOT_FOUND", "setting group does not exist")
		return
	}
	var value map[string]any
	if !decodeJSON(r, &value) {
		writeErr(w, 400, "INVALID_INPUT", "setting value must be an object")
		return
	}
	if err := validateSettingGroup(key, value); err != nil {
		writeErr(w, 400, "INVALID_SETTING", err.Error())
		return
	}
	// Merge with the existing group and preserve masked secrets returned by GET.
	var existingRaw []byte
	if err := s.db.QueryRow(r.Context(), `SELECT value FROM system_settings WHERE key=$1`, key).Scan(&existingRaw); err == nil {
		var existing map[string]any
		if json.Unmarshal(existingRaw, &existing) == nil {
			for field, incoming := range value {
				if incoming == "********" && existing[field] != nil {
					value[field] = existing[field]
				}
			}
			for field, current := range existing {
				if _, ok := value[field]; !ok {
					value[field] = current
				}
			}
			preserveMaskedStorageSecrets(existing, value)
		}
	}
	if key == "email" {
		if password, ok := value["smtp_password"].(string); ok && password != "" && password != "********" && !strings.HasPrefix(password, "enc:") && !strings.HasPrefix(password, "plain:") {
			if encrypted, encErr := encryptSecret(s.cfg.AppSecret, password); encErr == nil {
				value["smtp_password"] = encrypted
			} else {
				writeErr(w, 500, "SETTINGS_SAVE_FAILED", "could not protect SMTP password")
				return
			}
		}
	}
	if key == "storage" {
		if err := protectStorageSecrets(s.cfg.AppSecret, value); err != nil {
			writeErr(w, 500, "SETTINGS_SAVE_FAILED", err.Error())
			return
		}
		if _, _, _, err := s.buildStorageChannels(value); err != nil {
			writeErr(w, http.StatusBadRequest, "STORAGE_UNAVAILABLE", err.Error())
			return
		}
	}
	raw, _ := json.Marshal(value)
	if _, err := s.db.Exec(r.Context(), `INSERT INTO system_settings(key,value,updated_at) VALUES($1,$2,now()) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_at=now()`, key, raw); err != nil {
		writeErr(w, 500, "SETTINGS_SAVE_FAILED", "could not save settings")
		return
	}
	if key == "storage" {
		_ = s.applyStorageSettings(r.Context())
	}
	writeJSON(w, 200, map[string]any{"key": key, "value": redactSetting(key, value)})
}

func (s *server) testStorage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ChannelID string `json:"channel_id"`
	}
	_ = decodeJSON(r, &in)
	storage, channelID, err := s.configuredStorageChannel(strings.TrimSpace(in.ChannelID))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "STORAGE_UNAVAILABLE", err.Error())
		return
	}
	key := "healthcheck/" + requestID() + ".txt"
	stored, err := storage.Put(r.Context(), key, strings.NewReader("imagehub storage healthcheck"), int64(len("imagehub storage healthcheck")), "text/plain")
	if err != nil {
		writeErr(w, http.StatusBadGateway, "STORAGE_TEST_FAILED", "storage write test failed")
		return
	}
	if err = storage.Delete(r.Context(), stored.Key); err != nil {
		writeErr(w, http.StatusBadGateway, "STORAGE_TEST_FAILED", "storage delete test failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "backend": stored.Backend, "channel_id": channelID})
}

func (s *server) testEmail(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Recipient string `json:"recipient"`
	}
	if !decodeJSON(r, &in) || !strings.Contains(in.Recipient, "@") || strings.ContainsAny(in.Recipient, "\r\n") {
		writeErr(w, http.StatusBadRequest, "INVALID_RECIPIENT", "a valid recipient email is required")
		return
	}
	delivery, err := s.sendEmail(r.Context(), in.Recipient, "ImageHub SMTP test", "This message confirms that ImageHub can deliver email through the configured SMTP service.")
	if err != nil {
		writeErr(w, http.StatusBadGateway, "EMAIL_DELIVERY_FAILED", "SMTP test delivery failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": delivery != "not_configured", "delivery": delivery})
}
func validateSettingGroup(key string, value map[string]any) error {
	switch key {
	case "site":
		if language, ok := value["default_language"].(string); ok && language != "zh-CN" && language != "en-US" {
			return errors.New("default_language must be zh-CN or en-US")
		}
	case "storage":
		if raw, ok := value["channels"].([]any); ok {
			if len(raw) == 0 {
				return errors.New("at least one storage channel is required")
			}
			seen := map[string]bool{}
			for _, item := range raw {
				channel, ok := item.(map[string]any)
				if !ok {
					return errors.New("storage channels must be objects")
				}
				id := strings.TrimSpace(fmt.Sprint(channel["id"]))
				if id == "" || seen[id] {
					return errors.New("storage channel ids must be unique and non-empty")
				}
				seen[id] = true
				backend := strings.TrimSpace(fmt.Sprint(channel["backend"]))
				if backend != "local" && backend != "telegram" && backend != "s3" {
					return errors.New("storage channel backend must be local, telegram, or s3")
				}
				if backend == "s3" {
					for _, field := range []string{"s3_region", "s3_bucket", "s3_access_key", "s3_secret_key"} {
						if strings.TrimSpace(fmt.Sprint(channel[field])) == "" || fmt.Sprint(channel[field]) == "<nil>" || fmt.Sprint(channel[field]) == "********" {
							continue
						}
					}
				}
			}
			if defaultID, ok := value["default_channel"].(string); ok && defaultID != "" && !seen[defaultID] {
				return errors.New("default storage channel does not exist")
			}
		}
		if backend, ok := value["backend"].(string); ok && backend != "local" && backend != "telegram" && backend != "s3" {
			return errors.New("storage backend must be local, telegram, or s3")
		}
		if backend, _ := value["backend"].(string); backend == "s3" {
			for _, field := range []string{"s3_region", "s3_bucket", "s3_access_key", "s3_secret_key"} {
				if strings.TrimSpace(fmt.Sprint(value[field])) == "" || fmt.Sprint(value[field]) == "<nil>" {
					return fmt.Errorf("%s is required for S3 storage", field)
				}
			}
		}
	case "email":
		if security, ok := value["smtp_security"].(string); ok && security != "starttls" && security != "tls" && security != "plain" {
			return errors.New("smtp security must be starttls, tls, or plain")
		}
	case "registration":
	case "upload":
		if v, ok := value["max_file_bytes"].(float64); ok && (v < 1 || v > 10*1024*1024*1024) {
			return errors.New("max_file_bytes is out of range")
		}
		if v, ok := value["daily_upload_limit"].(float64); ok && v < 0 {
			return errors.New("daily_upload_limit cannot be negative")
		}
		if mode, ok := value["naming_mode"].(string); ok && mode != "sha256" && mode != "md5" && mode != "original" && mode != "uuid" && mode != "random" {
			return errors.New("naming_mode is invalid")
		}
		if rule, ok := value["directory_rule"].(string); ok && rule != "hash2" && rule != "none" && rule != "year" && rule != "ym" && rule != "ymd" {
			return errors.New("directory_rule is invalid")
		}
		if v, ok := value["random_length"].(float64); ok && (v < 4 || v > 128) {
			return errors.New("random_length must be between 4 and 128")
		}
		if template, ok := value["path_template"].(string); ok && len(template) > 512 {
			return errors.New("path_template is too long")
		}
	}
	return nil
}

func (s *server) storageFor(backend string) imagehub.Storage {
	if backend == "s3" && s.s3Storage != nil {
		return s.s3Storage
	}
	if backend == "telegram" && s.telegramStorage != nil {
		return s.telegramStorage
	}
	if backend == "local" && s.localStorage != nil {
		return s.localStorage
	}
	return s.storage
}

func (s *server) storageForChannel(backend, channelID string) imagehub.Storage {
	if channelID != "" {
		if storage := s.storageChannels[channelID]; storage != nil {
			return storage
		}
	}
	return s.storageFor(backend)
}

func (s *server) storageFromSettings(value map[string]any) (imagehub.Storage, error) {
	backend, _ := value["backend"].(string)
	switch backend {
	case "local", "":
		return s.localStorage, nil
	case "telegram":
		chatID := strings.TrimSpace(fmt.Sprint(value["telegram_chat_id"]))
		if chatID == "" || chatID == "<nil>" {
			chatID = s.cfg.TelegramChatID
		}
		if s.cfg.TelegramToken == "" || chatID == "" {
			return nil, errors.New("Telegram storage requires TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID")
		}
		s.telegramStorage = imagehub.NewTelegramStorage(s.cfg.TelegramToken, chatID)
		return s.telegramStorage, nil
	case "s3":
		secret, err := decryptSecret(s.cfg.AppSecret, fmt.Sprint(value["s3_secret_key"]))
		if err != nil {
			return nil, err
		}
		storage, err := imagehub.NewS3Storage(fmt.Sprint(value["s3_endpoint"]), fmt.Sprint(value["s3_region"]), fmt.Sprint(value["s3_bucket"]), fmt.Sprint(value["s3_access_key"]), secret, fmt.Sprint(value["s3_prefix"]), value["s3_path_style"] == true)
		if err != nil {
			return nil, err
		}
		return storage, nil
	default:
		return nil, errors.New("unsupported storage backend")
	}
}

func (s *server) applyStorageSettings(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	channels, backends, defaultID, err := s.buildStorageChannels(s.settings(ctx, "storage"))
	if err != nil {
		return err
	}
	s.storageChannels, s.storageBackends, s.defaultChannel = channels, backends, defaultID
	s.storage = channels[defaultID]
	if s.storage == nil {
		return errors.New("default storage channel is unavailable")
	}
	if s.storageBackends[defaultID] == "s3" {
		s.s3Storage = s.storage
	}
	return nil
}

func (s *server) configuredStorage() imagehub.Storage {
	storage, _, err := s.configuredStorageChannel("")
	if err != nil {
		return s.storage
	}
	return storage
}

func (s *server) configuredStorageChannel(requested string) (imagehub.Storage, string, error) {
	if len(s.storageChannels) == 0 && s.db != nil {
		if err := s.applyStorageSettings(context.Background()); err != nil {
			return nil, "", err
		}
	}
	if requested != "" {
		if storage := s.storageChannels[requested]; storage != nil {
			return storage, requested, nil
		}
		return nil, "", errors.New("storage channel is disabled or does not exist")
	}
	channelID := s.defaultChannel
	if channelID == "" {
		channelID = "local"
	}
	if storage := s.storageChannels[channelID]; storage != nil {
		return storage, channelID, nil
	}
	return s.storage, channelID, nil
}

func (s *server) buildStorageChannels(value map[string]any) (map[string]imagehub.Storage, map[string]string, string, error) {
	channels := map[string]imagehub.Storage{}
	backends := map[string]string{}
	defaultID := ""
	if raw, ok := value["channels"].([]any); ok && len(raw) > 0 {
		for _, item := range raw {
			channel, ok := item.(map[string]any)
			if !ok {
				return nil, nil, "", errors.New("storage channel must be an object")
			}
			enabled, exists := channel["enabled"].(bool)
			if exists && !enabled {
				continue
			}
			id := strings.TrimSpace(fmt.Sprint(channel["id"]))
			if id == "" {
				return nil, nil, "", errors.New("storage channel id is required")
			}
			backend := strings.TrimSpace(fmt.Sprint(channel["backend"]))
			if backend == "" {
				backend = "local"
			}
			storage, err := s.storageFromSettings(channel)
			if err != nil {
				return nil, nil, "", fmt.Errorf("channel %s: %w", id, err)
			}
			channels[id], backends[id] = storage, backend
			if defaultID == "" {
				defaultID = id
			}
		}
		if requested, ok := value["default_channel"].(string); ok && channels[requested] != nil {
			defaultID = requested
		}
	} else {
		backend, _ := value["backend"].(string)
		if backend == "" {
			backend = "local"
		}
		storage, err := s.storageFromSettings(value)
		if err != nil {
			return nil, nil, "", err
		}
		defaultID, channels[backend], backends[backend] = backend, storage, backend
	}
	if len(channels) == 0 {
		return nil, nil, "", errors.New("no enabled storage channels")
	}
	return channels, backends, defaultID, nil
}

func (s *server) listStorageChannels(w http.ResponseWriter, r *http.Request) {
	settings := s.settings(r.Context(), "storage")
	result := []map[string]any{}
	if raw, ok := settings["channels"].([]any); ok {
		defaultID, _ := settings["default_channel"].(string)
		for _, item := range raw {
			if channel, ok := item.(map[string]any); ok {
				enabled, exists := channel["enabled"].(bool)
				if exists && !enabled {
					continue
				}
				id := strings.TrimSpace(fmt.Sprint(channel["id"]))
				if id == "" {
					continue
				}
				result = append(result, map[string]any{"id": id, "name": fmt.Sprint(channel["name"]), "backend": fmt.Sprint(channel["backend"]), "is_default": id == defaultID})
			}
		}
	} else {
		backend, _ := settings["backend"].(string)
		if backend == "" {
			backend = "local"
		}
		result = append(result, map[string]any{"id": backend, "name": backend, "backend": backend, "is_default": true})
	}
	writeJSON(w, http.StatusOK, result)
}
func (s *server) featureStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"storage_backends": []string{"local", "telegram", "s3"}, "media": []string{"svg", "gif", "video", "remote_url"}, "teams": true, "billing": true, "cdn": true, "custom_domains": true, "oidc": s.settings(r.Context(), "oidc")})
}

func (s *server) updateUserPolicy(w http.ResponseWriter, r *http.Request) {
	var in struct {
		QuotaBytes       *int64         `json:"quota_bytes"`
		MaxFileBytes     *int64         `json:"max_file_bytes"`
		DailyUploadLimit *int64         `json:"daily_upload_limit"`
		UploadPolicy     map[string]any `json:"upload_policy"`
	}
	if !decodeJSON(r, &in) {
		writeErr(w, 400, "INVALID_INPUT", "invalid policy payload")
		return
	}
	if (in.QuotaBytes != nil && *in.QuotaBytes < 0) || (in.MaxFileBytes != nil && *in.MaxFileBytes < 1) || (in.DailyUploadLimit != nil && *in.DailyUploadLimit < 0) {
		writeErr(w, 400, "INVALID_POLICY", "quota and upload limits are invalid")
		return
	}
	var current map[string]any
	var raw []byte
	_ = s.db.QueryRow(r.Context(), `SELECT upload_policy FROM users WHERE id=$1`, r.PathValue("id")).Scan(&raw)
	_ = json.Unmarshal(raw, &current)
	if current == nil {
		current = map[string]any{}
	}
	for key, value := range in.UploadPolicy {
		current[key] = value
	}
	policy, _ := json.Marshal(current)
	var id string
	err := s.db.QueryRow(r.Context(), `UPDATE users SET quota_bytes=COALESCE($1,quota_bytes),max_file_bytes=COALESCE($2,max_file_bytes),daily_upload_limit=COALESCE($3,daily_upload_limit),upload_policy=$4,updated_at=now() WHERE id=$5 RETURNING id`, in.QuotaBytes, in.MaxFileBytes, in.DailyUploadLimit, policy, r.PathValue("id")).Scan(&id)
	if err != nil {
		writeErr(w, 404, "USER_NOT_FOUND", "user not found")
		return
	}
	writeJSON(w, 200, map[string]any{"user_id": id, "quota_bytes": in.QuotaBytes, "max_file_bytes": in.MaxFileBytes, "daily_upload_limit": in.DailyUploadLimit, "upload_policy": current})
}

func (s *server) listAdminUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `SELECT id,email,display_name,role,status,quota_bytes,used_bytes,max_file_bytes,daily_upload_limit,upload_policy,created_at FROM users ORDER BY created_at DESC LIMIT 500`)
	if err != nil {
		writeErr(w, 500, "USERS_FAILED", "could not load users")
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var id, email, name, role, status string
		var quota, used, maxFile, daily int64
		var uploadPolicy []byte
		var created time.Time
		if rows.Scan(&id, &email, &name, &role, &status, &quota, &used, &maxFile, &daily, &uploadPolicy, &created) == nil {
			var policy map[string]any
			_ = json.Unmarshal(uploadPolicy, &policy)
			result = append(result, map[string]any{"id": id, "email": email, "display_name": name, "role": role, "status": status, "quota_bytes": quota, "used_bytes": used, "max_file_bytes": maxFile, "daily_upload_limit": daily, "upload_policy": policy, "created_at": created})
		}
	}
	writeJSON(w, 200, result)
}

func (s *server) updateAdminUser(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userKey).(user)
	var in struct {
		DisplayName *string `json:"display_name"`
		Role        *string `json:"role"`
		Status      *string `json:"status"`
	}
	if !decodeJSON(r, &in) || (in.Role != nil && *in.Role != "admin" && *in.Role != "user") || (in.Status != nil && *in.Status != "active" && *in.Status != "disabled" && *in.Status != "pending") {
		writeErr(w, http.StatusBadRequest, "INVALID_USER_UPDATE", "role or status is invalid")
		return
	}
	if in.Status != nil && *in.Status == "disabled" && r.PathValue("id") == u.ID {
		writeErr(w, http.StatusBadRequest, "SELF_DISABLE_BLOCKED", "you cannot disable your own administrator account")
		return
	}
	var id, role, status string
	err := s.db.QueryRow(r.Context(), `UPDATE users SET display_name=COALESCE($1,display_name),role=COALESCE($2,role),status=COALESCE($3,status),updated_at=now() WHERE id=$4 RETURNING id,role,status`, in.DisplayName, in.Role, in.Status, r.PathValue("id")).Scan(&id, &role, &status)
	if err != nil {
		writeErr(w, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "role": role, "status": status})
}

func (s *server) listAdminImages(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	mimeFilter := strings.TrimSpace(r.URL.Query().Get("mime"))
	limit := 200
	if value, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && value > 0 && value < 1000 {
		limit = value
	}
	rows, err := s.db.Query(r.Context(), `SELECT i.id,i.original_name,i.content_hash,i.mime_type,i.size_bytes,COALESCE(i.width,0),COALESCE(i.height,0),i.storage_backend,COALESCE(i.storage_channel,''),COALESCE(i.owner_id::text,''),COALESCE(u.email,''),COALESCE(i.thumbnail_object_key,''),i.created_at,(SELECT count(*) FROM images ref WHERE ref.object_key=i.object_key AND ref.deleted_at IS NULL) FROM images i LEFT JOIN users u ON u.id=i.owner_id WHERE i.deleted_at IS NULL AND ($1='' OR i.original_name ILIKE '%'||$1||'%' OR i.content_hash ILIKE '%'||$1||'%' OR COALESCE(u.email,'') ILIKE '%'||$1||'%') AND ($2='' OR i.mime_type=$2) ORDER BY i.created_at DESC LIMIT $3`, query, mimeFilter, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ADMIN_IMAGES_FAILED", "could not load images")
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var id, name, hash, mimeType, backend, channelID, ownerID, ownerEmail, thumbnailKey string
		var size, width, height, references int64
		var created time.Time
		if rows.Scan(&id, &name, &hash, &mimeType, &size, &width, &height, &backend, &channelID, &ownerID, &ownerEmail, &thumbnailKey, &created, &references) == nil {
			item := map[string]any{"id": id, "name": name, "content_hash": hash, "mime_type": mimeType, "size_bytes": size, "width": width, "height": height, "storage_backend": backend, "storage_channel": channelID, "owner_id": ownerID, "owner_email": ownerEmail, "references": references, "created_at": created, "url": s.resourceURL(id, "public")}
			if thumbnailKey != "" {
				item["thumbnail_url"] = s.resourceURL(id+"/thumbnail", "public")
			}
			result = append(result, item)
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) forceDeleteImage(w http.ResponseWriter, r *http.Request) {
	var key, backend, channelID, thumbnail string
	if err := s.db.QueryRow(r.Context(), `UPDATE images SET deleted_at=now(),status='deleted',deleted_by=$1 WHERE id=$2 AND deleted_at IS NULL RETURNING object_key,storage_backend,COALESCE(storage_channel,''),COALESCE(thumbnail_object_key,'')`, r.Context().Value(userKey).(user).ID, r.PathValue("id")).Scan(&key, &backend, &channelID, &thumbnail); err != nil {
		writeErr(w, http.StatusNotFound, "IMAGE_NOT_FOUND", "image not found")
		return
	}
	var references int
	_ = s.db.QueryRow(r.Context(), `SELECT count(*) FROM images WHERE object_key=$1 AND deleted_at IS NULL`, key).Scan(&references)
	if references == 0 {
		_ = s.storageForChannel(backend, channelID).Delete(r.Context(), key)
		if thumbnail != "" {
			_ = s.storageForChannel(backend, channelID).Delete(r.Context(), thumbnail)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "physical_deleted": references == 0})
}

func (s *server) listTeams(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userKey).(user)
	if s.db == nil {
		writeJSON(w, 200, []any{})
		return
	}
	rows, err := s.db.Query(r.Context(), `SELECT t.id,t.name,t.slug,tm.role,t.quota_bytes,t.used_bytes,t.plan_code FROM teams t JOIN team_members tm ON tm.team_id=t.id WHERE tm.user_id=$1 ORDER BY t.created_at`, u.ID)
	if err != nil {
		writeErr(w, 500, "TEAMS_FAILED", "could not load teams")
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var id, name, slug, role, plan string
		var quota, used int64
		if rows.Scan(&id, &name, &slug, &role, &quota, &used, &plan) == nil {
			result = append(result, map[string]any{"id": id, "name": name, "slug": slug, "role": role, "quota_bytes": quota, "used_bytes": used, "plan_code": plan})
		}
	}
	writeJSON(w, 200, result)
}

func (s *server) createTeam(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userKey).(user)
	var in struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if !decodeJSON(r, &in) || strings.TrimSpace(in.Name) == "" {
		writeErr(w, 400, "INVALID_INPUT", "team name is required")
		return
	}
	slug := strings.ToLower(strings.TrimSpace(in.Slug))
	if slug == "" {
		slug = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(in.Name), " ", "-"))
	}
	slug = regexp.MustCompile(`[^a-z0-9-]`).ReplaceAllString(slug, "-")
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeErr(w, 500, "TEAM_CREATE_FAILED", "could not create team")
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	err = tx.QueryRow(r.Context(), `INSERT INTO teams(name,slug,owner_id) VALUES($1,$2,$3) RETURNING id`, strings.TrimSpace(in.Name), slug, u.ID).Scan(&id)
	if err != nil {
		writeErr(w, 409, "TEAM_EXISTS", "team slug is already in use")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO team_members(team_id,user_id,role) VALUES($1,$2,'owner')`, id, u.ID); err != nil {
		writeErr(w, 500, "TEAM_MEMBER_FAILED", "could not create team membership")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeErr(w, 500, "TEAM_CREATE_FAILED", "could not create team")
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "name": in.Name, "slug": slug, "role": "owner"})
}

func (s *server) listPlans(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeJSON(w, 200, []any{})
		return
	}
	rows, err := s.db.Query(r.Context(), `SELECT code,name,price_cents,quota_bytes,bandwidth_bytes,member_limit,features FROM plans WHERE active=true ORDER BY price_cents`)
	if err != nil {
		writeErr(w, 500, "PLANS_FAILED", "could not load plans")
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var code, name string
		var price, members int
		var quota, bandwidth int64
		var features []byte
		if rows.Scan(&code, &name, &price, &quota, &bandwidth, &members, &features) == nil {
			var parsed any
			_ = json.Unmarshal(features, &parsed)
			result = append(result, map[string]any{"code": code, "name": name, "price_cents": price, "quota_bytes": quota, "bandwidth_bytes": bandwidth, "member_limit": members, "features": parsed})
		}
	}
	writeJSON(w, 200, result)
}
func (s *server) listAdminPlans(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `SELECT code,name,price_cents,quota_bytes,bandwidth_bytes,member_limit,features,active,COALESCE(provider_price_id,'') FROM plans ORDER BY price_cents`)
	if err != nil {
		writeErr(w, 500, "PLANS_FAILED", "could not load plans")
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var code, name, priceID string
		var price, members int
		var quota, bandwidth int64
		var features []byte
		var active bool
		if rows.Scan(&code, &name, &price, &quota, &bandwidth, &members, &features, &active, &priceID) == nil {
			var parsed any
			_ = json.Unmarshal(features, &parsed)
			result = append(result, map[string]any{"code": code, "name": name, "price_cents": price, "quota_bytes": quota, "bandwidth_bytes": bandwidth, "member_limit": members, "features": parsed, "active": active, "provider_price_id": priceID})
		}
	}
	writeJSON(w, 200, result)
}
func (s *server) updateAdminPlan(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name            string         `json:"name"`
		PriceCents      *int           `json:"price_cents"`
		QuotaBytes      *int64         `json:"quota_bytes"`
		BandwidthBytes  *int64         `json:"bandwidth_bytes"`
		MemberLimit     *int           `json:"member_limit"`
		ProviderPriceID *string        `json:"provider_price_id"`
		Active          *bool          `json:"active"`
		Features        map[string]any `json:"features"`
	}
	if !decodeJSON(r, &in) {
		writeErr(w, 400, "INVALID_INPUT", "invalid plan payload")
		return
	}
	features, _ := json.Marshal(in.Features)
	res, err := s.db.Exec(r.Context(), `UPDATE plans SET name=COALESCE(NULLIF($1,''),name),price_cents=COALESCE($2,price_cents),quota_bytes=COALESCE($3,quota_bytes),bandwidth_bytes=COALESCE($4,bandwidth_bytes),member_limit=COALESCE($5,member_limit),provider_price_id=COALESCE($6,provider_price_id),active=COALESCE($7,active),features=CASE WHEN $8::jsonb='null'::jsonb THEN features ELSE $8 END WHERE code=$9`, in.Name, in.PriceCents, in.QuotaBytes, in.BandwidthBytes, in.MemberLimit, in.ProviderPriceID, in.Active, features, r.PathValue("code"))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, 404, "PLAN_NOT_FOUND", "plan not found")
		return
	}
	writeJSON(w, 200, map[string]any{"updated": true, "code": r.PathValue("code")})
}
func (s *server) listDomains(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userKey).(user)
	rows, err := s.db.Query(r.Context(), `SELECT id,domain,verified_at,tls_status,is_default FROM custom_domains WHERE owner_id=$1 OR team_id IN (SELECT team_id FROM team_members WHERE user_id=$1) ORDER BY created_at DESC`, u.ID)
	if err != nil {
		writeErr(w, 500, "DOMAINS_FAILED", "could not load domains")
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var id, domain, tls string
		var verified *time.Time
		var isDefault bool
		if rows.Scan(&id, &domain, &verified, &tls, &isDefault) == nil {
			result = append(result, map[string]any{"id": id, "domain": domain, "verified_at": verified, "tls_status": tls, "is_default": isDefault})
		}
	}
	writeJSON(w, 200, result)
}

func (s *server) verifyDomain(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userKey).(user)
	var domain, token string
	var ownerID, teamID *string
	if err := s.db.QueryRow(r.Context(), `SELECT domain,verification_token,owner_id,team_id FROM custom_domains WHERE id=$1`, r.PathValue("id")).Scan(&domain, &token, &ownerID, &teamID); err != nil {
		writeErr(w, http.StatusNotFound, "DOMAIN_NOT_FOUND", "domain is not configured")
		return
	}
	allowed := ownerID != nil && *ownerID == u.ID
	if !allowed && teamID != nil {
		_, allowed = s.teamRole(r, *teamID)
	}
	if !allowed {
		writeErr(w, http.StatusForbidden, "DOMAIN_ACCESS_DENIED", "domain ownership is required")
		return
	}
	records, err := net.LookupTXT("_imagehub-verification." + strings.TrimSuffix(domain, "."))
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "DOMAIN_DNS_LOOKUP_FAILED", "could not resolve the verification record")
		return
	}
	verified := false
	for _, record := range records {
		if strings.TrimSpace(record) == token {
			verified = true
			break
		}
	}
	if !verified {
		writeErr(w, http.StatusUnprocessableEntity, "DOMAIN_NOT_VERIFIED", "the TXT verification record does not match")
		return
	}
	tlsStatus := "failed"
	if conn, tlsErr := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", domain+":443", &tls.Config{ServerName: domain, MinVersion: tls.VersionTLS12}); tlsErr == nil {
		tlsStatus = "active"
		_ = conn.Close()
	}
	if _, err = s.db.Exec(r.Context(), `UPDATE custom_domains SET verified_at=COALESCE(verified_at,now()),tls_status=$1,last_checked_at=now() WHERE id=$2`, tlsStatus, r.PathValue("id")); err != nil {
		writeErr(w, 500, "DOMAIN_VERIFY_FAILED", "could not save domain verification")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": r.PathValue("id"), "domain": domain, "verified": true, "tls_status": tlsStatus})
}

func (s *server) createDomain(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userKey).(user)
	var in struct {
		Domain string `json:"domain"`
	}
	if !decodeJSON(r, &in) {
		writeErr(w, 400, "INVALID_DOMAIN", "a fully qualified domain is required")
		return
	}
	in.Domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(in.Domain), "."))
	if !validDomainName(in.Domain) {
		writeErr(w, 400, "INVALID_DOMAIN", "a fully qualified domain is required")
		return
	}
	token := requestID() + requestID()
	var id string
	err := s.db.QueryRow(r.Context(), `INSERT INTO custom_domains(owner_id,domain,verification_token) VALUES($1,$2,$3) RETURNING id`, u.ID, in.Domain, token).Scan(&id)
	if err != nil {
		writeErr(w, 409, "DOMAIN_EXISTS", "domain is already configured")
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "domain": in.Domain, "verification": map[string]string{"type": "TXT", "name": "_imagehub-verification." + in.Domain, "value": token}, "tls_status": "pending"})
}

func validDomainName(domain string) bool {
	if len(domain) < 3 || len(domain) > 253 || strings.Contains(domain, "..") {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if !(r == '-' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
				return false
			}
		}
	}
	return true
}

func (s *server) deleteDomain(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userKey).(user)
	var owner string
	var team *string
	if s.db.QueryRow(r.Context(), `SELECT owner_id,team_id FROM custom_domains WHERE id=$1`, r.PathValue("id")).Scan(&owner, &team) != nil {
		writeErr(w, 404, "DOMAIN_NOT_FOUND", "domain is not configured")
		return
	}
	allowed := owner == u.ID
	if !allowed && team != nil {
		_, allowed = s.teamRole(r, *team)
	}
	if !allowed {
		writeErr(w, 403, "DOMAIN_ACCESS_DENIED", "domain ownership is required")
		return
	}
	if _, err := s.db.Exec(r.Context(), `DELETE FROM custom_domains WHERE id=$1`, r.PathValue("id")); err != nil {
		writeErr(w, 500, "DOMAIN_DELETE_FAILED", "could not remove domain")
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": true})
}

// authorizeDomainTLS is intentionally read-only and returns a bare 200/404
// for Caddy's on-demand TLS ask hook. Only DNS-verified domains are approved.
func (s *server) authorizeDomainTLS(w http.ResponseWriter, r *http.Request) {
	domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(r.URL.Query().Get("domain")), "."))
	var ok bool
	if !validDomainName(domain) || s.db == nil || s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM custom_domains WHERE lower(domain)=$1 AND verified_at IS NOT NULL)`, domain).Scan(&ok) != nil || !ok {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listOIDCProviders(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `SELECT id,name,issuer_url,client_id,scopes,enabled,auto_create_users FROM oidc_providers ORDER BY created_at`)
	if err != nil {
		writeErr(w, 500, "OIDC_FAILED", "could not load OIDC providers")
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var id, name, issuer, client string
		var scopes []string
		var enabled, auto bool
		if rows.Scan(&id, &name, &issuer, &client, &scopes, &enabled, &auto) == nil {
			result = append(result, map[string]any{"id": id, "name": name, "issuer_url": issuer, "client_id": client, "scopes": scopes, "enabled": enabled, "auto_create_users": auto})
		}
	}
	writeJSON(w, 200, result)
}
func (s *server) publicOIDCProviders(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `SELECT id,name FROM oidc_providers WHERE enabled=true ORDER BY created_at`)
	if err != nil {
		writeJSON(w, 200, []any{})
		return
	}
	defer rows.Close()
	result := []map[string]string{}
	for rows.Next() {
		var id, name string
		if rows.Scan(&id, &name) == nil {
			result = append(result, map[string]string{"id": id, "name": name})
		}
	}
	writeJSON(w, 200, result)
}

func (s *server) settings(ctx context.Context, key string) map[string]any {
	result := map[string]any{}
	if s.db == nil {
		return result
	}
	var raw []byte
	if s.db.QueryRow(ctx, `SELECT value FROM system_settings WHERE key=$1`, key).Scan(&raw) == nil {
		_ = json.Unmarshal(raw, &result)
		if key == "email" {
			if encrypted, ok := result["smtp_password"].(string); ok && (strings.HasPrefix(encrypted, "enc:") || strings.HasPrefix(encrypted, "plain:")) {
				if decrypted, err := decryptSecret(s.cfg.AppSecret, encrypted); err == nil {
					result["smtp_password"] = decrypted
				}
			}
		}
		if key == "storage" {
			decryptStorageSecrets(s.cfg.AppSecret, result)
		}
	}
	return result
}
func redactSetting(key string, value any) any {
	if key != "email" && key != "oidc" && key != "storage" {
		return value
	}
	m, ok := value.(map[string]any)
	if !ok {
		return value
	}
	return redactSecrets(m)
}

func redactSecrets(value any) any {
	switch current := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, v := range current {
			lower := strings.ToLower(k)
			if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") {
				if v != nil && fmt.Sprint(v) != "" {
					out[k] = "********"
				} else {
					out[k] = ""
				}
			} else {
				out[k] = redactSecrets(v)
			}
		}
		return out
	case []any:
		out := make([]any, len(current))
		for i, item := range current {
			out[i] = redactSecrets(item)
		}
		return out
	default:
		return value
	}
}

func protectStorageSecrets(appSecret string, value map[string]any) error {
	protect := func(channel map[string]any) error {
		secret, ok := channel["s3_secret_key"].(string)
		if !ok || secret == "" || secret == "********" || strings.HasPrefix(secret, "enc:") || strings.HasPrefix(secret, "plain:") {
			return nil
		}
		encrypted, err := encryptSecret(appSecret, secret)
		if err != nil {
			return err
		}
		channel["s3_secret_key"] = encrypted
		return nil
	}
	if err := protect(value); err != nil {
		return errors.New("could not protect S3 secret key")
	}
	if raw, ok := value["channels"].([]any); ok {
		for _, item := range raw {
			if channel, ok := item.(map[string]any); ok {
				if err := protect(channel); err != nil {
					return errors.New("could not protect S3 secret key")
				}
			}
		}
	}
	return nil
}

func preserveMaskedStorageSecrets(existing, incoming map[string]any) {
	oldChannels, oldOK := existing["channels"].([]any)
	newChannels, newOK := incoming["channels"].([]any)
	if !oldOK || !newOK {
		return
	}
	byID := map[string]map[string]any{}
	for _, item := range oldChannels {
		if channel, ok := item.(map[string]any); ok {
			byID[fmt.Sprint(channel["id"])] = channel
		}
	}
	for _, item := range newChannels {
		if channel, ok := item.(map[string]any); ok {
			if old := byID[fmt.Sprint(channel["id"])]; old != nil {
				for _, field := range []string{"s3_secret_key"} {
					if channel[field] == "********" && old[field] != nil {
						channel[field] = old[field]
					}
				}
			}
		}
	}
}

func decryptStorageSecrets(appSecret string, value map[string]any) {
	decrypt := func(channel map[string]any) {
		if encrypted, ok := channel["s3_secret_key"].(string); ok && (strings.HasPrefix(encrypted, "enc:") || strings.HasPrefix(encrypted, "plain:")) {
			if decrypted, err := decryptSecret(appSecret, encrypted); err == nil {
				channel["s3_secret_key"] = decrypted
			}
		}
	}
	decrypt(value)
	if raw, ok := value["channels"].([]any); ok {
		for _, item := range raw {
			if channel, ok := item.(map[string]any); ok {
				decrypt(channel)
			}
		}
	}
}

func (s *server) upload(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userKey).(user)
	limit := s.uploadLimit(r.Context(), u.ID)
	r.Body = http.MaxBytesReader(w, r.Body, limit+1024)
	if err := r.ParseMultipartForm(limit + 1024); err != nil {
		writeErr(w, 413, "UPLOAD_TOO_LARGE", "upload exceeds configured limit")
		return
	}
	teamID := strings.TrimSpace(r.FormValue("team_id"))
	storageChannel := strings.TrimSpace(r.FormValue("storage_channel"))
	if teamID != "" {
		if _, ok := s.teamRole(r, teamID); !ok {
			writeErr(w, 403, "TEAM_ACCESS_DENIED", "team membership is required")
			return
		}
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "FILE_REQUIRED", "multipart field file is required")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		writeErr(w, 413, "UPLOAD_TOO_LARGE", "upload exceeds configured limit")
		return
	}
	mimeType := http.DetectContentType(data)
	if declared := header.Header.Get("Content-Type"); strings.HasPrefix(declared, "image/svg") || strings.HasPrefix(mimeType, "text/xml") {
		mimeType = "image/svg+xml"
	}
	if !allowedMime(mimeType, s.effectiveUploadSettings(r.Context(), u.ID)) {
		writeErr(w, 415, "UNSUPPORTED_MEDIA", "file type is not allowed")
		return
	}
	if !s.reserveStore(r.Context(), u.ID, int64(len(data)), limit) || (teamID != "" && !s.reserveTeam(r.Context(), teamID, int64(len(data)))) {
		if teamID != "" {
			s.releaseStore(r.Context(), u.ID, int64(len(data)))
		}
		writeErr(w, http.StatusRequestEntityTooLarge, "QUOTA_EXCEEDED", "user storage quota or file policy exceeded")
		return
	}
	if mimeType == "image/svg+xml" && unsafeSVG(data) {
		s.releaseStore(r.Context(), u.ID, int64(len(data)))
		if teamID != "" {
			s.releaseTeam(r.Context(), teamID, int64(len(data)))
		}
		writeErr(w, 415, "UNSAFE_SVG", "svg contains active content")
		return
	}
	if mimeType == "image/svg+xml" && !validSVG(data) {
		s.releaseStore(r.Context(), u.ID, int64(len(data)))
		if teamID != "" {
			s.releaseTeam(r.Context(), teamID, int64(len(data)))
		}
		writeErr(w, 415, "INVALID_SVG", "file is not a valid SVG document")
		return
	}
	id, err := s.persistImage(r.Context(), u.ID, header.Filename, mimeType, data, storageChannel, teamID)
	if err != nil {
		s.releaseStore(r.Context(), u.ID, int64(len(data)))
		if teamID != "" {
			s.releaseTeam(r.Context(), teamID, int64(len(data)))
		}
		writeErr(w, 500, "UPLOAD_FAILED", err.Error())
		return
	}
	if deduplicated, _ := id["deduplicated"].(bool); deduplicated {
		// The physical object is shared with an existing upload, so this new
		// upload record must not consume the same bytes a second time.
		s.releaseStore(r.Context(), u.ID, int64(len(data)))
		if teamID != "" {
			s.releaseTeam(r.Context(), teamID, int64(len(data)))
		}
	}
	writeJSON(w, 201, id)
}

// guestUpload keeps the public upload flow separate from the authenticated
// quota path. Guest records are link-only, carry an expiry, and never create a
// user account. The same persistence and hash-deduplication code is reused so
// a guest upload can share a physical object with an authenticated upload.
func (s *server) guestUpload(w http.ResponseWriter, r *http.Request) {
	settings := s.settings(r.Context(), "upload")
	if enabled, ok := settings["anonymous_enabled"].(bool); !ok || !enabled {
		writeErr(w, http.StatusForbidden, "GUEST_UPLOAD_DISABLED", "guest uploads are disabled")
		return
	}
	if s.guestRateLimited(r, settings) {
		writeErr(w, http.StatusTooManyRequests, "GUEST_UPLOAD_RATE_LIMITED", "guest upload limit reached")
		return
	}
	limit := s.cfg.MaxUploadBytes
	if value, ok := settings["max_file_bytes"].(float64); ok && value > 0 && int64(value) < limit {
		limit = int64(value)
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit+1024)
	if err := r.ParseMultipartForm(limit + 1024); err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "UPLOAD_TOO_LARGE", "upload exceeds configured limit")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "FILE_REQUIRED", "multipart field file is required")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		writeErr(w, http.StatusRequestEntityTooLarge, "UPLOAD_TOO_LARGE", "upload exceeds configured limit")
		return
	}
	mimeType := http.DetectContentType(data)
	if declared := header.Header.Get("Content-Type"); strings.HasPrefix(declared, "image/svg") || strings.HasPrefix(mimeType, "text/xml") {
		mimeType = "image/svg+xml"
	}
	if !allowedMime(mimeType, settings) {
		writeErr(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA", "file type is not allowed")
		return
	}
	if mimeType == "image/svg+xml" && unsafeSVG(data) {
		writeErr(w, http.StatusUnsupportedMediaType, "UNSAFE_SVG", "svg contains active content")
		return
	}
	if mimeType == "image/svg+xml" && !validSVG(data) {
		writeErr(w, http.StatusUnsupportedMediaType, "INVALID_SVG", "file is not a valid SVG document")
		return
	}
	if !s.guestBytesAllowed(r, settings, int64(len(data))) {
		writeErr(w, http.StatusRequestEntityTooLarge, "GUEST_QUOTA_EXCEEDED", "guest daily storage limit reached")
		return
	}
	result, err := s.persistImage(r.Context(), "", header.Filename, mimeType, data, "")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "UPLOAD_FAILED", err.Error())
		return
	}
	token := ""
	if shortURL, ok := result["short_url"].(string); ok {
		token = filepath.Base(strings.TrimRight(shortURL, "/"))
	}
	if token == "" {
		token, err = newOpaqueToken()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "TOKEN_FAILED", "could not create guest access token")
			return
		}
	}
	retentionDays := int64(7)
	if value, ok := settings["guest_retention_days"].(float64); ok && value >= 0 {
		retentionDays = int64(value)
	}
	var expiresAt *time.Time
	if retentionDays > 0 {
		value := time.Now().Add(time.Duration(retentionDays) * 24 * time.Hour)
		expiresAt = &value
	}
	if s.db != nil {
		if _, err = s.db.Exec(r.Context(), `UPDATE images SET visibility='link',link_token_hash=$1,guest_expires_at=$2 WHERE id=$3`, tokenDigest(token), expiresAt, result["id"]); err != nil {
			writeErr(w, http.StatusInternalServerError, "UPLOAD_FAILED", "could not save guest access policy")
			return
		}
	}
	id := fmt.Sprint(result["id"])
	result["guest"] = true
	if expiresAt != nil {
		result["expires_at"] = expiresAt
	}
	if _, ok := result["short_url"]; ok {
		result["url"] = strings.TrimRight(s.cfg.PublicURL, "/") + "/s/" + token
	} else {
		result["url"] = s.resourceURL(id, "link") + "?token=" + url.QueryEscape(token)
	}
	result["name"] = header.Filename
	result["mime_type"] = mimeType
	result["size_bytes"] = len(data)
	writeJSON(w, http.StatusCreated, result)
}

func (s *server) guestRateLimited(r *http.Request, settings map[string]any) bool {
	if s.redis == nil {
		return false
	}
	limit := int64(10)
	if value, ok := settings["guest_daily_upload_limit"].(float64); ok && value > 0 {
		limit = int64(value)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || host == "" {
		host = r.RemoteAddr
	}
	key := "guest-upload:" + host + ":" + time.Now().UTC().Format("20060102")
	count, err := s.redis.Incr(r.Context(), key).Result()
	if err != nil {
		return false
	}
	if count == 1 {
		_ = s.redis.Expire(r.Context(), key, 25*time.Hour).Err()
	}
	return count > limit
}

func (s *server) guestBytesAllowed(r *http.Request, settings map[string]any, size int64) bool {
	if s.redis == nil {
		return true
	}
	limit := int64(0)
	if value, ok := settings["guest_daily_upload_bytes"].(float64); ok && value > 0 {
		limit = int64(value)
	}
	if limit == 0 {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || host == "" {
		host = r.RemoteAddr
	}
	key := "guest-upload-bytes:" + host + ":" + time.Now().UTC().Format("20060102")
	used, err := s.redis.IncrBy(r.Context(), key, size).Result()
	if err != nil {
		return false
	}
	if used == size {
		_ = s.redis.Expire(r.Context(), key, 25*time.Hour).Err()
	}
	if used > limit {
		_ = s.redis.DecrBy(r.Context(), key, size).Err()
		return false
	}
	return true
}

func (s *server) importURL(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userKey).(user)
	var in struct {
		URL            string `json:"url"`
		TeamID         string `json:"team_id"`
		StorageChannel string `json:"storage_channel"`
	}
	uploadSettings := s.effectiveUploadSettings(r.Context(), u.ID)
	if enabled, ok := uploadSettings["allow_remote_url"].(bool); ok && !enabled {
		writeErr(w, http.StatusForbidden, "REMOTE_IMPORT_DISABLED", "remote URL import is disabled")
		return
	}
	if !decodeJSON(r, &in) {
		writeErr(w, 400, "INVALID_INPUT", "url is required")
		return
	}
	if in.TeamID != "" {
		if _, ok := s.teamRole(r, in.TeamID); !ok {
			writeErr(w, 403, "TEAM_ACCESS_DENIED", "team membership is required")
			return
		}
	}
	parsed, err := url.Parse(in.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || isPrivateHost(parsed.Hostname()) {
		writeErr(w, 400, "REMOTE_URL_BLOCKED", "remote URL is not allowed")
		return
	}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, parsed.String(), nil)
	client := s.safeHTTPClient()
	client.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		if next.URL.Scheme != "http" && next.URL.Scheme != "https" || isPrivateHost(next.URL.Hostname()) {
			return errors.New("redirect target is not allowed")
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode >= 300 {
		if resp != nil {
			resp.Body.Close()
		}
		writeErr(w, 400, "REMOTE_FETCH_FAILED", "could not fetch remote URL")
		return
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, s.cfg.MaxRemoteBytes+1))
	if err != nil || int64(len(data)) > s.cfg.MaxRemoteBytes {
		writeErr(w, 413, "REMOTE_TOO_LARGE", "remote file exceeds limit")
		return
	}
	mimeType := resp.Header.Get("Content-Type")
	if semi := strings.IndexByte(mimeType, ';'); semi >= 0 {
		mimeType = mimeType[:semi]
	}
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(data)
	}
	if !allowedMime(mimeType, uploadSettings) {
		writeErr(w, 415, "UNSUPPORTED_MEDIA", "remote type is not allowed")
		return
	}
	if mimeType == "image/svg+xml" && unsafeSVG(data) {
		writeErr(w, 415, "UNSAFE_SVG", "svg contains active content")
		return
	}
	if mimeType == "image/svg+xml" && !validSVG(data) {
		writeErr(w, 415, "INVALID_SVG", "file is not a valid SVG document")
		return
	}
	if !s.reserveStore(r.Context(), u.ID, int64(len(data)), s.uploadLimit(r.Context(), u.ID)) || (in.TeamID != "" && !s.reserveTeam(r.Context(), in.TeamID, int64(len(data)))) {
		if in.TeamID != "" {
			s.releaseStore(r.Context(), u.ID, int64(len(data)))
		}
		writeErr(w, http.StatusRequestEntityTooLarge, "QUOTA_EXCEEDED", "user storage quota or file policy exceeded")
		return
	}
	name := filepath.Base(parsed.Path)
	if name == "." || name == "/" || name == "" {
		name = "remote-upload"
	}
	id, err := s.persistImage(r.Context(), u.ID, name, mimeType, data, in.StorageChannel, in.TeamID)
	if err != nil {
		s.releaseStore(r.Context(), u.ID, int64(len(data)))
		if in.TeamID != "" {
			s.releaseTeam(r.Context(), in.TeamID, int64(len(data)))
		}
		writeErr(w, 500, "IMPORT_FAILED", err.Error())
		return
	}
	if deduplicated, _ := id["deduplicated"].(bool); deduplicated {
		s.releaseStore(r.Context(), u.ID, int64(len(data)))
		if in.TeamID != "" {
			s.releaseTeam(r.Context(), in.TeamID, int64(len(data)))
		}
	}
	writeJSON(w, 201, id)
}

func (s *server) safeHTTPClient() *http.Client {
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.LookupIP(host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("host resolution failed")
		}
		for _, ip := range ips {
			a, parseErr := netip.ParseAddr(ip.String())
			if parseErr != nil || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() || a.IsUnspecified() {
				continue
			}
			conn, dialErr := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
		}
		return nil, errors.New("all resolved addresses are private or unreachable")
	}}
	return &http.Client{Timeout: 20 * time.Second, Transport: transport}
}

func (s *server) persistImage(ctx context.Context, ownerID, name, mimeType string, data []byte, storageChannel string, teamIDs ...string) (map[string]any, error) {
	teamID := ""
	if len(teamIDs) > 0 {
		teamID = teamIDs[0]
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	ext := extension(name, mimeType)
	uploadSettings := s.effectiveUploadSettings(ctx, ownerID)
	key := objectKeyForUpload(uploadSettings, ownerID, name, mimeType, data, hash, time.Now().UTC())
	backendStorage, channelID, err := s.configuredStorageChannel(strings.TrimSpace(storageChannel))
	if err != nil {
		return nil, err
	}
	backendName := s.storageBackends[channelID]
	if backendName == "" {
		backendName = "local"
	}
	stored := imagehub.StoredObject{Key: key, Backend: backendName}
	deduplicated := false
	var existingWidth, existingHeight int
	var existingDuration float64
	var existingStatus, existingProcessingError, existingThumbnailKey, existingBackend, existingChannel string
	var existingMetadata []byte
	if s.db != nil {
		err = s.db.QueryRow(ctx, `SELECT object_key,storage_backend,COALESCE(storage_channel,''),COALESCE(width,0),COALESCE(height,0),COALESCE(duration_seconds::float8,0),status,COALESCE(thumbnail_object_key,''),COALESCE(processing_error,''),metadata FROM images WHERE content_hash=$1 AND deleted_at IS NULL AND (storage_channel=$2 OR (storage_channel='' AND storage_backend=$3)) ORDER BY created_at ASC LIMIT 1`, hash, channelID, backendName).Scan(&stored.Key, &existingBackend, &existingChannel, &existingWidth, &existingHeight, &existingDuration, &existingStatus, &existingThumbnailKey, &existingProcessingError, &existingMetadata)
		if err == nil {
			stored.Backend = existingBackend
			channelID = existingChannel
			backendStorage = s.storageForChannel(existingBackend, existingChannel)
			deduplicated = true
		}
	}
	if !deduplicated {
		stored, err = backendStorage.Put(ctx, key, strings.NewReader(string(data)), int64(len(data)), mimeType)
		if err != nil {
			return nil, err
		}
	}
	if s.db == nil {
		return map[string]any{"object_key": stored.Key, "storage_backend": stored.Backend, "storage_channel": channelID, "content_hash": hash}, nil
	}
	video := inspectVideo(ctx, name, mimeType, data)
	thumbnailKey := ""
	if deduplicated {
		video = videoInspection{Status: existingStatus, Error: existingProcessingError, Metadata: map[string]any{}}
		_ = json.Unmarshal(existingMetadata, &video.Metadata)
		if existingWidth > 0 {
			video.Width = &existingWidth
		}
		if existingHeight > 0 {
			video.Height = &existingHeight
		}
		if existingDuration > 0 {
			video.Duration = &existingDuration
		}
		thumbnailKey = existingThumbnailKey
	} else if len(video.Thumbnail) > 0 {
		thumbnailKey = key + ".thumb.jpg"
		if thumbnail, thumbErr := backendStorage.Put(ctx, thumbnailKey, strings.NewReader(string(video.Thumbnail)), int64(len(video.Thumbnail)), "image/jpeg"); thumbErr != nil {
			video.Metadata["thumbnail_status"] = "failed"
			video.Metadata["thumbnail_error"] = "thumbnail storage failed"
			thumbnailKey = ""
		} else {
			thumbnailKey = thumbnail.Key
		}
	} else if strings.HasPrefix(mimeType, "image/") && mimeType != "image/svg+xml" {
		if thumbnail, thumbErr := generateImageThumbnail(data); thumbErr == nil && len(thumbnail) > 0 {
			thumbnailKey = key + ".thumb.jpg"
			if storedThumbnail, storeErr := backendStorage.Put(ctx, thumbnailKey, strings.NewReader(string(thumbnail)), int64(len(thumbnail)), "image/jpeg"); storeErr != nil {
				thumbnailKey = ""
				video.Metadata["thumbnail_status"] = "failed"
				video.Metadata["thumbnail_error"] = "thumbnail storage failed"
			} else {
				thumbnailKey = storedThumbnail.Key
				video.Metadata["thumbnail_status"] = "ready"
			}
		}
	}
	metadata, _ := json.Marshal(video.Metadata)
	var id string
	err = s.db.QueryRow(ctx, `INSERT INTO images(owner_id,team_id,object_key,storage_backend,storage_channel,original_name,content_hash,mime_type,extension,size_bytes,width,height,duration_seconds,status,thumbnail_object_key,processing_error,metadata) VALUES(NULLIF($1,'')::uuid,NULLIF($2,'')::uuid,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,NULLIF($16,''),$17) RETURNING id`, ownerID, teamID, stored.Key, stored.Backend, channelID, name, hash, mimeType, ext, len(data), video.Width, video.Height, video.Duration, video.Status, thumbnailKey, video.Error, metadata).Scan(&id)
	if err != nil {
		if !deduplicated {
			_ = backendStorage.Delete(ctx, stored.Key)
			if thumbnailKey != "" {
				_ = backendStorage.Delete(ctx, thumbnailKey)
			}
		}
		return nil, err
	}
	if strings.HasPrefix(mimeType, "video/") && video.Error != "" {
		s.enqueueMediaJob(ctx, id, "video_inspect")
	}
	result := map[string]any{"id": id, "url": s.resourceURL(id, "private"), "object_key": stored.Key, "storage_backend": stored.Backend, "storage_channel": channelID, "content_hash": hash, "status": video.Status, "metadata": video.Metadata, "deduplicated": deduplicated}
	if code, codeErr := s.createShortLink(ctx, id); codeErr == nil {
		result["short_url"] = strings.TrimRight(s.cfg.PublicURL, "/") + "/s/" + code
	}
	if thumbnailKey != "" {
		result["thumbnail_url"] = s.resourceURL(id+"/thumbnail", "private")
	}
	if video.Error != "" {
		result["processing_error"] = video.Error
	}
	return result, nil
}

const shortCodeAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func (s *server) createShortLink(ctx context.Context, imageID string) (string, error) {
	if s.db == nil {
		return "", errors.New("database is not configured")
	}
	settings := s.settings(ctx, "upload")
	if enabled, ok := settings["short_links_enabled"].(bool); ok && !enabled {
		return "", errors.New("short links are disabled")
	}
	length := 8
	if value, ok := settings["short_code_length"].(float64); ok && value >= 4 && value <= 32 {
		length = int(value)
	}
	for attempt := 0; attempt < 8; attempt++ {
		buf := make([]byte, length)
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		code := make([]byte, length)
		for i, value := range buf {
			code[i] = shortCodeAlphabet[int(value)%len(shortCodeAlphabet)]
		}
		if _, err := s.db.Exec(ctx, `INSERT INTO short_links(code,image_id) VALUES($1,$2) ON CONFLICT (image_id) DO NOTHING`, string(code), imageID); err != nil {
			continue
		}
		var saved string
		if s.db.QueryRow(ctx, `SELECT code FROM short_links WHERE image_id=$1`, imageID).Scan(&saved) == nil {
			return saved, nil
		}
	}
	return "", errors.New("could not allocate short code")
}

func (s *server) shortLink(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.NotFound(w, r)
		return
	}
	var imageID, visibility, tokenHash string
	if err := s.db.QueryRow(r.Context(), `SELECT i.id,i.visibility,COALESCE(i.link_token_hash,'') FROM short_links sl JOIN images i ON i.id=sl.image_id WHERE sl.code=$1 AND i.deleted_at IS NULL`, r.PathValue("code")).Scan(&imageID, &visibility, &tokenHash); err != nil {
		http.NotFound(w, r)
		return
	}
	location := s.resourceURL(imageID, visibility)
	if visibility == "link" && tokenHash != "" && tokenDigest(r.PathValue("code")) == tokenHash {
		location += "?token=" + url.QueryEscape(r.PathValue("code"))
	}
	http.Redirect(w, r, location, http.StatusFound)
}

func (s *server) uploadLimit(ctx context.Context, userID string) int64 {
	limit := s.cfg.MaxUploadBytes
	if settings := s.settings(ctx, "upload"); settings["max_file_bytes"] != nil {
		switch value := settings["max_file_bytes"].(type) {
		case float64:
			if value > 0 {
				limit = int64(value)
			}
		case int64:
			if value > 0 {
				limit = value
			}
		}
	}
	if s.db != nil {
		var userLimit int64
		if s.db.QueryRow(ctx, `SELECT max_file_bytes FROM users WHERE id=$1`, userID).Scan(&userLimit) == nil && userLimit > 0 && userLimit < limit {
			limit = userLimit
		}
	}
	return limit
}
func (s *server) effectiveUploadSettings(ctx context.Context, userID string) map[string]any {
	result := s.settings(ctx, "upload")
	if s.db == nil || strings.TrimSpace(userID) == "" {
		return result
	}
	var raw []byte
	if s.db.QueryRow(ctx, `SELECT upload_policy FROM users WHERE id=$1`, userID).Scan(&raw) == nil {
		var policy map[string]any
		if json.Unmarshal(raw, &policy) == nil {
			for k, v := range policy {
				result[k] = v
			}
		}
	}
	return result
}

func (s *server) canStore(ctx context.Context, userID string, size, fileLimit int64) bool {
	if size <= 0 || size > fileLimit || s.db == nil {
		return size > 0 && size <= fileLimit
	}
	var quota, used, dailyLimit int64
	if err := s.db.QueryRow(ctx, `SELECT quota_bytes,used_bytes,daily_upload_limit FROM users WHERE id=$1`, userID).Scan(&quota, &used, &dailyLimit); err != nil {
		return false
	}
	if dailyLimit > 0 {
		var uploadedToday int64
		if err := s.db.QueryRow(ctx, `SELECT count(*) FROM images WHERE owner_id=$1 AND deleted_at IS NULL AND created_at >= CURRENT_DATE`, userID).Scan(&uploadedToday); err != nil || uploadedToday >= dailyLimit {
			return false
		}
	}
	return quota <= 0 || used <= quota-size
}

// reserveStore serializes quota and daily-count checks with a row lock. The
// reservation is released if object persistence fails, preventing concurrent
// uploads from bypassing per-user limits.
func (s *server) reserveStore(ctx context.Context, userID string, size, fileLimit int64) bool {
	if size <= 0 || size > fileLimit || s.db == nil {
		return size > 0 && size <= fileLimit
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false
	}
	defer tx.Rollback(ctx)
	var quota, used, daily int64
	if err = tx.QueryRow(ctx, `SELECT quota_bytes,used_bytes,daily_upload_limit FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&quota, &used, &daily); err != nil {
		return false
	}
	if global, ok := s.settings(ctx, "upload")["daily_upload_limit"].(float64); ok && global > 0 && (daily == 0 || int64(global) < daily) {
		daily = int64(global)
	}
	if daily > 0 {
		var count int64
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM images WHERE owner_id=$1 AND deleted_at IS NULL AND created_at >= CURRENT_DATE`, userID).Scan(&count); err != nil || count >= daily {
			return false
		}
	}
	if quota > 0 && used > quota-size {
		return false
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET used_bytes=used_bytes+$1,updated_at=now() WHERE id=$2`, size, userID); err != nil {
		return false
	}
	return tx.Commit(ctx) == nil
}

func (s *server) releaseStore(ctx context.Context, userID string, size int64) {
	if s.db != nil {
		_, _ = s.db.Exec(ctx, `UPDATE users SET used_bytes=GREATEST(0,used_bytes-$1),updated_at=now() WHERE id=$2`, size, userID)
	}
}
func (s *server) reserveTeam(ctx context.Context, teamID string, size int64) bool {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false
	}
	defer tx.Rollback(ctx)
	var quota, used int64
	if err = tx.QueryRow(ctx, `SELECT quota_bytes,used_bytes FROM teams WHERE id=$1 FOR UPDATE`, teamID).Scan(&quota, &used); err != nil {
		return false
	}
	if quota > 0 && used > quota-size {
		return false
	}
	if _, err = tx.Exec(ctx, `UPDATE teams SET used_bytes=used_bytes+$1 WHERE id=$2`, size, teamID); err != nil {
		return false
	}
	return tx.Commit(ctx) == nil
}
func (s *server) releaseTeam(ctx context.Context, teamID string, size int64) {
	if s.db != nil {
		_, _ = s.db.Exec(ctx, `UPDATE teams SET used_bytes=GREATEST(0,used_bytes-$1) WHERE id=$2`, size, teamID)
	}
}
func (s *server) listImages(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userKey).(user)
	if s.db == nil {
		writeJSON(w, 200, []any{})
		return
	}
	rows, err := s.db.Query(r.Context(), `SELECT id,original_name,mime_type,size_bytes,visibility,status,thumbnail_object_key,metadata,created_at,COALESCE(team_id::text,'') FROM images WHERE (owner_id=$1 OR team_id IN (SELECT team_id FROM team_members WHERE user_id=$1)) AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 100`, u.ID)
	if err != nil {
		writeErr(w, 500, "IMAGES_FAILED", "could not load images")
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var id, name, mt, visibility, status, thumbnailKey, teamID string
		var size int64
		var metadata []byte
		var created time.Time
		if rows.Scan(&id, &name, &mt, &size, &visibility, &status, &thumbnailKey, &metadata, &created, &teamID) == nil {
			var parsed any
			_ = json.Unmarshal(metadata, &parsed)
			item := map[string]any{"id": id, "name": name, "mime_type": mt, "size_bytes": size, "visibility": visibility, "status": status, "metadata": parsed, "created_at": created, "team_id": teamID, "url": s.resourceURL(id, visibility)}
			if thumbnailKey != "" {
				item["thumbnail_url"] = s.resourceURL(id+"/thumbnail", visibility)
			}
			result = append(result, item)
		}
	}
	writeJSON(w, 200, result)
}

func (s *server) updateImage(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userKey).(user)
	var in struct {
		Visibility string `json:"visibility"`
	}
	if !decodeJSON(r, &in) || (in.Visibility != "private" && in.Visibility != "public" && in.Visibility != "link") {
		writeErr(w, http.StatusBadRequest, "INVALID_VISIBILITY", "visibility must be private, public, or link")
		return
	}
	shareToken := ""
	if in.Visibility == "link" {
		shareToken, _ = newOpaqueToken()
	}
	var id, visibility string
	err := s.db.QueryRow(r.Context(), `UPDATE images SET visibility=$1,link_token_hash=CASE WHEN $1='link' THEN $2 ELSE NULL END WHERE id=$3 AND owner_id=$4 AND deleted_at IS NULL RETURNING id,visibility`, in.Visibility, func() string {
		if shareToken == "" {
			return ""
		}
		return tokenDigest(shareToken)
	}(), r.PathValue("id"), u.ID).Scan(&id, &visibility)
	if err != nil {
		writeErr(w, http.StatusNotFound, "IMAGE_NOT_FOUND", "image not found")
		return
	}
	shareURL := s.resourceURL(id, visibility)
	if shareToken != "" {
		shareURL += "?token=" + url.QueryEscape(shareToken)
	}
	if visibility == "public" {
		if purgeErr := s.purgeCDN(r.Context(), shareURL); purgeErr != nil {
			s.log.Warn("cdn purge failed", "image_id", id, "error", purgeErr)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "visibility": visibility, "url": shareURL})
}

func (s *server) deleteImage(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userKey).(user)
	if s.db == nil {
		writeErr(w, 503, "DATABASE_UNAVAILABLE", "database is not configured")
		return
	}
	var key, backend, channelID, thumbnail, teamID string
	var size int64
	err := s.db.QueryRow(r.Context(), `UPDATE images SET deleted_at=now(),status='deleted',deleted_by=$3 WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL RETURNING object_key,storage_backend,COALESCE(storage_channel,''),size_bytes,COALESCE(thumbnail_object_key,''),COALESCE(team_id::text,'')`, r.PathValue("id"), u.ID, u.ID).Scan(&key, &backend, &channelID, &size, &thumbnail, &teamID)
	if err != nil {
		writeErr(w, 404, "IMAGE_NOT_FOUND", "image not found")
		return
	}
	var references int
	_ = s.db.QueryRow(r.Context(), `SELECT count(*) FROM images WHERE object_key=$1 AND deleted_at IS NULL`, key).Scan(&references)
	if references == 0 {
		_ = s.storageForChannel(backend, channelID).Delete(r.Context(), key)
		if thumbnail != "" {
			_ = s.storageForChannel(backend, channelID).Delete(r.Context(), thumbnail)
		}
	}
	if purgeErr := s.purgeCDN(r.Context(), s.resourceURL(r.PathValue("id"), "public")); purgeErr != nil {
		s.log.Warn("cdn purge failed", "image_id", r.PathValue("id"), "error", purgeErr)
	}
	_, _ = s.db.Exec(r.Context(), `UPDATE users SET used_bytes=GREATEST(0,used_bytes-$1),updated_at=now() WHERE id=$2`, size, u.ID)
	if teamID != "" {
		s.releaseTeam(r.Context(), teamID, size)
	}
	writeJSON(w, 200, map[string]any{"ok": true, "storage_backend": backend, "storage_channel": channelID, "released_bytes": size})
}

func (s *server) media(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.NotFound(w, r)
		return
	}
	if !s.mediaHostAllowed(r.Context(), r.Host) {
		http.Error(w, "custom domain is not verified", http.StatusMisdirectedRequest)
		return
	}
	var key, backend, channelID, mt, visibility, linkTokenHash, guestTokenHash, teamID string
	var ownerID string
	var guestExpiresAt *time.Time
	err := s.db.QueryRow(r.Context(), `SELECT object_key,storage_backend,COALESCE(storage_channel,''),mime_type,visibility,COALESCE(owner_id::text,''),COALESCE(link_token_hash,''),COALESCE(link_token_hash,''),guest_expires_at,COALESCE(team_id::text,'') FROM images WHERE id=$1 AND deleted_at IS NULL`, r.PathValue("id")).Scan(&key, &backend, &channelID, &mt, &visibility, &ownerID, &linkTokenHash, &guestTokenHash, &guestExpiresAt, &teamID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	guestTokenValid := guestTokenHash != "" && tokenDigest(r.URL.Query().Get("token")) == guestTokenHash && (guestExpiresAt == nil || guestExpiresAt.After(time.Now()))
	if visibility == "link" && ((tokenDigest(r.URL.Query().Get("token")) == linkTokenHash && linkTokenHash != "") || guestTokenValid) { /* signed link */
	} else if visibility != "public" {
		u, ok := s.currentUser(r)
		teamMember := false
		if ok && teamID != "" {
			_ = s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM team_members WHERE team_id=$1 AND user_id=$2)`, teamID, u.ID).Scan(&teamMember)
		}
		if !ok || (u.ID != ownerID && !teamMember) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}
	reader, err := s.storageForChannel(backend, channelID).Open(r.Context(), key)
	if err != nil {
		http.Error(w, "storage unavailable", 502)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", mt)
	w.Header().Set("Accept-Ranges", "bytes")
	if visibility == "public" {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "private, no-store")
	}
	if seeker, ok := reader.(io.ReadSeeker); ok {
		if size, sizeErr := seeker.Seek(0, io.SeekEnd); sizeErr == nil && size >= 0 {
			_, _ = seeker.Seek(0, io.SeekStart)
			w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
			if rawRange := strings.TrimSpace(r.Header.Get("Range")); strings.HasPrefix(rawRange, "bytes=") {
				start, end, valid := parseByteRange(strings.TrimPrefix(rawRange, "bytes="), size)
				if !valid {
					w.Header().Set("Content-Range", "bytes */"+strconv.FormatInt(size, 10))
					w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
					return
				}
				if _, err = seeker.Seek(start, io.SeekStart); err != nil {
					http.Error(w, "storage unavailable", http.StatusBadGateway)
					return
				}
				length := end - start + 1
				w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
				w.Header().Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10)+"/"+strconv.FormatInt(size, 10))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = io.CopyN(w, seeker, length)
				return
			}
		}
	}
	_, _ = io.Copy(w, reader)
	_ = backend
}

func parseByteRange(raw string, size int64) (int64, int64, bool) {
	if size <= 0 || strings.Contains(raw, ",") {
		return 0, 0, false
	}
	parts := strings.SplitN(strings.TrimSpace(raw), "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	if strings.TrimSpace(parts[0]) == "" {
		suffix, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil || suffix <= 0 {
			return 0, 0, false
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, size - 1, true
	}
	start, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false
	}
	end := size - 1
	if strings.TrimSpace(parts[1]) != "" {
		end, err = strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil || end < start {
			return 0, 0, false
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end, true
}

func (s *server) mediaHostAllowed(ctx context.Context, host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	public, err := url.Parse(s.cfg.PublicURL)
	if err == nil && strings.EqualFold(host, public.Hostname()) {
		return true
	}
	var verified bool
	return s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM custom_domains WHERE lower(domain)=$1 AND verified_at IS NOT NULL)`, host).Scan(&verified) == nil && verified
}

func (s *server) resourceURL(path, visibility string) string {
	base := strings.TrimRight(s.cfg.PublicURL, "/")
	if visibility == "public" {
		if configured, ok := s.settings(context.Background(), "storage")["cdn_base_url"].(string); ok && strings.TrimSpace(configured) != "" {
			base = strings.TrimRight(strings.TrimSpace(configured), "/")
		}
	}
	return base + "/media/" + strings.TrimLeft(path, "/")
}
func (s *server) publicConfig(w http.ResponseWriter, _ *http.Request) {
	registration := s.settings(context.Background(), "registration")
	upload := s.settings(context.Background(), "upload")
	site := s.settings(context.Background(), "site")
	if upload["max_file_bytes"] == nil {
		upload["max_file_bytes"] = s.cfg.MaxUploadBytes
	}
	writeJSON(w, 200, map[string]any{"public_url": s.cfg.PublicURL, "default_language": site["default_language"], "registration": redactSetting("registration", registration), "upload": map[string]any{"max_file_bytes": upload["max_file_bytes"], "anonymous_enabled": upload["anonymous_enabled"], "guest_retention_days": upload["guest_retention_days"], "allowed_mime_types": upload["allowed_mime_types"], "allow_video": upload["allow_video"]}})
}

func (s *server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := s.currentUser(r)
		if !ok {
			writeErr(w, 401, "UNAUTHORIZED", "login required")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	}
}
func (s *server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		u := r.Context().Value(userKey).(user)
		if u.Role != "admin" {
			writeErr(w, 403, "FORBIDDEN", "administrator role required")
			return
		}
		next(w, r)
	})
}
func (s *server) currentUser(r *http.Request) (user, bool) {
	if s.redis == nil || s.db == nil {
		return user{}, false
	}
	token := ""
	if c, err := r.Cookie("ih_session"); err == nil {
		token = c.Value
	}
	if token == "" {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	if token == "" {
		return user{}, false
	}
	id, err := s.redis.Get(r.Context(), "session:"+token).Result()
	if err != nil {
		return user{}, false
	}
	var u user
	if s.db.QueryRow(r.Context(), `SELECT id,email,role FROM users WHERE id=$1 AND status='active'`, id).Scan(&u.ID, &u.Email, &u.Role) != nil {
		return user{}, false
	}
	return u, true
}
func (s *server) bootstrapAdmin(ctx context.Context) error {
	email, password := os.Getenv("BOOTSTRAP_ADMIN_EMAIL"), os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	if email == "" || password == "" {
		return nil
	}
	var count int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil || count > 0 {
		return err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO users(email,password_hash,display_name,role,email_verified_at) VALUES($1,$2,'Administrator','admin',now())`, strings.ToLower(email), hash)
	return err
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	digest := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return "$argon2id$v=19$m=65536,t=3,p=2$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(digest), nil
}
func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	expected, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}
func decodeJSON(r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(dst) == nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeErr(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiError{code, message})
}
func allowedMime(mt string, settings map[string]any) bool {
	if mt == "" {
		return false
	}
	if strings.HasPrefix(mt, "image/svg") {
		if enabled, ok := settings["allow_svg"].(bool); ok && !enabled {
			return false
		}
	}
	if strings.HasPrefix(mt, "video/") {
		if enabled, ok := settings["allow_video"].(bool); ok && !enabled {
			return false
		}
	}
	if list, ok := settings["allowed_mime_types"].([]any); ok {
		for _, item := range list {
			if mt == fmt.Sprint(item) {
				return true
			}
		}
		return false
	}
	return strings.HasPrefix(mt, "image/") || strings.HasPrefix(mt, "video/")
}
func unsafeSVG(data []byte) bool {
	lower := strings.ToLower(string(data))
	for _, bad := range []string{"<script", "javascript:", "vbscript:", "onload=", "onerror=", "onmouseover=", "<iframe", "<foreignobject", "<!entity", "<!doctype", "<object", "<embed"} {
		if strings.Contains(lower, bad) {
			return true
		}
	}
	return strings.ContainsRune(lower, '\x00')
}
func validSVG(data []byte) bool {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	decoder.Strict = true
	for {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		if start, ok := token.(xml.StartElement); ok {
			return strings.EqualFold(start.Name.Local, "svg")
		}
	}
}

var extPattern = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func extension(name, mt string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		if e, _ := mime.ExtensionsByType(mt); len(e) > 0 {
			ext = e[0]
		}
	}
	ext = extPattern.ReplaceAllString(ext, "")
	if ext == "" {
		ext = "bin"
	}
	return "." + strings.TrimPrefix(ext, ".")
}
func isPrivateHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return true
	}
	for _, ip := range ips {
		if addr, err := netip.ParseAddr(ip.String()); err == nil && (addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsUnspecified()) {
			return true
		}
	}
	return false
}

func (s *server) frontend(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/media/") {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.cfg.WebDir, filepath.Clean(r.URL.Path))
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		http.ServeFile(w, r, path)
		return
	}
	index := filepath.Join(s.cfg.WebDir, "index.html")
	if _, err := os.Stat(index); err == nil {
		http.ServeFile(w, r, index)
		return
	}
	writeJSON(w, 200, map[string]string{"service": "imagehub", "status": "frontend-not-built"})
}

// Keep pgx/sql imports in the generated backend dependency graph for future transactional repositories.
var _ = pgx.ErrNoRows
var _ = sql.ErrNoRows
