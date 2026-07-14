import { test, expect } from './fixtures';

test.describe('billing', () => {
	test('renders the billing page with plan options', async ({ page }) => {
		await page.goto('/settings/billing');

		await expect(page.getByRole('heading', { name: 'Billing' })).toBeVisible();
		// The current-plan card and the enterprise contact card both render for a
		// live billing status (beta mode in the e2e backend).
		await expect(page.getByText('Enterprise', { exact: true })).toBeVisible();
	});

	test('is reachable from the profile menu', async ({ page }) => {
		await page.goto('/dashboard/linear');

		await page.locator('header').getByRole('button').last().click();
		await page.getByRole('menuitem', { name: 'Billing' }).click();

		await expect(page).toHaveURL(/\/settings\/billing$/);
		await expect(page.getByRole('heading', { name: 'Billing' })).toBeVisible();
	});
});
