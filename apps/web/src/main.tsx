import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { App } from './app/App';

import './index.css';

const root = document.getElementById('root');
if (!root) throw new Error('#root가 없다. index.html을 확인할 것');

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
