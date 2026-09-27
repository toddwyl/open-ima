import { readFile, stat } from "node:fs/promises";

const index = await readFile(new URL("../dist/index.html", import.meta.url), "utf8");
const references = [...index.matchAll(/(?:src|href)="(\/[^"]+)"/g)].map((match) => match[1]);

if (references.length === 0) {
  throw new Error("web/dist/index.html does not reference any frontend assets");
}

for (const reference of references) {
  const asset = new URL(`.${reference}`, new URL("../dist/", import.meta.url));
  try {
    const info = await stat(asset);
    if (!info.isFile()) throw new Error("not a file");
  } catch {
    throw new Error(`web/dist/index.html references missing asset: ${reference}`);
  }
}

console.log(`checked ${references.length} frontend assets under web/dist/`);
