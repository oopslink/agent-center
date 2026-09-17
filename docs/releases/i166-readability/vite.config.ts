import { mergeConfig } from 'vite';
import config from './vite.config';
export default mergeConfig(config, { optimizeDeps: { esbuildOptions: { target: 'es2022' } } });
