import { cp, mkdir, rm } from 'node:fs/promises';

const here = new URL('./', import.meta.url);
const dist = new URL('./dist/', here);
const src = new URL('./src/', here);
const generated = new URL('./wailsjs/', here);

await rm(dist, { recursive: true, force: true });
await mkdir(dist, { recursive: true });
await cp(src, dist, { recursive: true });

// Wails generates frontend/wailsjs before running the frontend build command.
// Copy the generated binding tree into dist with its original structure.
await cp(generated, new URL('./dist/wailsjs/', here), { recursive: true });
