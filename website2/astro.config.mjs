import { defineConfig } from 'astro/config';
import sitemap from '@astrojs/sitemap';

// Placeholder production URL: canonical, OG and sitemap depend on it.
export default defineConfig({
  site: 'https://onigami.dev',
  integrations: [sitemap({ filter: (page) => !page.includes('404') })],
  prefetch: { prefetchAll: true, defaultStrategy: 'hover' },
  build: { inlineStylesheets: 'auto' },
});
