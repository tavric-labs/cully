import { defineConfig } from 'vitepress'
import { withMermaid } from 'vitepress-plugin-mermaid'

export default withMermaid(defineConfig({
  title: 'Cully',
  description: 'Guides for Cully: project tasks, handoffs, review and shared learning across people and coding agents.',
  lang: 'en-US',
  appearance: false,
  cleanUrls: true,
  outDir: '../dist/docs',
  mermaid: {
    theme: 'base',
    securityLevel: 'strict',
    themeVariables: {
      background: '#ffffff',
      primaryColor: '#ffffff',
      primaryTextColor: '#1f2937',
      primaryBorderColor: '#7eaf88',
      secondaryColor: '#f7f7f7',
      secondaryTextColor: '#1f2937',
      tertiaryColor: '#ffffff',
      tertiaryTextColor: '#1f2937',
      lineColor: '#527b5d',
      fontFamily: 'system-ui, sans-serif'
    }
  },
  head: [
    ['link', { rel: 'icon', href: '/favicon.svg', type: 'image/svg+xml' }],
    ['meta', { name: 'theme-color', content: '#ffffff' }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:site_name', content: 'Cully Docs' }]
  ],
  themeConfig: {
    logo: '/favicon.svg',
    siteTitle: 'Cully Docs',
    nav: [
      { text: 'Start', link: '/quickstart' },
      { text: 'Solo', link: '/solo' },
      { text: 'Team', link: '/team-workflows' },
      { text: 'How it works', link: '/how-cully-works' },
      { text: 'Terminal', link: '/terminal' },
      { text: 'Advisor', link: '/advisor' },
      { text: 'Self-host', link: '/hosting' },
      { text: 'Website', link: 'https://cully.net' }
    ],
    sidebar: [
      { text: 'Quickstart', link: '/quickstart' },
      { text: 'Solo mode', link: '/solo' },
      { text: 'Team mode', link: '/team-workflows' },
      {
        text: 'Core concepts',
        items: [
          { text: 'How Cully works', link: '/how-cully-works' },
          { text: 'The Cully terminal', link: '/terminal' },
          { text: 'Session intelligence', link: '/session-intelligence' },
          { text: 'Advisor', link: '/advisor' },
          { text: 'Project memory', link: '/memory' },
          { text: 'Connect an agent', link: '/agents' },
          { text: 'Use Cully in T3 Code', link: '/t3code' }
        ]
      },
      {
        text: 'Use Cully',
        items: [
          { text: 'Keep sessions focused', link: '/session-optimization' },
          { text: 'Privacy', link: '/privacy' },
          { text: 'Work across people and agents', link: '/team-workflows' }
        ]
      },
      {
        text: 'Run Cully',
        items: [
          { text: 'Self-hosting', link: '/hosting' },
          { text: 'Team deployment', link: '/team-deployment' },
          { text: 'OAuth sign-in', link: '/oauth' }
        ]
      },
      {
        text: 'Reference',
        collapsed: true,
        items: [
          { text: 'Installer and PATH', link: '/installation' },
          { text: 'Configuration', link: '/configuration' },
          { text: 'Mem0 recall', link: '/mem0' },
          { text: 'Architecture', link: '/architecture' },
          { text: 'Development', link: '/development' },
          { text: 'Current capabilities', link: '/capabilities' },
          { text: 'Product vision', link: '/product-direction' },
          { text: 'Team workspace design', link: '/team-workspace' },
          { text: 'Shared learning design', link: '/shared-learning' },
          { text: 'Roadmap', link: '/roadmap' },
          { text: 'Changelog ↗', link: 'https://github.com/mcp-runtime/cully/blob/main/CHANGELOG.md' }
        ]
      },
    ],
    search: { provider: 'local' },
    socialLinks: [{ icon: 'github', link: 'https://github.com/mcp-runtime/cully' }],
    editLink: {
      pattern: 'https://github.com/mcp-runtime/cully/edit/main/docs/:path',
      text: 'Improve this page'
    },
    footer: {
      message: 'Built in the open. Apache 2.0 licensed.',
      copyright: 'Cully'
    }
  }
}))
