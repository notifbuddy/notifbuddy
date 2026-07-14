import { test, expect } from './fixtures';

// The e2e backend has integrations enabled (DB + encryption configured), so the
// provider cards render. Nothing is connected in the fresh org.
test.describe('integrations', () => {
	test('workspace page lists providers with tabs', async ({ page }) => {
		await page.goto('/settings/integrations/workspace');

		await expect(page.getByRole('heading', { name: 'Integrations' })).toBeVisible();
		await expect(page.getByRole('link', { name: 'Workspace' })).toBeVisible();
		await expect(page.getByRole('link', { name: 'User' })).toBeVisible();
		await expect(page.getByText('Slack', { exact: true })).toBeVisible();
		await expect(page.getByText('Linear', { exact: true })).toBeVisible();
		// Fresh org: neither provider is connected.
		await expect(page.getByText('Not connected').first()).toBeVisible();
	});

	test('User tab is reachable from Workspace', async ({ page }) => {
		await page.goto('/settings/integrations/workspace');

		await page.getByRole('link', { name: 'User' }).click();
		await expect(page).toHaveURL(/\/settings\/integrations\/user$/);
	});
});
