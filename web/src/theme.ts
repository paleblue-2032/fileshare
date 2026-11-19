import {defineTheme} from '@astryxdesign/core/theme';
import {neutralTheme} from '@astryxdesign/theme-neutral/built';

export const fileshareTheme = defineTheme({
  name: 'fileshare',
  extends: neutralTheme,
  tokens: {
    '--color-accent': ['#5b7cfa', '#7c9cff'],
    '--color-accent-muted': ['#e8ecff', '#18213d'],
    '--color-on-accent': ['#ffffff', '#ffffff'],
    '--color-icon-accent': ['#4f6ef0', '#9db1ff'],
    '--color-text-accent': ['#4f6ef0', '#b3c2ff'],
  },
});
