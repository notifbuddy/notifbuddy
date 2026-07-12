// Command nuke is the break-glass environment reset: it revokes every stored
// Slack/Linear token at the provider, deletes all WorkOS organizations and
// users, and truncates every table in the app database. The schema survives
// (migrations are idempotent CREATE IF NOT EXISTS and re-apply on the next
// backend start); the data does not.
//
// Intended for wiping a dev/test environment back to zero — e.g. re-testing
// the sign-up → create-org → connect-integrations path from scratch. Run
// locally against dev (backend/.env) or via .github/workflows/nuke-all.yml
// against prod.
//
// Refuses to run unless NUKE_CONFIRM=destroy-everything. Steps run in
// dependency order — token revocation needs the DB rows and the decryptor,
// so it happens before anything is deleted:
//
//  1. decrypt + revoke Slack tokens (auth.revoke) and Linear tokens
//     (oauth/revoke) — best-effort per token, failures logged and skipped
//  2. delete every WorkOS organization, then every WorkOS user — best-effort
//     per item
//  3. TRUNCATE every table in the public schema (app tables + watermill
//     topics) — hard failure
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	workos "github.com/workos/workos-go/v9"

	"xolo/backend/internal/config"
	"xolo/backend/internal/crypto"
)

func main() {
	if os.Getenv("NUKE_CONFIRM") != "destroy-everything" {
		fatal("refusing to run: set NUKE_CONFIRM=destroy-everything to confirm", nil)
	}

	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		slog.Warn("could not load .env; relying on real environment", "error", err)
	}
	cfg, err := config.Load()
	if err != nil {
		fatal("config", err)
	}
	if cfg.Database.URL == "" {
		fatal("database.url is required", nil)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.Database.URL)
	if err != nil {
		fatal("database connect", err)
	}
	defer pool.Close()

	enc, err := buildEncryptor(ctx, cfg)
	if err != nil {
		fatal("encryptor", err)
	}

	revokeTokens(ctx, pool, enc)
	nukeWorkOS(ctx, workos.NewClient(cfg.WorkOS.APIKey))
	truncateAll(ctx, pool)

	slog.Info("nuke complete — schema intact, all data gone; migrations re-apply on next backend start")
}

// buildEncryptor mirrors cmd/server: local AES-GCM or Google Cloud KMS.
func buildEncryptor(ctx context.Context, cfg config.Config) (crypto.Encryptor, error) {
	switch cfg.Encryption.Provider {
	case "", "local":
		enc, _, err := crypto.NewLocalKeyEncryptorFromBase64(cfg.Encryption.LocalKey)
		return enc, err
	case "gcpkms":
		client, err := crypto.NewGCPKMSClient(ctx)
		if err != nil {
			return nil, err
		}
		return crypto.NewKMSEncryptor(ctx, client, cfg.Encryption.KMSKeyID)
	default:
		return nil, fmt.Errorf("unknown encryption.provider: %s", cfg.Encryption.Provider)
	}
}

// revokeTokens decrypts every stored integration token and revokes it at its
// provider so no live credential outlives the wipe. Best-effort: a token that
// fails to decrypt or revoke (already revoked, app uninstalled) is logged and
// skipped.
func revokeTokens(ctx context.Context, pool *pgxpool.Pool, enc crypto.Encryptor) {
	rows, err := pool.Query(ctx, `
		SELECT provider, level, org_id, encrypted_token
		FROM org_integrations
		WHERE encrypted_token IS NOT NULL
	`)
	if err != nil {
		slog.Error("nuke: list integrations failed — skipping revocation", "error", err)
		return
	}
	defer rows.Close()

	type row struct {
		provider, level, orgID string
		ct                     []byte
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.provider, &r.level, &r.orgID, &r.ct); err != nil {
			slog.Error("nuke: scan integration failed", "error", err)
			continue
		}
		all = append(all, r)
	}

	revoked, failed := 0, 0
	for _, r := range all {
		token, err := enc.Decrypt(r.ct)
		if err != nil {
			slog.Warn("nuke: decrypt token failed — skipping", "provider", r.provider, "level", r.level, "org_id", r.orgID, "error", err)
			failed++
			continue
		}
		switch r.provider {
		case "slack":
			err = revokeSlack(ctx, string(token))
		case "linear":
			err = revokeLinear(ctx, string(token))
		default:
			continue // nothing revocable stored for other providers
		}
		if err != nil {
			slog.Warn("nuke: revoke failed", "provider", r.provider, "level", r.level, "org_id", r.orgID, "error", err)
			failed++
			continue
		}
		revoked++
	}
	slog.Info("nuke: token revocation done", "revoked", revoked, "failed", failed)
}

func revokeSlack(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://slack.com/api/auth.revoke", strings.NewReader(url.Values{}.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack auth.revoke: HTTP %d", resp.StatusCode)
	}
	return nil
}

func revokeLinear(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.linear.app/oauth/revoke", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("linear oauth/revoke: HTTP %d", resp.StatusCode)
	}
	return nil
}

// nukeWorkOS deletes every organization (memberships and invitations go with
// them), then every user. Best-effort per item.
func nukeWorkOS(ctx context.Context, client *workos.Client) {
	orgs, users := 0, 0

	it := client.Organizations().List(ctx, &workos.OrganizationsListParams{})
	for it.Next() {
		org := it.Current()
		if err := client.Organizations().Delete(ctx, org.ID); err != nil {
			slog.Warn("nuke: delete workos org failed", "org_id", org.ID, "name", org.Name, "error", err)
			continue
		}
		orgs++
	}
	if err := it.Err(); err != nil {
		slog.Error("nuke: list workos orgs failed", "error", err)
	}

	ut := client.UserManagement().List(ctx, &workos.UserManagementListParams{})
	for ut.Next() {
		u := ut.Current()
		if err := client.UserManagement().Delete(ctx, u.ID); err != nil {
			slog.Warn("nuke: delete workos user failed", "user_id", u.ID, "email", u.Email, "error", err)
			continue
		}
		users++
	}
	if err := ut.Err(); err != nil {
		slog.Error("nuke: list workos users failed", "error", err)
	}

	slog.Info("nuke: workos wipe done", "organizations", orgs, "users", users)
}

// truncateAll empties every table in the public schema in one statement
// (CASCADE covers FKs). The schema itself survives.
func truncateAll(ctx context.Context, pool *pgxpool.Pool) {
	rows, err := pool.Query(ctx, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
	`)
	if err != nil {
		fatal("nuke: list tables", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			fatal("nuke: scan table name", err)
		}
		tables = append(tables, pgxIdent(t))
	}
	if len(tables) == 0 {
		slog.Info("nuke: no tables to truncate")
		return
	}
	if _, err := pool.Exec(ctx, "TRUNCATE TABLE "+strings.Join(tables, ", ")+" CASCADE"); err != nil {
		fatal("nuke: truncate", err)
	}
	slog.Info("nuke: database truncated", "tables", len(tables))
}

// pgxIdent quotes a table name as a Postgres identifier.
func pgxIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func fatal(msg string, err error) {
	if err != nil {
		slog.Error(msg, "error", err)
	} else {
		slog.Error(msg)
	}
	os.Exit(1)
}
