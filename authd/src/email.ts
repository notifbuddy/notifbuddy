// Outbound mail: Resend when email.resend_api_key is configured, console
// otherwise (local dev). Kept as a bare fetch — one endpoint doesn't need an
// SDK.
import { config } from './config.ts';

export async function sendEmail({ to, subject, text }: { to: string; subject: string; text: string }) {
	if (!config.email?.resend_api_key) {
		console.log(`authd: [email disabled] to=${to} subject=${JSON.stringify(subject)}\n${text}`);
		return;
	}
	const res = await fetch('https://api.resend.com/emails', {
		method: 'POST',
		headers: {
			Authorization: `Bearer ${config.email.resend_api_key}`,
			'Content-Type': 'application/json',
		},
		body: JSON.stringify({ from: config.email.from, to, subject, text }),
	});
	if (!res.ok) {
		throw new Error(`authd: resend send failed: ${res.status} ${await res.text()}`);
	}
}
