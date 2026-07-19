// Outbound mail: Resend when RESEND_API_KEY is set, console otherwise (local
// dev). Kept as a bare fetch — one endpoint doesn't need an SDK.
const { RESEND_API_KEY, EMAIL_FROM } = process.env;

// Half-configured email is a loud error, never a silent disable.
if (RESEND_API_KEY && !EMAIL_FROM) {
	throw new Error('authd: EMAIL_FROM is required when RESEND_API_KEY is set');
}

export async function sendEmail({ to, subject, text }) {
	if (!RESEND_API_KEY) {
		console.log(`authd: [email disabled] to=${to} subject=${JSON.stringify(subject)}\n${text}`);
		return;
	}
	const res = await fetch('https://api.resend.com/emails', {
		method: 'POST',
		headers: {
			Authorization: `Bearer ${RESEND_API_KEY}`,
			'Content-Type': 'application/json',
		},
		body: JSON.stringify({ from: EMAIL_FROM, to, subject, text }),
	});
	if (!res.ok) {
		throw new Error(`authd: resend send failed: ${res.status} ${await res.text()}`);
	}
}
