// sci-auth — Cloudflare Worker that brokers GitHub OAuth device flow
// and returns R2 credentials to authenticated sciminds org members.

interface Env {
	GITHUB_CLIENT_ID: string;
	GITHUB_CLIENT_SECRET: string;
	ORG_NAME: string;
	R2_ACCOUNT_ID: string;
	R2_ACCESS_KEY: string;
	R2_SECRET_KEY: string;
	R2_PUBLIC_URL: string;
	R2_PRIVATE_ACCESS_KEY: string;
	R2_PRIVATE_SECRET_KEY: string;
	PUBLIC_BUCKET_NAME: string;
	PRIVATE_BUCKET_NAME: string;
}

interface GitHubDeviceCodeResponse {
	device_code: string;
	user_code: string;
	verification_uri: string;
	expires_in: number;
	interval: number;
}

interface GitHubTokenResponse {
	access_token?: string;
	token_type?: string;
	scope?: string;
	error?: string;
	error_description?: string;
	interval?: number;
}

interface GitHubUser {
	login: string;
}

interface GitHubOrg {
	login: string;
}

const corsHeaders = {
	"Access-Control-Allow-Origin": "*",
	"Access-Control-Allow-Methods": "POST, OPTIONS",
	"Access-Control-Allow-Headers": "Content-Type",
};

function json(data: unknown, status = 200): Response {
	return new Response(JSON.stringify(data), {
		status,
		headers: { "Content-Type": "application/json", ...corsHeaders },
	});
}

export default {
	async fetch(request: Request, env: Env): Promise<Response> {
		// Handle CORS preflight.
		if (request.method === "OPTIONS") {
			return new Response(null, { headers: corsHeaders });
		}

		const url = new URL(request.url);

		if (request.method === "POST" && url.pathname === "/auth/device") {
			return handleDevice(env);
		}
		if (request.method === "POST" && url.pathname === "/auth/token") {
			return handleToken(request, env);
		}

		return json({ error: "not found" }, 404);
	},
};

// POST /auth/device — initiate GitHub device flow.
async function handleDevice(env: Env): Promise<Response> {
	const resp = await fetch("https://github.com/login/device/code", {
		method: "POST",
		headers: {
			"Content-Type": "application/json",
			Accept: "application/json",
		},
		body: JSON.stringify({
			client_id: env.GITHUB_CLIENT_ID,
			scope: "read:org",
		}),
	});

	if (!resp.ok) {
		const text = await resp.text();
		return json({ error: `GitHub error: ${text}` }, 502);
	}

	const data = (await resp.json()) as GitHubDeviceCodeResponse;
	return json({
		device_code: data.device_code,
		user_code: data.user_code,
		verification_uri: data.verification_uri,
		expires_in: data.expires_in,
		interval: data.interval,
	});
}

// POST /auth/token — exchange device code for credentials.
async function handleToken(request: Request, env: Env): Promise<Response> {
	const body = (await request.json()) as { device_code?: string };
	if (!body.device_code) {
		return json({ status: "error", message: "missing device_code" }, 400);
	}

	// Exchange device code for access token.
	const tokenResp = await fetch("https://github.com/login/oauth/access_token", {
		method: "POST",
		headers: {
			"Content-Type": "application/json",
			Accept: "application/json",
		},
		body: JSON.stringify({
			client_id: env.GITHUB_CLIENT_ID,
			client_secret: env.GITHUB_CLIENT_SECRET,
			device_code: body.device_code,
			grant_type: "urn:ietf:params:oauth:grant-type:device_code",
		}),
	});

	if (!tokenResp.ok) {
		return json({ status: "error", message: "GitHub token exchange failed" }, 502);
	}

	const tokenData = (await tokenResp.json()) as GitHubTokenResponse;

	// Handle pending / slow_down / error states.
	if (tokenData.error) {
		switch (tokenData.error) {
			case "authorization_pending":
				return json({ status: "pending" });
			case "slow_down":
				return json({ status: "slow_down", interval: tokenData.interval });
			case "expired_token":
				return json({ status: "error", message: "device code expired — please restart auth" });
			case "access_denied":
				return json({ status: "error", message: "authorization denied by user" });
			default:
				return json({
					status: "error",
					message: tokenData.error_description || tokenData.error,
				});
		}
	}

	if (!tokenData.access_token) {
		return json({ status: "error", message: "no access token in response" }, 502);
	}

	// Fetch GitHub user info.
	const [userResp, orgsResp] = await Promise.all([
		fetch("https://api.github.com/user", {
			headers: {
				Authorization: `Bearer ${tokenData.access_token}`,
				Accept: "application/vnd.github+json",
				"User-Agent": "sci-auth-worker",
			},
		}),
		fetch("https://api.github.com/user/orgs", {
			headers: {
				Authorization: `Bearer ${tokenData.access_token}`,
				Accept: "application/vnd.github+json",
				"User-Agent": "sci-auth-worker",
			},
		}),
	]);

	if (!userResp.ok || !orgsResp.ok) {
		return json({ status: "error", message: "failed to verify GitHub identity" }, 502);
	}

	const user = (await userResp.json()) as GitHubUser;
	const orgs = (await orgsResp.json()) as GitHubOrg[];

	// Verify org membership.
	const isMember = orgs.some(
		(org) => org.login.toLowerCase() === env.ORG_NAME.toLowerCase()
	);
	if (!isMember) {
		return json({
			status: "error",
			message: `@${user.login} is not a member of the ${env.ORG_NAME} GitHub org`,
		});
	}

	// Return R2 credentials for both buckets.
	return json({
		status: "ok",
		username: user.login,
		github_login: user.login,
		account_id: env.R2_ACCOUNT_ID,
		public: {
			access_key: env.R2_ACCESS_KEY,
			secret_key: env.R2_SECRET_KEY,
			bucket_name: env.PUBLIC_BUCKET_NAME,
			public_url: env.R2_PUBLIC_URL,
		},
		private: {
			access_key: env.R2_PRIVATE_ACCESS_KEY,
			secret_key: env.R2_PRIVATE_SECRET_KEY,
			bucket_name: env.PRIVATE_BUCKET_NAME,
		},
	});
}
