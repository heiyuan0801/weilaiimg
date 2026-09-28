package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const emailTokenTTL = 30 * time.Minute

func pkceChallenge(verifier string) string {
	d := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(d[:])
}

func newOpaqueToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func tokenDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

// issueEmailToken stores a one-time token before attempting delivery. A failed
// SMTP delivery leaves the token persisted so an operator can resend it later.
func (s *server) issueEmailToken(ctx context.Context, userID, email, kind string) (string, error) {
	if s.db == nil {
		return "not_configured", errors.New("database is not configured")
	}
	if s.emailRateLimited(ctx, kind, email, 5, 10*time.Minute) {
		return "rate_limited", errors.New("email delivery rate limit exceeded")
	}
	token, err := newOpaqueToken()
	if err != nil {
		return "failed", err
	}
	if _, err = s.db.Exec(ctx, `UPDATE email_tokens SET used_at=now() WHERE user_id=$1 AND kind=$2 AND used_at IS NULL`, userID, kind); err != nil {
		return "failed", err
	}
	if _, err = s.db.Exec(ctx, `INSERT INTO email_tokens(token_hash,user_id,kind,expires_at) VALUES($1,$2,$3,$4)`, tokenDigest(token), userID, kind, time.Now().Add(emailTokenTTL)); err != nil {
		return "failed", err
	}
	settings := s.settings(ctx, "email")
	path := "/verify-email?token=" + url.QueryEscape(token)
	subject := "Verify your ImageHub email"
	if kind == "reset" {
		path = "/reset-password?token=" + url.QueryEscape(token)
		subject = "Reset your ImageHub password"
	}
	text := "Use this link to continue: " + strings.TrimRight(s.cfg.PublicURL, "/") + path + "\n\nThis link expires in 30 minutes."
	_ = settings // settings are loaded by sendEmail; keep token generation independent of SMTP config.
	delivery, err := s.sendEmail(ctx, email, subject, text)
	if err != nil {
		return delivery, err
	}
	return delivery, nil
}

func (s *server) verifyEmail(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeErr(w, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", "database is not configured")
		return
	}
	rawToken := strings.TrimSpace(r.URL.Query().Get("token"))
	if rawToken == "" {
		writeErr(w, http.StatusBadRequest, "TOKEN_REQUIRED", "verification token is required")
		return
	}
	if s.authAttemptLimited(r.Context(), "verify", r.RemoteAddr, 20, 10*time.Minute) {
		writeErr(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many verification attempts")
		return
	}
	var userID string
	var tokenID string
	err := s.db.QueryRow(r.Context(), `SELECT token_hash,user_id FROM email_tokens WHERE token_hash=$1 AND kind='verify' AND used_at IS NULL AND expires_at>now()`, tokenDigest(rawToken)).Scan(&tokenID, &userID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "TOKEN_INVALID", "verification token is invalid or expired")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeErr(w, 500, "VERIFY_FAILED", "could not verify email")
		return
	}
	defer tx.Rollback(r.Context())
	result, updateErr := tx.Exec(r.Context(), `UPDATE users SET status=CASE WHEN status='pending' THEN 'active' ELSE status END,email_verified_at=now(),updated_at=now() WHERE id=$1 AND status<>'disabled'`, userID)
	if updateErr != nil || result.RowsAffected() == 0 {
		writeErr(w, 500, "VERIFY_FAILED", "could not verify email")
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE email_tokens SET used_at=now() WHERE token_hash=$1`, tokenID); err != nil {
		writeErr(w, 500, "VERIFY_FAILED", "could not consume verification token")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeErr(w, 500, "VERIFY_FAILED", "could not verify email")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"verified": true, "user_id": userID})
}

func (s *server) resetPassword(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeErr(w, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", "database is not configured")
		return
	}
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decodeJSON(r, &in) || strings.TrimSpace(in.Token) == "" || len(in.Password) < 10 {
		writeErr(w, http.StatusBadRequest, "INVALID_INPUT", "token and a password of at least 10 characters are required")
		return
	}
	if s.authAttemptLimited(r.Context(), "reset", r.RemoteAddr, 20, 10*time.Minute) {
		writeErr(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many password reset attempts")
		return
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		writeErr(w, 500, "HASH_FAILED", "could not create password")
		return
	}
	var userID, tokenID string
	err = s.db.QueryRow(r.Context(), `SELECT token_hash,user_id FROM email_tokens WHERE token_hash=$1 AND kind='reset' AND used_at IS NULL AND expires_at>now()`, tokenDigest(in.Token)).Scan(&tokenID, &userID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "TOKEN_INVALID", "reset token is invalid or expired")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeErr(w, 500, "RESET_FAILED", "could not reset password")
		return
	}
	defer tx.Rollback(r.Context())
	result, updateErr := tx.Exec(r.Context(), `UPDATE users SET password_hash=$1,status=CASE WHEN status='pending' THEN 'active' ELSE status END,email_verified_at=COALESCE(email_verified_at,now()),updated_at=now() WHERE id=$2 AND status<>'disabled'`, hash, userID)
	if updateErr != nil || result.RowsAffected() == 0 {
		writeErr(w, 500, "RESET_FAILED", "could not reset password")
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE email_tokens SET used_at=now() WHERE token_hash=$1`, tokenID); err != nil {
		writeErr(w, 500, "RESET_FAILED", "could not consume reset token")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeErr(w, 500, "RESET_FAILED", "could not reset password")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reset": true})
}

func (s *server) emailRateLimited(ctx context.Context, kind, email string, limit int64, window time.Duration) bool {
	if s.redis == nil {
		return false
	}
	key := "email-rate:" + kind + ":" + tokenDigest(strings.ToLower(strings.TrimSpace(email)))
	count, err := s.redis.Incr(ctx, key).Result()
	if err != nil {
		return false
	}
	if count == 1 {
		_ = s.redis.Expire(ctx, key, window).Err()
	}
	return count > limit
}

func (s *server) authAttemptLimited(ctx context.Context, kind, remote string, limit int64, window time.Duration) bool {
	if s.redis == nil {
		return false
	}
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}
	key := "auth-attempt:" + kind + ":" + tokenDigest(remote)
	count, err := s.redis.Incr(ctx, key).Result()
	if err != nil {
		return false
	}
	if count == 1 {
		_ = s.redis.Expire(ctx, key, window).Err()
	}
	return count > limit
}

func (s *server) sendEmail(ctx context.Context, recipient, subject, text string) (string, error) {
	settings := s.settings(ctx, "email")
	host, _ := settings["smtp_host"].(string)
	from, _ := settings["from_address"].(string)
	if strings.TrimSpace(host) == "" || strings.TrimSpace(from) == "" {
		return "not_configured", nil
	}
	port := 587
	switch value := settings["smtp_port"].(type) {
	case float64:
		if value > 0 {
			port = int(value)
		}
	case int:
		if value > 0 {
			port = value
		}
	case string:
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			port = parsed
		}
	}
	username, _ := settings["smtp_username"].(string)
	password, _ := settings["smtp_password"].(string)
	security, _ := settings["smtp_security"].(string)
	security = strings.ToLower(strings.TrimSpace(security))
	if security == "" {
		security = "starttls"
	}
	fromName, _ := settings["from_name"].(string)
	if strings.ContainsAny(recipient+from+fromName, "\r\n") {
		return "failed", errors.New("invalid email address")
	}
	if !strings.Contains(recipient, "@") || !strings.Contains(from, "@") {
		return "failed", errors.New("invalid email address")
	}
	message := "From: " + formatMailbox(fromName, from) + "\r\n" + "To: " + recipient + "\r\n" + "Subject: " + strings.ReplaceAll(subject, "\n", " ") + "\r\n" + "MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + text + "\r\n"
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var client *smtp.Client
	if security == "tls" {
		conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(host, strconv.Itoa(port)), &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err != nil {
			return "failed", err
		}
		client, err = smtp.NewClient(conn, host)
		if err != nil {
			conn.Close()
			return "failed", err
		}
	} else {
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			return "failed", err
		}
		client, err = smtp.NewClient(conn, host)
		if err != nil {
			conn.Close()
			return "failed", err
		}
		if security == "starttls" {
			if err = client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
				client.Close()
				return "failed", err
			}
		}
	}
	defer client.Close()
	if username != "" {
		if err := client.Auth(smtp.PlainAuth("", username, password, host)); err != nil {
			return "failed", err
		}
	}
	if err := client.Mail(from); err != nil {
		return "failed", err
	}
	if err := client.Rcpt(recipient); err != nil {
		return "failed", err
	}
	writer, err := client.Data()
	if err != nil {
		return "failed", err
	}
	if _, err = io.WriteString(writer, message); err != nil {
		writer.Close()
		return "failed", err
	}
	if err = writer.Close(); err != nil {
		return "failed", err
	}
	if err = client.Quit(); err != nil {
		return "failed", err
	}
	return "sent", nil
}

func formatMailbox(name, address string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return address
	}
	return name + " <" + address + ">"
}

// OIDC provider management and a minimal authorization-code flow. Discovery,
// token exchange, and userinfo are intentionally explicit so deployments can
// audit the outbound endpoints and add PKCE/provider-specific validation later.
type oidcDiscovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
	Issuer                string `json:"issuer"`
}

type oidcProvider struct {
	ID, Name, IssuerURL, ClientID, Secret string
	Scopes                                []string
	Enabled, AutoCreate                   bool
}

func (s *server) loadOIDCProvider(ctx context.Context, id string) (oidcProvider, error) {
	var p oidcProvider
	var encrypted string
	err := s.db.QueryRow(ctx, `SELECT id,name,issuer_url,client_id,client_secret_encrypted,scopes,enabled,auto_create_users FROM oidc_providers WHERE id=$1`, id).Scan(&p.ID, &p.Name, &p.IssuerURL, &p.ClientID, &encrypted, &p.Scopes, &p.Enabled, &p.AutoCreate)
	if err != nil {
		return p, err
	}
	p.Secret, err = decryptSecret(s.cfg.AppSecret, encrypted)
	return p, err
}

func (s *server) oidcStart(w http.ResponseWriter, r *http.Request) {
	if s.db == nil || s.redis == nil {
		writeErr(w, 503, "OIDC_UNAVAILABLE", "OIDC requires database and redis")
		return
	}
	p, err := s.loadOIDCProvider(r.Context(), r.PathValue("id"))
	if err != nil || !p.Enabled {
		writeErr(w, 404, "OIDC_PROVIDER_NOT_FOUND", "OIDC provider is unavailable")
		return
	}
	discovery, err := s.oidcDiscovery(r.Context(), p.IssuerURL)
	if err != nil || discovery.AuthorizationEndpoint == "" {
		writeErr(w, 502, "OIDC_DISCOVERY_FAILED", "could not load OIDC provider metadata")
		return
	}
	state, err := newOpaqueToken()
	if err != nil {
		writeErr(w, 500, "OIDC_STATE_FAILED", "could not create OIDC state")
		return
	}
	verifier, err := newOpaqueToken()
	if err != nil {
		writeErr(w, 500, "OIDC_STATE_FAILED", "could not create PKCE verifier")
		return
	}
	nonce, err := newOpaqueToken()
	if err != nil {
		writeErr(w, 500, "OIDC_STATE_FAILED", "could not create OIDC nonce")
		return
	}
	statePayload, _ := json.Marshal(map[string]string{"provider_id": p.ID, "verifier": verifier, "nonce": nonce})
	if err = s.redis.Set(r.Context(), "oidc_state:"+state, statePayload, 10*time.Minute).Err(); err != nil {
		writeErr(w, 503, "OIDC_STATE_FAILED", "could not store OIDC state")
		return
	}
	scopes := strings.Join(p.Scopes, " ")
	if scopes == "" {
		scopes = "openid profile email"
	}
	callback := strings.TrimRight(s.cfg.PublicURL, "/") + "/api/v1/auth/oidc/" + url.PathEscape(p.ID) + "/callback"
	authURL, _ := url.Parse(discovery.AuthorizationEndpoint)
	query := authURL.Query()
	query.Set("response_type", "code")
	query.Set("client_id", p.ClientID)
	query.Set("redirect_uri", callback)
	query.Set("scope", scopes)
	query.Set("state", state)
	query.Set("nonce", nonce)
	query.Set("code_challenge", pkceChallenge(verifier))
	query.Set("code_challenge_method", "S256")
	authURL.RawQuery = query.Encode()
	http.SetCookie(w, &http.Cookie{Name: "ih_oidc_state", Value: state, Path: "/", HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	http.Redirect(w, r, authURL.String(), http.StatusFound)
}

func (s *server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if s.db == nil || s.redis == nil {
		writeErr(w, 503, "OIDC_UNAVAILABLE", "OIDC requires database and redis")
		return
	}
	if oauthErr := r.URL.Query().Get("error"); oauthErr != "" {
		writeErr(w, 400, "OIDC_DENIED", oauthErr)
		return
	}
	state, code := r.URL.Query().Get("state"), r.URL.Query().Get("code")
	if state == "" || code == "" {
		writeErr(w, 400, "OIDC_CALLBACK_INVALID", "state and code are required")
		return
	}
	stateCookie, _ := r.Cookie("ih_oidc_state")
	if stateCookie == nil || subtle.ConstantTimeCompare([]byte(stateCookie.Value), []byte(state)) != 1 {
		writeErr(w, 400, "OIDC_STATE_INVALID", "OIDC browser state does not match")
		return
	}
	stateRaw, err := s.redis.GetDel(r.Context(), "oidc_state:"+state).Result()
	if err != nil {
		writeErr(w, 400, "OIDC_STATE_INVALID", "OIDC state is invalid or expired")
		return
	}
	var stateData struct {
		ProviderID string `json:"provider_id"`
		Verifier   string `json:"verifier"`
		Nonce      string `json:"nonce"`
	}
	if json.Unmarshal([]byte(stateRaw), &stateData) != nil || stateData.ProviderID != r.PathValue("id") || stateData.Verifier == "" || stateData.Nonce == "" {
		writeErr(w, 400, "OIDC_STATE_INVALID", "OIDC state does not match provider")
		return
	}
	providerID := stateData.ProviderID
	http.SetCookie(w, &http.Cookie{Name: "ih_oidc_state", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	p, err := s.loadOIDCProvider(r.Context(), providerID)
	if err != nil || !p.Enabled {
		writeErr(w, 404, "OIDC_PROVIDER_NOT_FOUND", "OIDC provider is unavailable")
		return
	}
	discovery, err := s.oidcDiscovery(r.Context(), p.IssuerURL)
	if err != nil || discovery.TokenEndpoint == "" {
		writeErr(w, 502, "OIDC_DISCOVERY_FAILED", "could not load OIDC provider metadata")
		return
	}
	callback := strings.TrimRight(s.cfg.PublicURL, "/") + "/api/v1/auth/oidc/" + url.PathEscape(p.ID) + "/callback"
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {callback}, "client_id": {p.ClientID}, "client_secret": {p.Secret}, "code_verifier": {stateData.Verifier}}
	request, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, discovery.TokenEndpoint, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		writeErr(w, 502, "OIDC_TOKEN_FAILED", "could not exchange authorization code")
		return
	}
	defer response.Body.Close()
	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	if response.StatusCode >= 300 || json.NewDecoder(response.Body).Decode(&tokenResponse) != nil || tokenResponse.AccessToken == "" {
		writeErr(w, 502, "OIDC_TOKEN_FAILED", "OIDC provider rejected authorization code")
		return
	}
	if tokenResponse.IDToken != "" {
		if err := s.validateOIDCIDToken(r.Context(), discovery, p, tokenResponse.IDToken, stateData.Nonce); err != nil {
			writeErr(w, 403, "OIDC_TOKEN_INVALID", err.Error())
			return
		}
	}
	if discovery.UserinfoEndpoint == "" {
		writeErr(w, 501, "OIDC_USERINFO_TODO", "provider has no userinfo endpoint")
		return
	}
	userinfoReq, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, discovery.UserinfoEndpoint, nil)
	userinfoReq.Header.Set("Authorization", "Bearer "+tokenResponse.AccessToken)
	userinfoResp, err := (&http.Client{Timeout: 15 * time.Second}).Do(userinfoReq)
	if err != nil {
		writeErr(w, 502, "OIDC_USERINFO_FAILED", "could not load OIDC user profile")
		return
	}
	defer userinfoResp.Body.Close()
	var profile struct {
		Subject           string `json:"sub"`
		Email             string `json:"email"`
		EmailVerified     bool   `json:"email_verified"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if userinfoResp.StatusCode >= 300 || json.NewDecoder(userinfoResp.Body).Decode(&profile) != nil || profile.Subject == "" || !strings.Contains(profile.Email, "@") || !profile.EmailVerified {
		writeErr(w, 502, "OIDC_USERINFO_FAILED", "OIDC profile did not include a valid subject and email")
		return
	}
	userID, err := s.upsertOIDCUser(r.Context(), p, profile.Subject, profile.Email, profile.Name, profile.PreferredUsername)
	if err != nil {
		writeErr(w, 403, "OIDC_USER_FAILED", err.Error())
		return
	}
	if err = s.createSession(w, r, userID); err != nil {
		writeErr(w, 503, "SESSION_FAILED", "could not create session")
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		writeJSON(w, 200, map[string]any{"user_id": userID, "provider_id": p.ID})
		return
	}
	http.Redirect(w, r, strings.TrimRight(s.cfg.PublicURL, "/")+"/?oidc=success", http.StatusFound)
}

func (s *server) oidcDiscovery(ctx context.Context, issuer string) (oidcDiscovery, error) {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	if issuer == "" {
		return oidcDiscovery{}, errors.New("issuer is empty")
	}
	parsedIssuer, parseErr := url.Parse(issuer)
	if parseErr != nil || (parsedIssuer.Scheme != "https" && parsedIssuer.Scheme != "http") || parsedIssuer.Hostname() == "" || isPrivateHost(parsedIssuer.Hostname()) {
		return oidcDiscovery{}, errors.New("OIDC issuer host is not allowed")
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, issuer+"/.well-known/openid-configuration", nil)
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		return oidcDiscovery{}, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return oidcDiscovery{}, fmt.Errorf("discovery returned %s", response.Status)
	}
	var result oidcDiscovery
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
		return result, err
	}
	if result.Issuer != "" && strings.TrimRight(result.Issuer, "/") != issuer {
		return result, errors.New("discovery issuer does not match configured issuer")
	}
	if result.AuthorizationEndpoint == "" || result.TokenEndpoint == "" || result.JWKSURI == "" {
		return result, errors.New("OIDC discovery metadata is incomplete")
	}
	return result, nil
}

func (s *server) upsertOIDCUser(ctx context.Context, p oidcProvider, subject, email, name, username string) (string, error) {
	var id string
	if err := s.db.QueryRow(ctx, `SELECT user_id FROM oidc_subjects WHERE provider_id=$1 AND subject=$2`, p.ID, subject).Scan(&id); err == nil {
		var status string
		if err := s.db.QueryRow(ctx, `SELECT status FROM users WHERE id=$1`, id).Scan(&status); err != nil || status == "disabled" {
			return "", errors.New("OIDC account is disabled")
		}
		_, _ = s.db.Exec(ctx, `UPDATE users SET email_verified_at=COALESCE(email_verified_at,now()),updated_at=now() WHERE id=$1`, id)
		return id, nil
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if err := s.db.QueryRow(ctx, `SELECT id FROM users WHERE email=$1`, email).Scan(&id); err != nil {
		if !p.AutoCreate {
			return "", errors.New("OIDC account is not linked and automatic account creation is disabled")
		}
		display := strings.TrimSpace(name)
		if display == "" {
			display = strings.TrimSpace(username)
		}
		if err = s.db.QueryRow(ctx, `INSERT INTO users(email,display_name,status,email_verified_at) VALUES($1,$2,'active',now()) RETURNING id`, email, display).Scan(&id); err != nil {
			return "", err
		}
	} else {
		var status string
		if err = s.db.QueryRow(ctx, `SELECT status FROM users WHERE id=$1`, id).Scan(&status); err != nil {
			return "", err
		}
		if status == "disabled" {
			return "", errors.New("OIDC account is disabled")
		}
		if _, err = s.db.Exec(ctx, `UPDATE users SET email_verified_at=COALESCE(email_verified_at,now()),updated_at=now() WHERE id=$1`, id); err != nil {
			return "", err
		}
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO oidc_subjects(provider_id,subject,user_id) VALUES($1,$2,$3) ON CONFLICT(provider_id,subject) DO UPDATE SET user_id=EXCLUDED.user_id`, p.ID, subject, id); err != nil {
		return "", err
	}
	return id, nil
}

func (s *server) createSession(w http.ResponseWriter, r *http.Request, userID string) error {
	if s.redis == nil {
		return errors.New("redis unavailable")
	}
	token := requestID() + requestID()
	if err := s.redis.Set(r.Context(), "session:"+token, userID, s.cfg.SessionTTL).Err(); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: "ih_session", Value: token, Path: "/", HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: int(s.cfg.SessionTTL.Seconds())})
	return nil
}

func encryptSecret(appSecret, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if appSecret == "" {
		return "plain:" + value, nil
	}
	key := sha256.Sum256([]byte(appSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return "enc:" + base64.RawStdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(value), nil)), nil
}

func decryptSecret(appSecret, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "plain:") {
		return strings.TrimPrefix(value, "plain:"), nil
	}
	if !strings.HasPrefix(value, "enc:") {
		return value, nil
	}
	if appSecret == "" {
		return "", errors.New("APP_SECRET is required to decrypt OIDC client secret")
	}
	key := sha256.Sum256([]byte(appSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	data, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, "enc:"))
	if err != nil || len(data) < gcm.NonceSize() {
		return "", errors.New("invalid encrypted secret")
	}
	plaintext, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	if err != nil {
		return "", errors.New("invalid encrypted secret")
	}
	return string(plaintext), nil
}

func mustOpen(gcm cipher.AEAD, data []byte) []byte {
	plaintext, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	if err != nil {
		return nil
	}
	return plaintext
}

// Keep slog available to callers that use this file with older generated code.
var _ = slog.LevelInfo
var _ = pgx.ErrNoRows
