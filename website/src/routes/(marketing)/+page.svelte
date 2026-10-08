<script lang="ts">
	import { base } from '$app/paths';
	import { siteConfig } from '$lib/config/site.js';

	let copied = $state('');

	const installCommand = 'go get github.com/mrlm-net/voice-goio';

	async function copy(text: string) {
		try {
			await navigator.clipboard.writeText(text);
			copied = text;
			setTimeout(() => {
				if (copied === text) copied = '';
			}, 2000);
		} catch {
			// clipboard not available
		}
	}

	const packages = [
		{ name: 'speaker', what: 'Radio, intercom, cabin PA, chimes' },
		{ name: 'voices', what: 'Packs, downloads, the voice pool' },
		{ name: 'normalise', what: 'Written ATC text to spoken ICAO / FAA' },
		{ name: 'tts/piper', what: 'Neural synthesis, warm sidecars' },
		{ name: 'audio/radio', what: 'Band pass, noise, squelch, level' },
		{ name: 'stt', what: 'Pilot speech to intent tags' }
	];

	// Icon paths: 24×24, stroke 1.75
	const features = [
		{
			title: 'Fully offline',
			icon: '<path d="M2 2l20 20"/><path d="M8.5 16.5a5 5 0 0 1 7 0"/><path d="M5 12.9a10 10 0 0 1 5.2-2.8"/><path d="M19 12.9a10 10 0 0 0-2.3-1.6"/><path d="M12 20h.01"/>',
			body: "No services. Everything runs on the user's PC and works with the network adapter disabled, once piper and the voices are installed."
		},
		{
			title: 'Zero dependencies',
			icon: '<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 9h6v6H9z"/>',
			body: 'go.mod has zero require lines. Standard library only, CGO_ENABLED=0 everywhere; Windows APIs through syscall.'
		},
		{
			title: 'The radio, heard',
			icon: '<circle cx="12" cy="12" r="2"/><path d="M16.24 7.76a6 6 0 0 1 0 8.49M7.76 16.24a6 6 0 0 1 0-8.49M19.07 4.93a10 10 0 0 1 0 14.14M4.93 19.07a10 10 0 0 1 0-14.14"/>',
			body: 'One frequency followed, one call at a time with 1 to 5 s between calls, nothing said more than 60 s late, and the ATIS as a looping broadcast joined mid-sentence.'
		},
		{
			title: 'Intercom and cabin PA',
			icon: '<path d="M3 11v2a1 1 0 0 0 1 1h3l5 4V6L7 10H4a1 1 0 0 0-1 1z"/><path d="M16 9a3 3 0 0 1 0 6"/>',
			body: 'The crew on a dry intercom and the cabin PA through its own speaker chain, each on its own queue and output device, with chimes generated in code.'
		},
		{
			title: '1,145 voices, 23 accents',
			icon: '<circle cx="9" cy="8" r="4"/><path d="M2 21a7 7 0 0 1 14 0"/><path d="M17 4a4 4 0 0 1 0 8"/><path d="M22 21a7 7 0 0 0-4-6.3"/>',
			body: 'A voice per controller position with shift changes, one voice per controller on every frequency, and crews that never speak in a controller’s voice.'
		},
		{
			title: 'Phraseology that sounds right',
			icon: '<path d="M4 7V4h16v3"/><path d="M9 20h6"/><path d="M12 4v16"/>',
			body: 'BAW123 climb FL350 becomes “Speedbird one two tree, climb flight level tree fife zero”: ICAO and FAA, emergencies, METAR shorthand and the pauses.'
		},
		{
			title: 'A real radio chain',
			icon: '<polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/>',
			body: 'Band pass, presence, soft clip, noise, squelch and dropouts, compressed and levelled to −15 dBFS RMS the way real transmitters sound.'
		},
		{
			title: 'Intent tags in',
			icon: '<path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3z"/><path d="M19 10v2a7 7 0 0 1-14 0v-2"/><path d="M12 19v3"/>',
			body: 'Pilot speech to intent, callsign and value, with the callsigns on frequency as a dynamic rule and grammars of your own. SAPI on Windows is written but untried.'
		},
		{
			title: 'Windows target, any dev box',
			icon: '<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/>',
			body: 'Ships on Windows with piper and winmm; develops on macOS with the say backend. Windows-only code is cross-compiled on every build so it cannot rot.'
		}
	];

	const docs = [
		{ title: 'Getting Started', href: '/docs/getting-started', body: 'What the library is, how to add it, and the demos that make it audible.' },
		{ title: 'The Speaker', href: '/docs/speaker', body: 'The radio, intercom and cabin PA as applications use them.' },
		{ title: 'Piper and Voices', href: '/docs/piper-and-voices', body: 'Install the synthesiser and the voice packs on the user’s machine.' },
		{ title: 'Platform Status', href: '/docs/platform-status', body: 'What runs where, and what has not been tried yet.' }
	];
</script>

<svelte:head>
	<title>voice-goio — offline ATC voice for Go</title>
	<meta name="description" content={siteConfig.description} />
</svelte:head>

{#snippet arrow()}
	<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M5 12h14M12 5l7 7-7 7" /></svg>
{/snippet}

{#snippet command(text: string, label: string)}
	<div class="cmd">
		<code><span style="color: var(--text-3);">$&nbsp;</span>{text}</code>
		<button onclick={() => copy(text)} aria-label={label} title="Copy to clipboard" class:ok={copied === text}>
			<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
				{#if copied === text}
					<polyline points="20 6 9 17 4 12" />
				{:else}
					<rect x="9" y="9" width="13" height="13" rx="2" /><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
				{/if}
			</svg>
		</button>
	</div>
{/snippet}

<!-- Hero -->
<section class="hero px-6 pt-16 pb-16 sm:pt-24 sm:pb-20">
	<div class="mx-auto grid max-w-6xl items-center gap-12 lg:grid-cols-[1.25fr_1fr] [&>*]:min-w-0">
		<div>
			<div class="mb-6 flex flex-wrap gap-2">
				<span class="pill"><span class="dot"></span>Go 1.27+ · stdlib only · offline</span>
			</div>
			<h1 class="mb-5 text-4xl font-semibold tracking-tight sm:text-6xl" style="color: var(--text);">
				voice<span class="brand-word">-goio</span>
			</h1>
			<p class="mb-8 max-w-xl text-base leading-relaxed sm:text-lg" style="color: var(--text-2);">
				Offline ATC voice in and out, as a Go library. Controller speech through a radio chain, pilot
				speech to intent tags &mdash; for a flight simulator add-on, a training tool or a controller
				trainer. It knows nothing about any particular simulator.
			</p>
			<div class="mb-8 flex flex-wrap gap-3">
				<a href="{base}/docs/getting-started" class="btn btn-primary">Get started {@render arrow()}</a>
				<a href={siteConfig.repoUrl} target="_blank" rel="noopener noreferrer" class="btn">View on GitHub</a>
			</div>
			<div class="max-w-xl">{@render command(installCommand, 'Copy install command')}</div>
			<div class="mt-4 flex flex-wrap gap-2">
				<a href="{base}/docs/platform-status" class="pill">Windows · macOS · Linux</a>
				<a href="https://pkg.go.dev/github.com/mrlm-net/voice-goio" target="_blank" rel="noopener noreferrer" class="pill">pkg.go.dev</a>
				<a href="{siteConfig.repoUrl}/blob/main/LICENSE" target="_blank" rel="noopener noreferrer" class="pill">{siteConfig.licenseLabel}</a>
			</div>
		</div>

		<div class="card overflow-hidden">
			<div class="flex items-center justify-between px-5 py-3" style="border-bottom: 1px solid var(--border);">
				<span class="eyebrow">Package</span>
				<span class="eyebrow">What it does</span>
			</div>
			{#each packages as p (p.name)}
				<div class="flex items-center justify-between gap-4 px-5 py-3" style="border-bottom: 1px solid var(--border);">
					<div class="mono text-sm font-medium" style="color: var(--text);">{p.name}</div>
					<div class="text-right text-xs" style="color: var(--text-3);">{p.what}</div>
				</div>
			{/each}
			<div class="px-5 py-3 text-xs" style="color: var(--text-3); background: var(--surface-2);">
				voicegoio.go is the compatibility surface
			</div>
		</div>
	</div>
</section>

<!-- Features -->
<section class="px-6 py-20" style="border-top: 1px solid var(--border);">
	<div class="mx-auto max-w-6xl">
		<p class="eyebrow mb-3">Why voice-goio</p>
		<h2 class="mb-10 max-w-2xl text-2xl font-semibold tracking-tight sm:text-3xl" style="color: var(--text);">
			Text in, radio out. Speech in, tags out.
		</h2>
		<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
			{#each features as f (f.title)}
				<div class="card p-5">
					<div class="mb-3 flex items-center gap-3">
						<span class="icon">
							<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">{@html f.icon}</svg>
						</span>
						<h3 class="text-[0.95rem] font-semibold" style="color: var(--text);">{f.title}</h3>
					</div>
					<p class="text-sm leading-relaxed" style="color: var(--text-2);">{f.body}</p>
				</div>
			{/each}
		</div>
	</div>
</section>

<!-- Docs -->
<section class="px-6 py-20" style="border-top: 1px solid var(--border);">
	<div class="mx-auto max-w-6xl">
		<p class="eyebrow mb-3">Explore the docs</p>
		<h2 class="mb-10 text-2xl font-semibold tracking-tight sm:text-3xl" style="color: var(--text);">From go get to the first transmission</h2>
		<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
			{#each docs as d (d.href)}
				<a href="{base}{d.href}" class="card group block p-5">
					<div class="mb-2 flex items-center justify-between text-sm font-semibold" style="color: var(--text);">
						{d.title}
						<span class="go" style="color: var(--text-3);">{@render arrow()}</span>
					</div>
					<p class="text-sm leading-relaxed" style="color: var(--text-2);">{d.body}</p>
				</a>
			{/each}
		</div>
	</div>
</section>

<!-- Commercial use + sponsor -->
<section class="px-6 py-20" style="border-top: 1px solid var(--border);">
	<div class="mx-auto grid max-w-6xl gap-4 md:grid-cols-2">
		<div class="card p-8">
			<p class="eyebrow mb-3">Business Source License 1.1</p>
			<h2 class="mb-3 text-xl font-semibold tracking-tight" style="color: var(--text);">Commercial use</h2>
			<p class="mb-6 text-sm leading-relaxed" style="color: var(--text-2);">
				Non-commercial use is free. Want to use voice-goio in a paid add-on or product, a paid service,
				or inside a business? Open an issue and we'll sort out a licence.
			</p>
			<a href="https://github.com/mrlm-net/voice-goio/issues" target="_blank" rel="noopener noreferrer" class="btn btn-primary">Open an issue</a>
		</div>
		<div class="card p-8">
			<p class="eyebrow mb-3">Back open-source MSFS tooling</p>
			<h2 class="mb-3 flex items-center gap-2 text-xl font-semibold tracking-tight" style="color: var(--text);">
				<svg width="18" height="18" viewBox="0 0 24 24" fill="var(--danger)" aria-hidden="true"><path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z" /></svg>
				Support the project
			</h2>
			<p class="mb-6 text-sm leading-relaxed" style="color: var(--text-2);">
				Sponsoring covers infrastructure costs and development time.
			</p>
			<a href="https://revolut.me/mrlm?currency=EUR" target="_blank" rel="noopener noreferrer" class="btn">Sponsor via Revolut</a>
		</div>
	</div>
</section>

<style>
	.hero {
		background:
			radial-gradient(ellipse 70% 60% at 85% 0%, var(--brand-soft) 0%, transparent 70%),
			var(--bg);
	}
	.icon {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 34px;
		height: 34px;
		flex-shrink: 0;
		border-radius: 8px;
		background: var(--brand-soft);
		color: var(--brand);
	}
	.cmd {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.6rem 0.6rem 0.6rem 1rem;
		border: 1px solid var(--border);
		border-radius: 8px;
		background: var(--surface);
		box-shadow: var(--shadow-1);
	}
	.cmd code {
		flex: 1;
		min-width: 0;
		overflow-x: auto;
		white-space: nowrap;
		font-family: var(--font-mono);
		font-size: 0.8125rem;
		color: var(--text);
		scrollbar-width: none;
	}
	.cmd code::-webkit-scrollbar {
		display: none;
	}
	.cmd button {
		display: inline-flex;
		padding: 0.4rem;
		border-radius: 6px;
		color: var(--text-3);
		cursor: pointer;
	}
	.cmd button:hover {
		color: var(--text);
		background: var(--surface-2);
	}
	.cmd button.ok {
		color: var(--ok);
	}
	.group:hover .go {
		color: var(--text) !important;
	}
</style>
