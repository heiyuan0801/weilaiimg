package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (s *server) teamRole(r *http.Request, teamID string) (string, bool) {
	u, ok := r.Context().Value(userKey).(user)
	if !ok || s.db == nil {
		return "", false
	}
	var role string
	if err := s.db.QueryRow(r.Context(), `SELECT role FROM team_members WHERE team_id=$1 AND user_id=$2`, teamID, u.ID).Scan(&role); err != nil {
		return "", false
	}
	return role, true
}

func canManageTeam(role string) bool {
	return role == "owner" || role == "admin"
}

func (s *server) listTeamMembers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.teamRole(r, r.PathValue("id")); !ok {
		writeErr(w, http.StatusForbidden, "TEAM_ACCESS_DENIED", "team membership is required")
		return
	}
	rows, err := s.db.Query(r.Context(), `SELECT u.id,u.email,u.display_name,tm.role,tm.created_at FROM team_members tm JOIN users u ON u.id=tm.user_id WHERE tm.team_id=$1 ORDER BY tm.created_at`, r.PathValue("id"))
	if err != nil {
		writeErr(w, 500, "TEAM_MEMBERS_FAILED", "could not load team members")
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var id, email, name, role string
		var created time.Time
		if rows.Scan(&id, &email, &name, &role, &created) == nil {
			result = append(result, map[string]any{"id": id, "user_id": id, "email": email, "display_name": name, "role": role, "created_at": created})
		}
	}
	writeJSON(w, 200, result)
}

func (s *server) listTeamInvitations(w http.ResponseWriter, r *http.Request) {
	if role, ok := s.teamRole(r, r.PathValue("id")); !ok || !canManageTeam(role) {
		writeErr(w, http.StatusForbidden, "TEAM_ACCESS_DENIED", "team administrator role is required")
		return
	}
	rows, err := s.db.Query(r.Context(), `SELECT id,email,role,expires_at,accepted_at,created_at FROM team_invitations WHERE team_id=$1 ORDER BY created_at DESC LIMIT 100`, r.PathValue("id"))
	if err != nil {
		writeErr(w, 500, "TEAM_INVITATIONS_FAILED", "could not load team invitations")
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var id, email, role string
		var expires, created time.Time
		var accepted *time.Time
		if rows.Scan(&id, &email, &role, &expires, &accepted, &created) == nil {
			result = append(result, map[string]any{"id": id, "email": email, "role": role, "expires_at": expires, "accepted_at": accepted, "created_at": created})
		}
	}
	writeJSON(w, 200, result)
}

func (s *server) createTeamInvitation(w http.ResponseWriter, r *http.Request) {
	teamID := r.PathValue("id")
	role, ok := s.teamRole(r, teamID)
	if !ok || !canManageTeam(role) {
		writeErr(w, http.StatusForbidden, "TEAM_ACCESS_DENIED", "team administrator role is required")
		return
	}
	var in struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if !decodeJSON(r, &in) || !strings.Contains(in.Email, "@") {
		writeErr(w, http.StatusBadRequest, "INVALID_INPUT", "a valid email is required")
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if in.Role == "" {
		in.Role = "member"
	}
	if in.Role != "member" && in.Role != "admin" && in.Role != "billing" {
		writeErr(w, http.StatusBadRequest, "INVALID_ROLE", "invitation role must be member, admin, or billing")
		return
	}
	u := r.Context().Value(userKey).(user)
	var planLimit int
	var memberCount int
	if err := s.db.QueryRow(r.Context(), `SELECT p.member_limit FROM teams t JOIN plans p ON p.code=t.plan_code WHERE t.id=$1`, teamID).Scan(&planLimit); err == nil && planLimit > 0 {
		_ = s.db.QueryRow(r.Context(), `SELECT count(*) FROM team_members WHERE team_id=$1`, teamID).Scan(&memberCount)
		if memberCount >= planLimit {
			writeErr(w, http.StatusConflict, "MEMBER_LIMIT_REACHED", "the current plan does not allow more members")
			return
		}
	}
	var token string
	if token, _ = newOpaqueToken(); token == "" {
		writeErr(w, 500, "TOKEN_FAILED", "could not create invitation")
		return
	}
	var invitationID string
	expiresAt := time.Now().Add(7 * 24 * time.Hour)
	err := s.db.QueryRow(r.Context(), `INSERT INTO team_invitations(team_id,email,role,token_hash,invited_by,expires_at) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, teamID, in.Email, in.Role, tokenDigest(token), u.ID, expiresAt).Scan(&invitationID)
	if err != nil {
		writeErr(w, http.StatusConflict, "INVITATION_EXISTS", "could not create team invitation")
		return
	}
	text := "You have been invited to join an ImageHub team. Use this link to accept: " + strings.TrimRight(s.cfg.PublicURL, "/") + "/team-invitations/" + token + "\n\nThis invitation expires in 7 days."
	delivery, mailErr := s.sendEmail(r.Context(), in.Email, "You are invited to an ImageHub team", text)
	result := map[string]any{"id": invitationID, "team_id": teamID, "email": in.Email, "role": in.Role, "status": "pending", "expires_at": expiresAt, "email_delivery": delivery}
	if mailErr != nil {
		s.log.Warn("team invitation email could not be sent", "invitation_id", invitationID, "error", mailErr)
		result["email_delivery_error"] = "delivery failed; resend after configuring SMTP"
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *server) acceptTeamInvitation(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userKey).(user)
	var invitationID, teamID, email, role string
	var expires time.Time
	err := s.db.QueryRow(r.Context(), `SELECT id,team_id,email,role,expires_at FROM team_invitations WHERE token_hash=$1 AND accepted_at IS NULL`, tokenDigest(r.PathValue("token"))).Scan(&invitationID, &teamID, &email, &role, &expires)
	if err != nil || expires.Before(time.Now()) {
		writeErr(w, http.StatusBadRequest, "INVITATION_INVALID", "invitation is invalid or expired")
		return
	}
	if !strings.EqualFold(email, u.Email) {
		writeErr(w, http.StatusForbidden, "INVITATION_EMAIL_MISMATCH", "invitation email does not match the signed-in account")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeErr(w, 500, "INVITATION_FAILED", "could not accept invitation")
		return
	}
	defer tx.Rollback(r.Context())
	var limit, members int
	if err = tx.QueryRow(r.Context(), `SELECT p.member_limit FROM teams t JOIN plans p ON p.code=t.plan_code WHERE t.id=$1 FOR UPDATE`, teamID).Scan(&limit); err == nil {
		_ = tx.QueryRow(r.Context(), `SELECT count(*) FROM team_members WHERE team_id=$1`, teamID).Scan(&members)
	}
	if err == nil && limit > 0 && members >= limit {
		writeErr(w, http.StatusConflict, "MEMBER_LIMIT_REACHED", "the current plan does not allow more members")
		return
	}
	var accepted bool
	if err = tx.QueryRow(r.Context(), `SELECT accepted_at IS NOT NULL FROM team_invitations WHERE id=$1 FOR UPDATE`, invitationID).Scan(&accepted); err != nil || accepted {
		writeErr(w, 400, "INVITATION_INVALID", "invitation is invalid or already accepted")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO team_members(team_id,user_id,role) VALUES($1,$2,$3) ON CONFLICT(team_id,user_id) DO UPDATE SET role=CASE WHEN team_members.role='owner' THEN team_members.role ELSE EXCLUDED.role END`, teamID, u.ID, role); err != nil {
		writeErr(w, 500, "INVITATION_FAILED", "could not add team member")
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE team_invitations SET accepted_at=now() WHERE id=$1 AND accepted_at IS NULL`, invitationID); err != nil {
		writeErr(w, 500, "INVITATION_FAILED", "could not consume invitation")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeErr(w, 500, "INVITATION_FAILED", "could not accept invitation")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accepted": true, "team_id": teamID, "role": role})
}

func (s *server) getSubscription(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.teamRole(r, r.PathValue("id")); !ok {
		writeErr(w, http.StatusForbidden, "TEAM_ACCESS_DENIED", "team membership is required")
		return
	}
	var id, plan, status string
	var provider, providerID *string
	var periodEnd *time.Time
	err := s.db.QueryRow(r.Context(), `SELECT id,plan_code,status,provider,provider_subscription_id,current_period_end FROM subscriptions WHERE team_id=$1 ORDER BY created_at DESC LIMIT 1`, r.PathValue("id")).Scan(&id, &plan, &status, &provider, &providerID, &periodEnd)
	if err != nil {
		writeJSON(w, 200, map[string]any{"team_id": r.PathValue("id"), "subscription": nil})
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "team_id": r.PathValue("id"), "plan_code": plan, "status": status, "provider": provider, "provider_subscription_id": providerID, "current_period_end": periodEnd})
}

func (s *server) upsertSubscription(w http.ResponseWriter, r *http.Request) {
	teamID := r.PathValue("id")
	role, ok := s.teamRole(r, teamID)
	if !ok || (role != "owner" && role != "billing") {
		writeErr(w, http.StatusForbidden, "BILLING_ACCESS_DENIED", "team owner or billing role is required")
		return
	}
	var in struct {
		PlanCode               string     `json:"plan_code"`
		Provider               string     `json:"provider"`
		ProviderSubscriptionID string     `json:"provider_subscription_id"`
		Status                 string     `json:"status"`
		CurrentPeriodEnd       *time.Time `json:"current_period_end"`
	}
	if !decodeJSON(r, &in) || strings.TrimSpace(in.PlanCode) == "" {
		writeErr(w, 400, "INVALID_INPUT", "plan_code is required")
		return
	}
	if in.Status == "" {
		in.Status = "active"
	}
	var quota int64
	var price int
	if err := s.db.QueryRow(r.Context(), `SELECT quota_bytes,price_cents FROM plans WHERE code=$1 AND active=true`, in.PlanCode).Scan(&quota, &price); err != nil {
		writeErr(w, 404, "PLAN_NOT_FOUND", "plan is not active")
		return
	}
	if price > 0 {
		writeErr(w, http.StatusConflict, "CHECKOUT_REQUIRED", "paid plans must be activated through a signed payment webhook")
		return
	}
	var id string
	err := s.db.QueryRow(r.Context(), `INSERT INTO subscriptions(team_id,plan_code,status,provider,provider_subscription_id,current_period_end) VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6) RETURNING id`, teamID, in.PlanCode, in.Status, in.Provider, in.ProviderSubscriptionID, in.CurrentPeriodEnd).Scan(&id)
	if err != nil {
		writeErr(w, 500, "SUBSCRIPTION_FAILED", "could not create subscription")
		return
	}
	_, _ = s.db.Exec(r.Context(), `UPDATE teams SET plan_code=$1,quota_bytes=$2 WHERE id=$3`, in.PlanCode, quota, teamID)
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "team_id": teamID, "plan_code": in.PlanCode, "status": in.Status})
}

func (s *server) billingWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		writeErr(w, 400, "WEBHOOK_INVALID", "could not read webhook")
		return
	}
	if !verifyStripeSignature(r.Header.Get("Stripe-Signature"), body, s.cfg.StripeWebhookSecret) {
		writeErr(w, 400, "WEBHOOK_INVALID", "invalid Stripe signature")
		return
	}
	var event struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Object json.RawMessage `json:"object"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &event) != nil || event.ID == "" {
		writeErr(w, 400, "WEBHOOK_INVALID", "invalid event payload")
		return
	}
	var inserted bool
	if s.db.QueryRow(r.Context(), `INSERT INTO billing_events(provider,event_id,event_type,payload) VALUES('stripe',$1,$2,$3) ON CONFLICT DO NOTHING RETURNING true`, event.ID, event.Type, body).Scan(&inserted) != nil {
		writeJSON(w, 200, map[string]any{"received": true, "duplicate": true})
		return
	}
	if event.Type == "checkout.session.completed" || event.Type == "customer.subscription.updated" || event.Type == "customer.subscription.created" || event.Type == "customer.subscription.deleted" {
		_ = s.reconcileStripeEvent(r.Context(), event.Type, event.Data.Object)
	}
	writeJSON(w, 200, map[string]any{"received": true})
}

func verifyStripeSignature(header string, body []byte, secret string) bool {
	if secret == "" || header == "" {
		return false
	}
	var ts, sig string
	for _, part := range strings.Split(header, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		if kv[0] == "t" {
			ts = kv[1]
		}
		if kv[0] == "v1" {
			sig = kv[1]
		}
	}
	timestamp, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || absInt64(time.Now().Unix()-timestamp) > 300 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(ts + "." + string(body)))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}
func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func (s *server) createCheckoutSession(w http.ResponseWriter, r *http.Request) {
	teamID := r.PathValue("id")
	role, ok := s.teamRole(r, teamID)
	if !ok || (role != "owner" && role != "billing") {
		writeErr(w, 403, "BILLING_ACCESS_DENIED", "team owner or billing role is required")
		return
	}
	var in struct {
		PlanCode string `json:"plan_code"`
	}
	if !decodeJSON(r, &in) || in.PlanCode == "" {
		writeErr(w, 400, "INVALID_INPUT", "plan_code is required")
		return
	}
	if s.cfg.StripeSecretKey == "" {
		writeErr(w, 503, "BILLING_UNAVAILABLE", "Stripe is not configured")
		return
	}
	var priceID string
	var price int
	if err := s.db.QueryRow(r.Context(), `SELECT COALESCE(provider_price_id,''),price_cents FROM plans WHERE code=$1 AND active=true`, in.PlanCode).Scan(&priceID, &price); err != nil || price <= 0 || priceID == "" {
		writeErr(w, 409, "PLAN_NOT_CHECKOUTABLE", "plan has no Stripe price")
		return
	}
	form := url.Values{"mode": {"subscription"}, "success_url": {strings.TrimRight(s.cfg.PublicURL, "/") + "/billing?checkout=success"}, "cancel_url": {strings.TrimRight(s.cfg.PublicURL, "/") + "/billing?checkout=cancelled"}, "line_items[0][price]": {priceID}, "line_items[0][quantity]": {"1"}, "subscription_data[metadata][team_id]": {teamID}, "subscription_data[metadata][plan_code]": {in.PlanCode}}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, "https://api.stripe.com/v1/checkout/sessions", strings.NewReader(form.Encode()))
	req.Header.Set("Authorization", "Bearer "+s.cfg.StripeSecretKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeErr(w, 502, "CHECKOUT_FAILED", "could not create checkout session")
		return
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode >= 300 {
		writeErr(w, 502, "CHECKOUT_FAILED", "payment provider rejected checkout")
		return
	}
	writeJSON(w, 201, map[string]any{"id": out["id"], "url": out["url"]})
}

func (s *server) reconcileStripeEvent(ctx context.Context, eventType string, raw json.RawMessage) error {
	var obj struct {
		ID                string            `json:"id"`
		Status            string            `json:"status"`
		Metadata          map[string]string `json:"metadata"`
		CurrentPeriodEnd  int64             `json:"current_period_end"`
		CancelAtPeriodEnd bool              `json:"cancel_at_period_end"`
		Subscription      string            `json:"subscription"`
	}
	if json.Unmarshal(raw, &obj) != nil {
		return errors.New("invalid Stripe object")
	}
	teamID := obj.Metadata["team_id"]
	plan := obj.Metadata["plan_code"]
	if teamID == "" || plan == "" {
		return nil
	}
	status := "active"
	if eventType == "customer.subscription.deleted" {
		status = "canceled"
	}
	end := time.Unix(obj.CurrentPeriodEnd, 0)
	result, err := s.db.Exec(ctx, `UPDATE subscriptions SET plan_code=$1,status=$2,current_period_end=$3 WHERE provider='stripe' AND provider_subscription_id=$4`, plan, status, end, obj.ID)
	if err == nil && result.RowsAffected() == 0 {
		_, err = s.db.Exec(ctx, `INSERT INTO subscriptions(team_id,plan_code,status,provider,provider_subscription_id,current_period_end) VALUES($1,$2,$3,'stripe',$4,$5)`, teamID, plan, status, obj.ID, end)
	}
	if err == nil {
		_, err = s.db.Exec(ctx, `UPDATE teams SET plan_code=$1,quota_bytes=(SELECT quota_bytes FROM plans WHERE code=$1) WHERE id=$2`, plan, teamID)
	}
	return err
}

var _ = errors.New
