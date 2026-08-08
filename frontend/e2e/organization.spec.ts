import { test, expect } from './fixtures';

const INVITEE = 'revoke-me@example.com';

test.describe('organization invitations', () => {
	test('invites a teammate, then revoking drops the row from the list', async ({ page }) => {
		await page.goto('/settings/organization');
		await expect(page.getByRole('heading', { name: 'Organization' })).toBeVisible();

		await page.getByPlaceholder('teammate@example.com').fill(INVITEE);
		await page.getByRole('button', { name: 'Invite' }).click();

		await expect(page.getByText(`Invited ${INVITEE}.`)).toBeVisible();
		const row = page.getByText(INVITEE, { exact: true });
		await expect(row).toBeVisible();

		await page.getByRole('button', { name: `Revoke invitation for ${INVITEE}` }).click();
		await expect(page.getByRole('heading', { name: 'Revoke this invitation?' })).toBeVisible();
		await page.getByRole('button', { name: 'Revoke', exact: true }).click();

		await expect(page.getByText(`Revoked the invitation for ${INVITEE}.`)).toBeVisible();
		await expect(row).toHaveCount(0);

		// The row must stay gone across a reload: the server reports the
		// invitation as canceled, not as some other spelling the list still shows.
		await page.reload();
		await expect(page.getByRole('heading', { name: 'Organization' })).toBeVisible();
		await expect(page.getByText(INVITEE, { exact: true })).toHaveCount(0);
	});

	test('cancelling the confirm dialog keeps the invitation', async ({ page }) => {
		const email = 'keep-me@example.com';
		await page.goto('/settings/organization');

		await page.getByPlaceholder('teammate@example.com').fill(email);
		await page.getByRole('button', { name: 'Invite' }).click();
		await expect(page.getByText(`Invited ${email}.`)).toBeVisible();

		await page.getByRole('button', { name: `Revoke invitation for ${email}` }).click();
		await page.getByRole('button', { name: 'Cancel' }).click();

		await expect(page.getByText(email, { exact: true })).toBeVisible();
		await expect(page.getByText(`Revoked the invitation for ${email}.`)).toHaveCount(0);
	});
});
