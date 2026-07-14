import { test, anonTest, expect, session } from './fixtures';

// Signed-in: the forged session cookie is seeded by the `test` fixture.
test.describe('signed-in entry', () => {
	test('redirects into the dashboard and shows the active org', async ({ page }) => {
		await page.goto('/');

		await expect(page).toHaveURL(/\/dashboard\/linear$/);
		await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();
		// The org switcher is populated from /me (backed by the WorkOS membership
		// fake), proving the whole identity round-trip works end to end.
		await expect(page.getByRole('button', { name: session.orgName })).toBeVisible();
	});
});

// Signed-out: the raw test, no cookie — the real backend answers /me with 401.
anonTest.describe('signed-out entry', () => {
	anonTest('shows the login card', async ({ page }) => {
		await page.goto('/');

		await expect(
			page.getByRole('heading', { name: 'Streamline your notifications' })
		).toBeVisible();
		await expect(page.getByRole('button', { name: /Continue with GitHub/i })).toBeVisible();
	});

	anonTest('deep-link bounces to the login scene', async ({ page }) => {
		await page.goto('/dashboard/linear');

		await expect(page).toHaveURL(/\/$/);
		await expect(page.getByRole('button', { name: /Continue with GitHub/i })).toBeVisible();
	});
});
