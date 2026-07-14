import { test, expect, session } from './fixtures';

test.describe('dashboard', () => {
	test('renders the Linear tab with the not-connected prompt', async ({ page }) => {
		await page.goto('/dashboard/linear');

		await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();
		await expect(page.getByRole('link', { name: 'Linear' })).toBeVisible();
		// Nothing is connected in the fresh e2e org, so Linear settings show the
		// connect-first empty state.
		await expect(
			page.getByText('Connect Linear and Slack to configure channel rules')
		).toBeVisible();
		await expect(page.getByRole('link', { name: /Go to integrations/i })).toBeVisible();
	});

	test('top nav links navigate to Integrations', async ({ page }) => {
		await page.goto('/dashboard/linear');

		await page.getByRole('link', { name: 'Integrations', exact: true }).click();
		await expect(page).toHaveURL(/\/settings\/integrations\/workspace$/);
		await expect(page.getByRole('heading', { name: 'Integrations' })).toBeVisible();
	});

	test('org switcher lists the active organization', async ({ page }) => {
		await page.goto('/dashboard/linear');

		await page.getByRole('button', { name: session.orgName }).click();
		await expect(page.getByText('Organizations')).toBeVisible();
		// The org appears inside the dropdown as a selectable item.
		await expect(page.getByRole('menuitem', { name: session.orgName })).toBeVisible();
	});

	test('profile menu routes to a settings page', async ({ page }) => {
		await page.goto('/dashboard/linear');

		// The profile trigger is the trailing avatar button in the header (its
		// accessible name is the initials fallback, so target it positionally).
		await page.locator('header').getByRole('button').last().click();
		await page.getByRole('menuitem', { name: 'Billing' }).click();
		await expect(page).toHaveURL(/\/settings\/billing$/);
	});
});
