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
func seedTestInstanceTenant(ctx context.Context, pack *accessPack) error {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 15 * time.Second, Jar: jar}
	base := strings.TrimRight(pack.WebURL, "/")

	// Per-instance unique tenant (id is unique per sandbox → unique slug/email).
	slug := "acme-" + pack.ID
	signin := seedSignin{
		OrgSlug:     slug,
		DisplayName: "Owner " + pack.ID,
		Email:       "owner@" + slug + ".test",
		Passcode:    "123456",
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
		return fmt.Errorf("signup: %w", err)
	}

	// 2. create a project (org resolved via ?org_slug=, same as the UI).
	var projResp struct {
		ID string `json:"id"`
	}
	if err := seedPostJSON(ctx, client, base+"/api/projects?org_slug="+slug, map[string]any{
		"name": "Alpha", "description": "seeded by install test-instance --with-seed",
	}, &projResp); err != nil {
		return fmt.Errorf("create project: %w", err)
	}

	// 3. create a channel (response id field is conversation_id).
	var chanResp struct {
		ConversationID string `json:"conversation_id"`
	}
	if err := seedPostJSON(ctx, client, base+"/api/conversations?org_slug="+slug, map[string]any{
		"kind": "channel", "name": "general",
	}, &chanResp); err != nil {
		return fmt.Errorf("create channel: %w", err)
	}

	pack.Signin = &signin
	pack.Entities = &seedEntities{
		OrgID:     signupResp.OrganizationID,
		ProjectID: projResp.ID,
		ChannelID: chanResp.ConversationID,
	}
	pack.WebLogin = "seeded ✓ — sign in at web_url with the credentials in `signin`"
	pack.EntityIDs = "seeded ✓ — see `entity_refs` (org/project/channel)"
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
