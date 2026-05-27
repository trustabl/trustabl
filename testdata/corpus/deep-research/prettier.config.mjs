/** @type {import('prettier').Config} */
export default {
  endOfLine: 'lf',
  semi: true,
  useTabs: false,
  singleQuote: true,
  arrowParens: 'avoid',
  tabWidth: 2,
  trailingComma: 'all',
  importOrder: [
    '^(react/(.*)$)|^(react$)',
    '^(next/(.*)$)|^(next$)',
    '<THIRD_PARTY_MODULES>',
    '',
    '@repo/(.*)$',
    '',
    '^@/(.*)$',
    '',
    '^[./]',
  ],
  importOrderParserPlugins: ['typescript', 'jsx'],
  importOrderTypeScriptVersion: '5.7.2',
  plugins: ['@ianvs/prettier-plugin-sort-imports'],
};
