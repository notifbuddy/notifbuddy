<script lang="ts">
	import { goto } from '$app/navigation';

	// /settings/integrations has no content of its own — it splits into Workspace
	// and User sub-pages (see the sidebar). Land on Workspace by default.
	goto('/settings/integrations/workspace', { replaceState: true });
</script>
