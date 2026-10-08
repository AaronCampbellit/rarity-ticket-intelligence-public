import { cp, mkdir, rm } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const destination = resolve(root, "frontend/dist/legal");
await rm(destination, { recursive: true, force: true });
await mkdir(destination, { recursive: true });
for (const name of ["LICENSE", "BRANDING.md", "THIRD_PARTY_NOTICES.md", "third-party-licenses"]) {
  await cp(resolve(root, name), resolve(destination, name), { recursive: true });
}
