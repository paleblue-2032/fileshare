import './astryx';
import {StrictMode} from 'react';
import {createRoot} from 'react-dom/client';
import {Theme} from '@astryxdesign/core/theme';
import {fileshareTheme} from './theme';
import {App} from './App';

const root = document.getElementById('root');
if (!root) throw new Error('Root element not found');

createRoot(root).render(
  <StrictMode>
    <Theme theme={fileshareTheme} mode="dark">
      <App />
    </Theme>
  </StrictMode>,
);
