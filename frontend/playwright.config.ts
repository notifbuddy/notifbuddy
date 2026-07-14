import { defineConfig, devices } from '@playwright/test';

// Dashboard e2e — runs against the REAL backend stack (docker compose), not a
// mock. The SvelteKit SPA is built with PUBLIC_API_BASE_URL=http://localhost:8080
// and served here on :5173; inside the compose "ui" container both are localhost
// because it shares the backend's network namespace. See backend/e2e/run-ui.sh
// and backend/e2e/docker-compose.e2e.yml.
//
// The browser authenticates with a forged wos_session cookie that fakeapis
// seals onto the shared volume (see e2e/fixtures.ts + backend/e2e/fakeapis).
const PORT = Number(process.env.PORT ?? 5173);

export default defineConfig({
	testDir: './e2e',
	fullyParallel: true,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 1 : 0,
	reporter: process.env.CI ? [['github'], ['list']] : 'list',
	use: {
		baseURL: `http://localhost:${PORT}`,
		trace: 'on-first-retry'
	},
	projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
	// Serve the prebuilt SPA. The build happens in Dockerfile.ui; locally, run
	// `npm run build` first (with PUBLIC_API_BASE_URL set) against a running stack.
	webServer: {
		command: `node e2e/serve.mjs`,
		port: PORT,
		reuseExistingServer: !process.env.CI,
		env: { PORT: String(PORT) },
		timeout: 60_000
	}
});
