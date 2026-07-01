<script lang="ts">
	import { goto } from '$app/navigation';

	// /dashboard has no content of its own — it splits into per-product pages
	// (Linear now, GitHub later), surfaced as secondary tabs. Land on Linear.
	goto('/dashboard/linear', { replaceState: true });
</script>
