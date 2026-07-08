import { fetchOrgProfile, type OrgProfile } from '$lib/organization';

// Shared, reactive profile of the *active* organization. `profile` is:
//   undefined → not yet loaded
//   null      → loaded, unavailable (signed out / no active org)
//   OrgProfile → loaded
// Lives in a `.svelte.ts` module so the top-nav org switcher and the
// organization settings page render the same avatar and name — a rename or
// avatar change on the settings page shows up in the nav immediately via
// `set()`, no refetch.
class OrgProfileStore {
	profile = $state<OrgProfile | null | undefined>(undefined);
	private loadedForOrg: string | null = null;

	// Fetch the active org's profile. Re-fetches only when the active org
	// changes (or on force), so the nav and settings page share one request.
	async load(orgId: string, force = false): Promise<void> {
		if (!force && this.loadedForOrg === orgId && this.profile !== undefined) return;
		this.loadedForOrg = orgId;
		this.profile = await fetchOrgProfile();
	}

	// Replace the profile after a mutation whose response already carries the
	// refreshed profile.
	set(profile: OrgProfile) {
		this.profile = profile;
		this.loadedForOrg = profile.id;
	}
}

export const orgProfileStore = new OrgProfileStore();
