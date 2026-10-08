import type { SiteConfig } from '$lib/types/index.js';

export const siteConfig: SiteConfig = {
    title: 'voice-goio',
    description: 'Offline ATC voice in and out, as a Go library — controller speech through a radio chain, pilot speech to intent tags. No services, no dependencies.',
    repoUrl: 'https://github.com/mrlm-net/voice-goio',
    basePath: '',
    url: 'https://voice-goio.mrlm.net',
    ogImage: {
        width: 1200,
        height: 630
    },
    locale: 'en_US',
    license: 'BUSL-1.1',
    licenseLabel: 'BSL 1.1 · non-commercial',
    since: 2026,
    glyph: '))',
    nav: [
        { title: 'Docs', href: '/docs/getting-started', match: '/docs' },
        { title: 'Speaker', href: '/docs/speaker' },
        { title: 'Voices', href: '/docs/piper-and-voices' },
        { title: 'Changelog', href: '/docs/changelog' }
    ]
};
