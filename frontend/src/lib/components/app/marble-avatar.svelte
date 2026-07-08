<script lang="ts">
	// Generated "marble" avatar: soft overlapping gradient blobs derived
	// deterministically from a seed. The same seed always renders the same
	// marble, so re-rolling the seed server-side changes the avatar everywhere.
	let { seed, class: className = '' }: { seed: string; class?: string } = $props();

	// FNV-1a hash of the seed, feeding a mulberry32 PRNG.
	function rng(s: string): () => number {
		let h = 0x811c9dc5;
		for (let i = 0; i < s.length; i++) {
			h ^= s.charCodeAt(i);
			h = Math.imul(h, 0x01000193);
		}
		let a = h >>> 0;
		return () => {
			a |= 0;
			a = (a + 0x6d2b79f5) | 0;
			let t = Math.imul(a ^ (a >>> 15), 1 | a);
			t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
			return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
		};
	}

	const marble = $derived.by(() => {
		const r = rng(seed);
		const hue = Math.floor(r() * 360);
		// Base field: pale tint of the hue; blobs: the hue, a neighbor, and a
		// far accent, all mid-lightness so they blend instead of clash.
		const base = `hsl(${hue} 45% 86%)`;
		const hues = [hue, (hue + 40 + Math.floor(r() * 30)) % 360, (hue + 180 + Math.floor(r() * 60)) % 360];
		const blobs = hues.map((h, i) => ({
			cx: 12 + r() * 56,
			cy: 12 + r() * 56,
			rad: 26 + r() * 22 - i * 4,
			fill: `hsl(${h} ${62 + Math.floor(r() * 14)}% ${56 + Math.floor(r() * 12)}%)`,
			opacity: 0.75 + r() * 0.2
		}));
		return { base, blobs };
	});
</script>

<svg viewBox="0 0 80 80" role="img" aria-label="Generated organization avatar" class={className}>
	<defs>
		<clipPath id="marble-clip-{seed}">
			<circle cx="40" cy="40" r="40" />
		</clipPath>
		<filter id="marble-blur-{seed}" x="-40%" y="-40%" width="180%" height="180%">
			<feGaussianBlur stdDeviation="9" />
		</filter>
	</defs>
	<g clip-path="url(#marble-clip-{seed})">
		<rect width="80" height="80" fill={marble.base} />
		{#each marble.blobs as blob, i (i)}
			<circle
				cx={blob.cx}
				cy={blob.cy}
				r={blob.rad}
				fill={blob.fill}
				opacity={blob.opacity}
				filter="url(#marble-blur-{seed})"
			/>
		{/each}
	</g>
</svg>
