// @ts-check

/** @type {import('@docusaurus/plugin-content-docs').SidebarsConfig} */
const sidebars = {
  wachtSidebar: [
    'index',
    {
      type: 'category',
      label: 'Tutorial',
      collapsed: false,
      items: [
        'wacht/install-docker-compose',
        'wacht/server-first-probes',
      ],
    },
    {
      type: 'category',
      label: 'How-to Guides',
      collapsed: false,
      items: [
        'wacht/probes',
        'wacht/operations',
      ],
    },
    {
      type: 'category',
      label: 'Reference',
      collapsed: false,
      items: [
        'wacht/config-reference',
      ],
    },
    {
      type: 'category',
      label: 'Explanation',
      collapsed: false,
      items: [
        'wacht/alerts-and-quorum',
        'wacht/status-pages',
      ],
    },
  ],
};

export default sidebars;
