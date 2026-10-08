import { access, readdir, readFile } from "node:fs/promises";
import { dirname, extname, relative, resolve } from "node:path";

const root = resolve(import.meta.dirname, "..");
const documentsRoot = resolve(root, "docs");

async function markdownFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const nested = await Promise.all(
    entries.map(async (entry) => {
      const path = resolve(directory, entry.name);
      if (entry.isDirectory()) return markdownFiles(path);
      return entry.isFile() && extname(entry.name) === ".md" ? [path] : [];
    }),
  );
  return nested.flat();
}

function localTarget(raw) {
  const target = raw.trim().replace(/^<|>$/g, "");
  if (
    !target ||
    target.startsWith("#") ||
    /^(?:https?:|mailto:|tel:)/i.test(target)
  )
    return undefined;
  return decodeURIComponent(target.split("#", 1)[0]);
}

const failures = [];
for (const document of await markdownFiles(documentsRoot)) {
  const body = await readFile(document, "utf8");
  for (const match of body.matchAll(/!?\[[^\]]*]\(([^)\s]+)(?:\s+"[^"]*")?\)/g)) {
    const target = localTarget(match[1]);
    if (!target) continue;
    const destination = resolve(dirname(document), target);
    try {
      await access(destination);
    } catch {
      failures.push(
        `${relative(root, document)} -> ${target}`,
      );
    }
  }
}

if (failures.length) {
  console.error(`broken local Markdown links:\n${failures.sort().join("\n")}`);
  process.exitCode = 1;
} else {
  console.log("Local Markdown links valid.");
}
