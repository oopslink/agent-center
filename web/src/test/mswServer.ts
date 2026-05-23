import { setupServer } from 'msw/node';
import { handlers } from '../mocks/handlers';

// Node-side MSW server. setup.ts wires beforeAll / afterEach / afterAll
// so every test file gets a clean handler set.
export const server = setupServer(...handlers);
