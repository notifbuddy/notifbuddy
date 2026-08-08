import { test, expect } from './fixtures';

// getByRole name matching is a substring match, and the role select is labelled
// "Role for the invitee" — which contains "Invite". Every button locator here is
// exact, or scoped to the dialog, for that reason.
const invite = async (page: import('@playwright/test').Page, email: string) => {
	await page.getByPlaceholder('teammate@example.com').fill(email);
	await page.getByRole('button', { name: 'Invite', exact: true }).click();
	await expect(page.getByText(`Invited ${email}.`)).toBeVisible();
};

test.describe('organization invitations', () => {
	test('invites a teammate, then revoking drops the row from the list', async ({ page }) => {
		const email = 'revoke-me@example.com';
		await page.goto('/settings/organization');
		await expect(page.getByRole('heading', { name: 'Organization', exact: true })).toBeVisible();

		await invite(page, email);
		const row = page.getByText(email, { exact: true });
		await expect(row).toBeVisible();

		await page.getByRole('button', { name: `Revoke invitation for ${email}`, exact: true }).click();
		const dialog = page.getByRole('alertdialog');
		await expect(dialog).toContainText('Revoke this invitation?');
		await expect(dialog).toContainText(email);
		await dialog.getByRole('button', { name: 'Revoke', exact: true }).click();

		await expect(page.getByText(`Revoked the invitation for ${email}.`)).toBeVisible();
		await expect(row).toHaveCount(0);

		// The row must stay gone across a reload: the server reports the
		// invitation as canceled, not as some other spelling the list still shows.
		await page.reload();
		await expect(page.getByRole('heading', { name: 'Organization', exact: true })).toBeVisible();
		await expect(page.getByText(email, { exact: true })).toHaveCount(0);
	});

	test('cancelling the confirm dialog keeps the invitation', async ({ page }) => {
		const email = 'keep-me@example.com';
		await page.goto('/settings/organization');

		await invite(page, email);

		await page.getByRole('button', { name: `Revoke invitation for ${email}`, exact: true }).click();
		const dialog = page.getByRole('alertdialog');
		await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();

		await expect(page.getByText(email, { exact: true })).toBeVisible();
		await expect(page.getByText(`Revoked the invitation for ${email}.`)).toHaveCount(0);
	});
});
