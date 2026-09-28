package main

import (
	"net/http"
	"net/url"
	"strings"
)

func (s *server) createOIDCProvider(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name           string   `json:"name"`
		IssuerURL      string   `json:"issuer_url"`
		ClientID       string   `json:"client_id"`
		ClientSecret   string   `json:"client_secret"`
		Scopes         []string `json:"scopes"`
		Enabled        bool     `json:"enabled"`
		AutoCreateUser bool     `json:"auto_create_users"`
	}
	if !decodeJSON(r, &in) || strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.IssuerURL) == "" || strings.TrimSpace(in.ClientID) == "" || in.ClientSecret == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_INPUT", "name, issuer_url, client_id and client_secret are required")
		return
	}
	parsedIssuer, parseErr := url.Parse(strings.TrimSpace(in.IssuerURL))
	if parseErr != nil || (parsedIssuer.Scheme != "https" && parsedIssuer.Scheme != "http") || parsedIssuer.Host == "" {
		writeErr(w, 400, "INVALID_ISSUER", "issuer_url must be an absolute HTTP(S) URL")
		return
	}
	if len(in.Scopes) == 0 {
		in.Scopes = []string{"openid", "profile", "email"}
	}
	encrypted, err := encryptSecret(s.cfg.AppSecret, in.ClientSecret)
	if err != nil {
		writeErr(w, 500, "OIDC_SECRET_FAILED", "could not protect OIDC client secret")
		return
	}
	var id string
	err = s.db.QueryRow(r.Context(), `INSERT INTO oidc_providers(name,issuer_url,client_id,client_secret_encrypted,scopes,enabled,auto_create_users) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, strings.TrimSpace(in.Name), strings.TrimRight(strings.TrimSpace(in.IssuerURL), "/"), strings.TrimSpace(in.ClientID), encrypted, in.Scopes, in.Enabled, in.AutoCreateUser).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusConflict, "OIDC_EXISTS", "could not create OIDC provider")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "name": in.Name, "issuer_url": strings.TrimRight(in.IssuerURL, "/"), "client_id": in.ClientID, "scopes": in.Scopes, "enabled": in.Enabled, "auto_create_users": in.AutoCreateUser})
}

func (s *server) updateOIDCProvider(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name           string   `json:"name"`
		IssuerURL      string   `json:"issuer_url"`
		ClientID       string   `json:"client_id"`
		ClientSecret   string   `json:"client_secret"`
		Scopes         []string `json:"scopes"`
		Enabled        *bool    `json:"enabled"`
		AutoCreateUser *bool    `json:"auto_create_users"`
	}
	if !decodeJSON(r, &in) {
		writeErr(w, 400, "INVALID_INPUT", "invalid provider payload")
		return
	}
	secret := ""
	if in.ClientSecret != "" {
		var err error
		secret, err = encryptSecret(s.cfg.AppSecret, in.ClientSecret)
		if err != nil {
			writeErr(w, 500, "OIDC_SECRET_FAILED", "could not protect OIDC client secret")
			return
		}
	}
	_, err := s.db.Exec(r.Context(), `UPDATE oidc_providers SET name=COALESCE(NULLIF($1,''),name),issuer_url=COALESCE(NULLIF($2,''),issuer_url),client_id=COALESCE(NULLIF($3,''),client_id),client_secret_encrypted=CASE WHEN $4='' THEN client_secret_encrypted ELSE $4 END,scopes=CASE WHEN COALESCE(cardinality($5::text[]),0)=0 THEN scopes ELSE $5 END,enabled=COALESCE($6,enabled),auto_create_users=COALESCE($7,auto_create_users) WHERE id=$8`, in.Name, strings.TrimRight(in.IssuerURL, "/"), in.ClientID, secret, in.Scopes, in.Enabled, in.AutoCreateUser, r.PathValue("id"))
	if err != nil {
		writeErr(w, 500, "OIDC_UPDATE_FAILED", "could not update OIDC provider")
		return
	}
	writeJSON(w, 200, map[string]bool{"updated": true})
}

func (s *server) deleteOIDCProvider(w http.ResponseWriter, r *http.Request) {
	result, err := s.db.Exec(r.Context(), `DELETE FROM oidc_providers WHERE id=$1`, r.PathValue("id"))
	if err != nil || result.RowsAffected() == 0 {
		writeErr(w, 404, "OIDC_NOT_FOUND", "OIDC provider not found")
		return
	}
	writeJSON(w, 200, map[string]bool{"deleted": true})
}
