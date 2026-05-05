// @ts-check

import {themes as prismThemes} from 'prism-react-renderer';

/** @type {import('@docusaurus/types').Config} */
const config = {
  title: 'Wacht',
  tagline: 'Distributed uptime monitoring, built in the EU.',

  future: {
    v4: true,
  },

  url: 'https://wacht.cloud',
  baseUrl: '/',
  organizationName: 'tmater',
  projectName: 'wacht',

  onBrokenLinks: 'throw',

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  markdown: {
    mermaid: true,
    hooks: {
      onBrokenMarkdownLinks: 'warn',
    },
  },

  themes: ['@docusaurus/theme-mermaid'],

  presets: [
    [
      'classic',
      /** @type {import('@docusaurus/preset-classic').Options} */
      ({
        docs: {
          routeBasePath: '/',
          sidebarPath: './sidebars.js',
          lastVersion: 'current',
          versions: {
            current: {
              label: '0.1',
            },
          },
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      }),
    ],
  ],

  themeConfig:
    /** @type {import('@docusaurus/preset-classic').ThemeConfig} */
    ({
      colorMode: {
        respectPrefersColorScheme: true,
      },
      announcementBar: {
        id: 'docs-wip',
        content: 'Documentation is work in progress while the first release is prepared.',
        backgroundColor: '#f59e0b',
        textColor: '#111827',
        isCloseable: false,
      },
      navbar: {
        title: 'Wacht',
        items: [
          {
            type: 'docSidebar',
            sidebarId: 'wachtSidebar',
            position: 'left',
            label: 'Docs',
          },
          {
            href: 'https://github.com/tmater/wacht',
            label: 'GitHub',
            position: 'right',
          },
        ],
      },
      footer: {
        style: 'dark',
        links: [
          {
            title: 'Project',
            items: [
              {
                label: 'GitHub',
                href: 'https://github.com/tmater/wacht',
              },
              {
                label: 'License',
                href: 'https://github.com/tmater/wacht/blob/master/LICENSE',
              },
            ],
          },
        ],
        copyright: `Copyright © ${new Date().getFullYear()} Wacht contributors.`,
      },
      prism: {
        theme: prismThemes.github,
        darkTheme: prismThemes.dracula,
      },
    }),
};

export default config;
