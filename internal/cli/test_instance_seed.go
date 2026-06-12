// test_instance_seed.go — `install test-instance --with-seed` (v2.8 #257).
//
// After the test sandbox's center is healthy, drive a usable tenant through the
// REAL center HTTP API (the same endpoints a human/agent uses — never a SQL
// shortcut): signup (creates the owner user + org + auto-signin JWT) → create
// one project → create one channel. The resulting signin credentials + entity
// ids are folded into the access pack so a consumer can log into the UI and
// navigate entities with zero round-trips (closes the manual-seed handoff pain
// that every acceptance round hit).
//
// Scope (#257 phase 1): tenant seed only. Worker org-enrollment +
// `--with-agent` (real agent producing tool events on a control-connected
// worker) is phase 2 — the #255 workers remain workforce-registered but not
// org-connected until then (see the access pack's workers_note).
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

// seedTestInstanceTenant signs up an owner + org, then creates one project and
// one channel, populating pack.Signin + pack.Entities. The HTTP client carries
// the signup's session cookie (cookiejar) to the authenticated create calls.
// Returns the authenticated client (session cookie in its jar) + the org slug
// so callers (#261 --with-agent) can reuse the session for org-scoped
// mint-enroll. seedOnly callers ignore the returned client/slug.
func seedTestInstanceTenant(ctx context.Context, pack *accessPack) (*http.Client, string, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, "", err
	}
	client := &http.Client{Timeout: 15 * time.Second, Jar: jar}
	base := strings.TrimRight(pack.WebURL, "/")

	// Per-instance unique tenant (id is unique per sandbox → unique slug/email).
	slug := "acme-" + pack.ID
	signin := seedSignin{
		OrgSlug:     slug,
		DisplayName: "Owner " + pack.ID,
		Email:       "owner@" + slug + ".test",
		// v2.9 #290: passcode rule strengthened (≥6 + letter+digit+symbol, ≤128) —
		// the old "123456" (6 digits) now fails ValidatePasscodePlain → seed signup
		// would 400. Fixed compliant value (printed in the signin pack for UI login).
		Passcode: "SeedPass1!",
	}

	// 1. signup → owner user + org + auto-signin (Set-Cookie captured by the jar).
	var signupResp struct {
		OrganizationID string `json:"organization_id"`
	}
	if err := seedPostJSON(ctx, client, base+"/api/auth/signup", map[string]any{
		"display_name":      signin.DisplayName,
		"passcode":          signin.Passcode,
		"organization_name": "Acme " + pack.ID,
		"organization_slug": slug,
		"email":             signin.Email,
	}, &signupResp); err != nil {
		return nil, "", fmt.Errorf("signup: %w", err)
	}

	// 2. create a project (org carried by the /api/orgs/{slug} path, same as the UI).
	var projResp struct {
		ID string `json:"id"`
	}
	if err := seedPostJSON(ctx, client, base+"/api/orgs/"+slug+"/projects", map[string]any{
		"name": "Alpha", "description": "seeded by install test-instance --with-seed",
	}, &projResp); err != nil {
		return nil, "", fmt.Errorf("create project: %w", err)
	}

	// 3. create a channel (response id field is conversation_id).
	var chanResp struct {
		ConversationID string `json:"conversation_id"`
	}
	if err := seedPostJSON(ctx, client, base+"/api/orgs/"+slug+"/conversations", map[string]any{
		"kind": "channel", "name": "general",
	}, &chanResp); err != nil {
		return nil, "", fmt.Errorf("create channel: %w", err)
	}

	pack.Signin = &signin
	pack.Entities = &seedEntities{
		OrgID:     signupResp.OrganizationID,
		ProjectID: projResp.ID,
		ChannelID: chanResp.ConversationID,
	}
	pack.WebLogin = "seeded ✓ — sign in at web_url with the credentials in `signin`"
	pack.EntityIDs = "seeded ✓ — see `entity_refs` (org/project/channel)"
	return client, slug, nil
}

// seedGetJSON GETs a URL (carrying the session cookie) and decodes a 2xx JSON
// response into out. A non-2xx status is an error carrying the response body.
func seedGetJSON(ctx context.Context, client *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// seedPostJSON POSTs a JSON body and decodes a 2xx JSON response into out (out
// may be nil). A non-2xx status is an error carrying the response body so a
// seed failure is diagnosable.
func seedPostJSON(ctx context.Context, client *http.Client, url string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
