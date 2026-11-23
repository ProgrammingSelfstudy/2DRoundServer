import { readFile, writeFile } from 'node:fs/promises';
import { dirname, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

// A portable copy for double-clicking or sharing without a running dev server.
// Keep this list explicit: the app also selects pack faces with a template string.
const root = dirname(fileURLToPath(import.meta.url));
const assetTypes = {
  'assets/zoro-pack-front-hd.svg': 'image/svg+xml',
  'assets/zoro-pack-back-hd.svg': 'image/svg+xml',
  'assets/zoro-card-back.svg': 'image/svg+xml',
  'assets/zoro-card-art.png': 'image/png',
};

function localFile(name) {
  const path = resolve(root, name);
  if (!path.startsWith(root + sep)) throw new Error(`Not a demo file: ${name}`);
  return path;
}

function attribute(tag, name) {
  return tag.match(new RegExp(`\\b${name}\\s*=\\s*(["'])(.*?)\\1`, 'i'))?.[2];
}

function assertEmbeddedUrls(source, label) {
  for (const match of source.matchAll(/\b(?:href|xlink:href)\s*=\s*(["'])(.*?)\1/gi)) {
    if (!/^(?:data:|#)/i.test(match[2])) throw new Error(`${label} has an external image reference: ${match[2]}`);
  }
}

const [template, app, assetEntries] = await Promise.all([
  readFile(localFile('index.html'), 'utf8'),
  readFile(localFile('app.js'), 'utf8'),
  Promise.all(Object.entries(assetTypes).map(async ([name, type]) => {
    const bytes = await readFile(localFile(name));
    if (type === 'image/svg+xml') assertEmbeddedUrls(bytes.toString('utf8'), name);
    return [name, `data:${type};base64,${bytes.toString('base64')}`];
  })),
]);
const assets = Object.fromEntries(assetEntries);
let html = template;

const links = [...template.matchAll(/<link\b[^>]*>/gi)];
const styles = await Promise.all(links.map(async ([tag]) => {
  if (attribute(tag, 'rel') !== 'stylesheet') throw new Error(`Unhandled link: ${tag}`);
  const name = attribute(tag, 'href');
  if (!name || /^(?:[a-z]+:|\/)/i.test(name)) throw new Error(`Not a local stylesheet: ${name}`);
  const css = await readFile(localFile(name), 'utf8');
  if (/@import\b/i.test(css)) throw new Error(`Cannot bundle CSS imports in ${name}`);
  for (const match of css.matchAll(/url\(\s*(["']?)(.*?)\1\s*\)/gi)) {
    if (!/^(?:data:|#)/i.test(match[2])) throw new Error(`Unbundled CSS URL in ${name}: ${match[2]}`);
  }
  return [tag, `<style>\n${css.replace(/<\/style/gi, '<\\/style')}\n</style>`];
}));
for (const [tag, inlineStyle] of styles) html = html.replace(tag, () => inlineStyle);

let scriptsRemoved = 0;
html = html.replace(/[ \t]*<script\b[^>]*>[\s\S]*?<\/script\s*>/gi, tag => {
  if (attribute(tag, 'src') !== 'app.js') throw new Error(`Unhandled script: ${tag}`);
  scriptsRemoved++;
  return '';
});
if (scriptsRemoved !== 1) throw new Error('Expected exactly one app.js script');

let imagesEmbedded = 0;
html = html.replace(/<img\b[^>]*>/gi, tag => {
  const name = attribute(tag, 'src');
  if (!name || !assets[name]) throw new Error(`Unbundled image: ${name}`);
  imagesEmbedded++;
  return tag.replace(/\bsrc\s*=\s*(["'])(.*?)\1/i, () => `data-embedded-asset="${name}"`);
});

// Inline scripts do not honor defer. Place the map and app after the complete
// document body so initialization sees the same DOM as the original defer script.
const assetMap = JSON.stringify(assets).replace(/</g, '\\u003c');
// Hydrate before app initialization clones the pack template. Each bitmap is
// stored once in the map instead of repeating its base64 text in every img tag.
const hydrateImages = `document.querySelectorAll('img[data-embedded-asset]').forEach(image => {
  image.src = window.__SUMMON_ASSETS__[image.dataset.embeddedAsset];
  image.removeAttribute('data-embedded-asset');
});`;
const script = `window.__SUMMON_ASSETS__ = ${assetMap};\n${hydrateImages}\n${app}`.replace(/<\/script/gi, '<\\/script');
if (!/<\/body\s*>/i.test(html)) throw new Error('Missing document body');
html = html.replace(/<\/body\s*>/i, () => `<script>\n${script}\n</script>\n</body>`);
// A relative homepage link would leave the self-contained file on file://.
html = html.replace(/href=(['"])\.\/\1/g, 'href="#"');

const output = localFile('standalone.html');
await writeFile(output, html);
console.log(`Built ${output} (${(Buffer.byteLength(html) / 1024 / 1024).toFixed(1)} MiB; ${assetEntries.length} assets, ${imagesEmbedded} embedded images)`);
