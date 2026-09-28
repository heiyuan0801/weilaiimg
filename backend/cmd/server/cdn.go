package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// purgeCDN is optional and deliberately fail-closed: media remains available
// when Cloudflare is not configured, while a configured purge is retried by
// the caller's next visibility update if the provider rejects it.
func (s *server) purgeCDN(ctx context.Context, urls ...string) error {
	if s.cfg.CloudflareAPIToken == "" || s.cfg.CloudflareZoneID == "" || len(urls) == 0 {
		return nil
	}
	body, _ := json.Marshal(map[string]any{"files": urls})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.cloudflare.com/client/v4/zones/"+s.cfg.CloudflareZoneID+"/purge_cache", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.CloudflareAPIToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("CDN purge returned %s", resp.Status)
	}
	var result struct {
		Success bool `json:"success"`
	}
	if json.NewDecoder(resp.Body).Decode(&result) != nil || !result.Success {
		return fmt.Errorf("CDN purge was not accepted")
	}
	return nil
}
